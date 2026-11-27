// Package repo mantiene el repositorio APT publicado: su estado en git
// (repo/state.yaml), el almacén de .deb, la generación del árbol con aptly,
// su verificación y el cambio atómico de generación
// (specs/002-signed-apt-repo).
package repo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/report"
)

// StateFile es la ruta del estado relativa a la raíz del repositorio.
const StateFile = "repo/state.yaml"

const stateHeader = "# Generado por debfactory publish/retire. No editar a mano.\n"

// State es la fuente de verdad del repositorio (FR-035; contracts/state.md).
type State struct {
	Schema   int                    `yaml:"schema"`
	Serial   int                    `yaml:"serial"`
	Packages map[string][]Published `yaml:"packages"`
	Retired  []Retired              `yaml:"retired"`
}

// Published es una versión publicada de un paquete.
type Published struct {
	Version   string            `yaml:"version"`
	Published time.Time         `yaml:"published"`
	Suites    []string          `yaml:"suites"`
	Debs      map[string]string `yaml:"debs"` // arch -> sha256 (nivel 1)

	// Nivel 2 (specs/008-source-builds, data-model §2): Version es la versión
	// base sin sufijo; cada suite tiene su compilación y Suites son las
	// claves de Builds en el orden de la configuración.
	Level  int              `yaml:"level,omitempty"`
	Orig   *Orig            `yaml:"orig,omitempty"`
	Builds map[string]Build `yaml:"builds,omitempty"`
}

