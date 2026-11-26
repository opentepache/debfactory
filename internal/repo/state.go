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
	Debs      map[string]string `yaml:"debs"` // arch -> sha256
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
	if s.Schema != 1 {
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
	fmt.Fprintf(&b, "schema: %d\nserial: %d\n", s.Schema, s.Serial)
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
			b.WriteString("      debs:\n")
			arches := make([]string, 0, len(p.Debs))
			for a := range p.Debs {
				arches = append(arches, a)
			}
			sort.Strings(arches)
			for _, a := range arches {
				fmt.Fprintf(&b, "        %s: %s\n", a, p.Debs[a])
			}
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