// Orig es el tarball original, común a todas las suites (FR-010).
type Orig struct {
	File   string `yaml:"file" json:"file"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

// Build es la compilación de una versión de nivel 2 en una suite.
type Build struct {
	Version   string            `yaml:"version" json:"version"`       // base + "+" + level2_suffix
	DSC       string            `yaml:"dsc" json:"dsc"`               // sha256 del .dsc
	DebianTar string            `yaml:"debian_tar" json:"debian_tar"` // sha256 del .debian.tar.xz
	Debs      map[string]string `yaml:"debs" json:"debs"`             // arch -> sha256
	Buildinfo map[string]string `yaml:"buildinfo" json:"buildinfo"`   // arch -> sha256
}

// Equal indica si dos compilaciones son la misma.
func (b Build) Equal(o Build) bool {
	return b.Version == o.Version && b.DSC == o.DSC && b.DebianTar == o.DebianTar &&
		maps.Equal(b.Debs, o.Debs) && maps.Equal(b.Buildinfo, o.Buildinfo)
}

// Artifact es un archivo publicado de una versión: su nombre en el pool, su
// sha256 y, si aplica, la suite y la arquitectura.
type Artifact struct {
	Name, SHA256 string
	Kind         string // deb | dsc | debian_tar | orig | buildinfo
	Suite, Arch  string // Suite vacía: el archivo es común (nivel 1 o .orig)
}

// Artifacts lista los archivos de p en un orden fijo. En nivel 1 son los
// .deb; en nivel 2, el .orig común y, por suite, el .dsc, el .debian.tar.xz,
// los .deb y los .buildinfo (research D10).
func (p Published) Artifacts(pkg string, arches []string) []Artifact {
	if !p.Level2() {
		out := make([]Artifact, 0, len(arches))
		for _, a := range arches {
			out = append(out, Artifact{Name: DebFile(pkg, p.Version, a), SHA256: p.Debs[a], Kind: "deb", Arch: a})
		}
		return out
	}
	out := []Artifact{{Name: p.Orig.File, SHA256: p.Orig.SHA256, Kind: "orig"}}
	for _, su := range p.Suites {
		b := p.Builds[su]
		out = append(out,
			Artifact{Name: DscFile(pkg, b.Version), SHA256: b.DSC, Kind: "dsc", Suite: su},
			Artifact{Name: DebianTarFile(pkg, b.Version), SHA256: b.DebianTar, Kind: "debian_tar", Suite: su})
		for _, a := range arches {
			out = append(out,
				Artifact{Name: DebFile(pkg, b.Version, a), SHA256: b.Debs[a], Kind: "deb", Suite: su, Arch: a},
				Artifact{Name: BuildinfoFile(pkg, b.Version, a), SHA256: b.Buildinfo[a], Kind: "buildinfo", Suite: su, Arch: a})
		}
	}
	return out
}

// Level2 indica que la versión se compiló desde el código fuente.
func (p Published) Level2() bool { return p.Level == 2 }

// SuiteVersion es la versión Debian de p en una suite: la de su compilación
// en nivel 2 y la versión común en nivel 1.
func (p Published) SuiteVersion(suite string) string {
	if b, ok := p.Builds[suite]; ok {
		return b.Version
	}
	return p.Version
}

// DebSHA es el sha256 del .deb de p en una suite y arquitectura.
func (p Published) DebSHA(suite, arch string) string {
	if p.Level2() {
		return p.Builds[suite].Debs[arch]
	}
	return p.Debs[arch]
}

// Retired es una versión retirada; nunca se vuelve a publicar (constitución VI).
type Retired struct {
	Package string    `yaml:"package"`
	Version string    `yaml:"version"`
	Date    time.Time `yaml:"date"`
	Reason  string    `yaml:"reason"`
}

// Empty es el estado de un repositorio sin publicar.
func Empty() *State {
	return &State{Schema: 1, Packages: map[string][]Published{}, Retired: []Retired{}}
}

// schemaFor es 2 si hay alguna versión de nivel 2 y 1 si no: un estado sin
// nivel 2 se escribe igual que antes de la fase 008 (FR-014).
func (s *State) schemaFor() int {
	for _, vs := range s.Packages {
		for _, p := range vs {
			if p.Level2() {
				return 2
			}
		}
	}
	return 1
}

// ReadState lee repo/state.yaml de forma estricta. Si no existe, devuelve
// el estado vacío.
func ReadState(root string) (*State, error) {
	return ReadStateFile(filepath.Join(root, StateFile))
}

// ReadStateFile lee un estado desde path (p. ej. la copia de un PR de estado
// sin integrar; specs/006-repo-website, data-model §1). Si no existe,
// devuelve el estado vacío.
func ReadStateFile(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return nil, stateErr("%v", err)
	}
	st := Empty()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(st); err != nil && !errors.Is(err, io.EOF) {
		return nil, stateErr("%v", err)
	}
	if st.Packages == nil {
		st.Packages = map[string][]Published{}
	}
	if st.Retired == nil {
		st.Retired = []Retired{}
	}
	return st, nil
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate aplica las reglas de contracts/state.md y los invariantes de
// data-model.md §1.
func (s *State) Validate(cfg *config.Config) error {
	if s.Schema != 1 && s.Schema != 2 {
		return stateErr("schema %d no soportado", s.Schema)
	}
	retired := map[string]Retired{}
	for i, r := range s.Retired {
		if r.Package == "" || r.Version == "" {
			return stateErr("retired[%d]: package y version son obligatorios", i)
		}
		if strings.TrimSpace(r.Reason) == "" || strings.ContainsAny(r.Reason, "\n\r") {
			return stateErr("retired[%d].reason: debe ser una línea no vacía", i)
		}
		retired[r.Package+" "+r.Version] = r
	}
	names := map[string]string{} // sha -> nombre de archivo
	for _, pkg := range s.PackageNames() {
		versions := s.Packages[pkg]
		seen := map[string]bool{}
		perSuite := map[string]int{}
		for i, p := range versions {
			at := fmt.Sprintf("packages.%s[%d]", pkg, i)
			if p.Version == "" {
				return stateErr("%s.version: no puede estar vacía", at)
			}
			if seen[p.Version] {
				return stateErr("%s: versión %s repetida", at, p.Version)
			}
			seen[p.Version] = true
			if _, ok := retired[pkg+" "+p.Version]; ok {
				return stateErr("%s %s está publicada y retirada", pkg, p.Version)
			}
			if len(p.Suites) == 0 {
				return stateErr("%s.suites: no puede estar vacío", at)
			}
			for _, su := range p.Suites {
				if _, ok := cfg.Suite(su); !ok {
					return stateErr("%s.suites: suite %q no está en config", at, su)
				}
				perSuite[su]++
			}
			if p.Level2() {
				if err := p.validateLevel2(cfg, pkg, at, names); err != nil {
					return err
				}
				continue
			}
			if p.Level != 0 && p.Level != 1 {
				return stateErr("%s.level: %d no es 1 ni 2", at, p.Level)
			}
			if p.Orig != nil || len(p.Builds) > 0 {
				return stateErr("%s: orig y builds solo existen en nivel 2", at)
			}
			for _, arch := range cfg.Arches {
				sha, ok := p.Debs[arch]
				if !ok {
					return stateErr("%s.debs: falta %s", at, arch)
				}
				if !sha256Re.MatchString(sha) {
					return stateErr("%s.debs.%s: sha256 inválido", at, arch)
				}
				file := DebFile(pkg, p.Version, arch)
				if other, ok := names[sha]; ok && other != file {
					return stateErr("%s.debs.%s: el sha256 ya está publicado como %s", at, arch, other)
				}
				names[sha] = file
			}
			for arch := range p.Debs {
				if !slices.Contains(cfg.Arches, arch) {
					return stateErr("%s.debs: arquitectura %q no está en config", at, arch)
				}
			}
		}
		for _, su := range cfg.Suites {
			if n := perSuite[su.Name]; n > cfg.Repo.Keep {
				return stateErr("%s tiene %d versiones en %s (keep: %d)", pkg, n, su.Name, cfg.Repo.Keep)
			}
		}
	}
	return nil
}

// validateLevel2 aplica las reglas de una versión de nivel 2: Suites son las
// claves de Builds en el orden de la configuración, cada versión lleva el
// sufijo de su suite y ningún sha256 se publica con dos nombres (SC-005).
func (p Published) validateLevel2(cfg *config.Config, pkg, at string, names map[string]string) error {
	if s := schemaOf(p); s != "" {
		return stateErr("%s: %s", at, s)
	}
	if len(p.Debs) > 0 {
		return stateErr("%s.debs: en nivel 2 los .deb van en builds", at)
	}
	if p.Orig == nil || p.Orig.File == "" || !sha256Re.MatchString(p.Orig.SHA256) {
		return stateErr("%s.orig: file y sha256 son obligatorios", at)
	}
	claim := func(field, sha, file string) error {
		if !sha256Re.MatchString(sha) {
			return stateErr("%s.%s: sha256 inválido", at, field)
		}
		if other, ok := names[sha]; ok && other != file {
			return stateErr("%s.%s: el sha256 ya está publicado como %s", at, field, other)
		}
		// Un nombre, un contenido (SC-005): el mismo archivo del pool no
		// puede tener dos sha256 (el .orig común sí se repite con el mismo).
		if other, ok := names["file:"+file]; ok && other != sha {
			return stateErr("%s.%s: %s ya está publicado con otro sha256", at, field, file)
		}
		names[sha], names["file:"+file] = file, sha
		return nil
	}
	if err := claim("orig", p.Orig.SHA256, p.Orig.File); err != nil {
		return err
	}
	var order []string
	for _, su := range cfg.Suites {
		if _, ok := p.Builds[su.Name]; ok {
			order = append(order, su.Name)
		}
	}
	if len(order) != len(p.Builds) {
		for su := range p.Builds {
			if _, ok := cfg.Suite(su); !ok {
				return stateErr("%s.builds: suite %q no está en config", at, su)
			}
		}
	}
	if !slices.Equal(order, p.Suites) {
		return stateErr("%s.suites: debe ser %v (las suites de builds en el orden de config)", at, order)
	}
	for _, name := range order {
		su, _ := cfg.Suite(name)
		b := p.Builds[name]
		bat := at + ".builds." + name
		if want := p.Version + "+" + su.Level2Suffix; b.Version != want {
			return stateErr("%s.version: %q debería ser %q", bat, b.Version, want)
		}
		if err := claim("builds."+name+".dsc", b.DSC, DscFile(pkg, b.Version)); err != nil {
			return err
		}
		if err := claim("builds."+name+".debian_tar", b.DebianTar, DebianTarFile(pkg, b.Version)); err != nil {
			return err
		}
		for _, arch := range cfg.Arches {
			if err := claim("builds."+name+".debs."+arch, b.Debs[arch], DebFile(pkg, b.Version, arch)); err != nil {
				return err
			}
			if err := claim("builds."+name+".buildinfo."+arch, b.Buildinfo[arch], BuildinfoFile(pkg, b.Version, arch)); err != nil {
				return err
			}
		}
		if len(b.Debs) != len(cfg.Arches) || len(b.Buildinfo) != len(cfg.Arches) {
			return stateErr("%s: debs y buildinfo deben tener exactamente %v", bat, cfg.Arches)
		}
	}
	return nil
}

// schemaOf comprueba que la versión base no lleve sufijo de suite.
func schemaOf(p Published) string {
	if strings.Contains(p.Version, "+") {
		return "version: la versión base de nivel 2 no lleva sufijo de suite"
	}
	return ""
}

// PackageNames devuelve los paquetes en orden alfabético.
func (s *State) PackageNames() []string {
	names := make([]string, 0, len(s.Packages))
	for n := range s.Packages {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Find devuelve la versión publicada de pkg, si existe.
func (s *State) Find(pkg, version string) (Published, bool) {
	for _, p := range s.Packages[pkg] {
		if p.Version == version {
			return p, true
		}
	}
	return Published{}, false
}

// FindRetired devuelve el retiro de pkg version, si existe.
func (s *State) FindRetired(pkg, version string) (Retired, bool) {
	for _, r := range s.Retired {
		if r.Package == pkg && r.Version == version {
			return r, true
		}
	}
	return Retired{}, false
}

// DebFile es el nombre del .deb en el pool.
func DebFile(pkg, version, arch string) string {
	return fmt.Sprintf("%s_%s_%s.deb", pkg, version, arch)
}

// DscFile, DebianTarFile y BuildinfoFile son los nombres de los archivos del
// paquete fuente de nivel 2 en el pool (research D6, D10).
func DscFile(pkg, version string) string { return fmt.Sprintf("%s_%s.dsc", pkg, version) }

// DebianTarFile es el nombre del .debian.tar.xz de una versión con sufijo.
func DebianTarFile(pkg, version string) string {
	return fmt.Sprintf("%s_%s.debian.tar.xz", pkg, version)
}

// BuildinfoFile es el nombre del .buildinfo de una compilación.
func BuildinfoFile(pkg, version, arch string) string {
	return fmt.Sprintf("%s_%s_%s.buildinfo", pkg, version, arch)
}

// Clone devuelve una copia profunda.
func (s *State) Clone() *State {
	c := &State{Schema: s.Schema, Serial: s.Serial, Packages: map[string][]Published{},
		Retired: slices.Clone(s.Retired)}
	if c.Retired == nil {
		c.Retired = []Retired{}
	}
	for name, vs := range s.Packages {
		out := make([]Published, len(vs))
		for i, p := range vs {
			p.Suites = slices.Clone(p.Suites)
			debs := map[string]string{}
			for k, v := range p.Debs {
				debs[k] = v
			}
			p.Debs = debs
			if p.Orig != nil {
				o := *p.Orig
				p.Orig = &o
			}
			if p.Builds != nil {
				builds := make(map[string]Build, len(p.Builds))
				for su, b := range p.Builds {
					b.Debs = maps.Clone(b.Debs)
					b.Buildinfo = maps.Clone(b.Buildinfo)
					builds[su] = b
				}
				p.Builds = builds
			}
			out[i] = p
		}
		c.Packages[name] = out
	}
	return c
}

// compareVersions ejecuta dpkg --compare-versions (constitución VI).
func compareVersions(a, op, b string) (bool, error) {
	return installtest.CompareVersions(a, op, b)
}

// sortVersions ordena de mayor a menor con dpkg --compare-versions.
func sortVersions(vs []Published) error {
	var err error
	sort.SliceStable(vs, func(i, j int) bool {
		gt, e := compareVersions(vs[i].Version, "gt", vs[j].Version)
		if e != nil && err == nil {
			err = e
		}
		return gt
	})
	return err
}

// Marshal serializa el estado de forma determinista (contracts/state.md).
func (s *State) Marshal() ([]byte, error) {
	var b strings.Builder
	b.WriteString(stateHeader)
	fmt.Fprintf(&b, "schema: %d\nserial: %d\n", s.schemaFor(), s.Serial)
	if len(s.Packages) == 0 {
		b.WriteString("packages: {}\n")
	} else {
		b.WriteString("packages:\n")
	}
	for _, name := range s.PackageNames() {
		vs := slices.Clone(s.Packages[name])
		if err := sortVersions(vs); err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "  %s:\n", name)
		for _, p := range vs {
			fmt.Fprintf(&b, "    - version: %s\n", scalar(p.Version))
			fmt.Fprintf(&b, "      published: %s\n", p.Published.UTC().Format(time.RFC3339))
			fmt.Fprintf(&b, "      suites: [%s]\n", strings.Join(p.Suites, ", "))
			if p.Level2() {
				marshalLevel2(&b, p)
				continue
			}
			b.WriteString("      debs:\n")
			writeMap(&b, "        ", p.Debs)
		}
	}
	if len(s.Retired) == 0 {
		b.WriteString("retired: []\n")
	} else {
		b.WriteString("retired:\n")
	}
	rs := slices.Clone(s.Retired)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Date.Before(rs[j].Date) })
	for _, r := range rs {
		fmt.Fprintf(&b, "  - package: %s\n", scalar(r.Package))
		fmt.Fprintf(&b, "    version: %s\n", scalar(r.Version))
		fmt.Fprintf(&b, "    date: %s\n", r.Date.UTC().Format(time.RFC3339))
		fmt.Fprintf(&b, "    reason: %s\n", quoted(r.Reason))
	}
	return []byte(b.String()), nil
}

// marshalLevel2 escribe los campos de una versión de nivel 2; las suites de
// builds van en el orden de suites (que ya es el de la configuración).
func marshalLevel2(b *strings.Builder, p Published) {
	fmt.Fprintf(b, "      level: 2\n")
	fmt.Fprintf(b, "      orig: {file: %s, sha256: %s}\n", scalar(p.Orig.File), p.Orig.SHA256)
	b.WriteString("      builds:\n")
	for _, su := range p.Suites {
		bd := p.Builds[su]
		fmt.Fprintf(b, "        %s:\n", su)
		fmt.Fprintf(b, "          version: %s\n", scalar(bd.Version))
		fmt.Fprintf(b, "          dsc: %s\n", bd.DSC)
		fmt.Fprintf(b, "          debian_tar: %s\n", bd.DebianTar)
		b.WriteString("          debs:\n")
		writeMap(b, "            ", bd.Debs)
		b.WriteString("          buildinfo:\n")
		writeMap(b, "            ", bd.Buildinfo)
	}
}

// writeMap escribe un mapa con las claves ordenadas.
func writeMap(b *strings.Builder, indent string, m map[string]string) {
	keys := slices.Sorted(maps.Keys(m))
	for _, k := range keys {
		fmt.Fprintf(b, "%s%s: %s\n", indent, k, m[k])
	}
}

// Write escribe repo/state.yaml de forma atómica.
func (s *State) Write(root string) error {
	data, err := s.Marshal()
	if err != nil {
		return stateErr("%v", err)
	}
	return writeAtomic(filepath.Join(root, StateFile), data)
}

// SHA256 es el sha256 del estado serializado.
func (s *State) SHA256() (string, error) {
	data, err := s.Marshal()
	if err != nil {
		return "", err
	}
	return fetch.SHA256(data), nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// scalar escribe un escalar YAML sin comillas si es seguro.
func scalar(v string) string {
	out, err := yaml.Marshal(v)
	if err != nil {
		return quoted(v)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// quoted escribe una cadena YAML entre comillas dobles.
func quoted(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(v) + `"`
}

func stateErr(format string, args ...any) *report.Error {
	return &report.Error{Stage: report.State, Cause: StateFile + ": " + fmt.Sprintf(format, args...)}
}
