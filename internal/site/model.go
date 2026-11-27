package site

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/watch"
)

// Model es el contenido completo del sitio (site.json, data-model.md §1). Es
// el único insumo de Build: las plantillas no leen manifiestos ni estado.
type Model struct {
	Schema   int       `json:"schema"`
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Source   string    `json:"source"` // repositorio de paquetes en GitHub (pie)
	Serial   int       `json:"serial"`
	Updated  string    `json:"updated"` // fecha UTC YYYY-MM-DD de la publicación más reciente
	Repo     Repo      `json:"repo"`
	Commands Commands  `json:"commands"`
	Packages []Package `json:"packages"`
}

// Repo describe el repositorio APT.
type Repo struct {
	Suites        []Suite  `json:"suites"`
	Architectures []string `json:"architectures"`
	Component     string   `json:"component"`
	Keyring       Keyring  `json:"keyring"`
}

// Suite es una suite soportada, tal como la describe la configuración
// (specs/007-configurable-suites, data-model §1).
type Suite struct {
	Name     string `json:"name"`
	Distro   string `json:"distro"`   // debian | ubuntu
	Label    string `json:"label"`    // "Debian 14 (testing)", "Ubuntu 24.04"
	Version  string `json:"version"`  // "14", "24.04"
	Catchall bool   `json:"catchall"` // recibe cualquier versión de su distribución que no coincida con otra
}

// Keyring es la llave pública del repositorio.
type Keyring struct {
	Path         string   `json:"path"`         // relativa a la URL del repositorio
	InstalledAs  string   `json:"installed_as"` // ruta en la máquina del usuario
	Fingerprints []string `json:"fingerprints"` // llaves maestras, agrupadas como gpg
}

// Commands son los bloques que el visitante copia (research D8).
type Commands struct {
	Detect Command `json:"detect"` // detecta la suite (FR-002)
	Debian Command `json:"debian"`
	Ubuntu Command `json:"ubuntu"`
	Verify Command `json:"verify"`
	Remove Command `json:"remove"`
}

// Command es un bloque de comandos; Step es su data-debfactory-step.
type Command struct {
	Step   string `json:"step"`
	Script string `json:"script"`
}

// Package es un paquete publicado.
type Package struct {
	Name        string      `json:"name"`
	Summary     Text        `json:"summary"`
	Description Paragraphs  `json:"description"`
	License     string      `json:"license"`
	Homepage    string      `json:"homepage"`
	Upstream    string      `json:"upstream"` // owner/repo en GitHub
	Excluded    []Exclusion `json:"excluded"`
	Install     string      `json:"install"`
	Versions    []Version   `json:"versions"` // de mayor a menor; la primera es la actual
}

// Text es un texto en inglés con su traducción opcional al español.
type Text struct {
	EN string  `json:"en"`
	ES *string `json:"es"`
}

// Paragraphs es una descripción en párrafos, en inglés y opcionalmente en
// español.
type Paragraphs struct {
	EN []string `json:"en"`
	ES []string `json:"es"`
}

// Exclusion es una suite excluida por el manifiesto, con su motivo.
type Exclusion struct {
	Suite  string `json:"suite"`
	Reason string `json:"reason"`
}

// Version es una versión publicada de un paquete.
type Version struct {
	Version       string   `json:"version"`
	Upstream      string   `json:"upstream"`
	Published     string   `json:"published"` // fecha UTC YYYY-MM-DD
	Suites        []string `json:"suites"`
	Architectures []string `json:"architectures"`
	Available     string   `json:"available"` // "Debian 12, 13, sid · Ubuntu 24.04, 26.04"
	Missing       string   `json:"missing"`   // suites soportadas donde no está: "Debian 12"
	Install       string   `json:"install"`
	// Nivel 2 (specs/008-source-builds, research D13): la versión de cada
	// suite (lleva su sufijo) y la orden para bajar el código fuente.
	SuiteVersions map[string]string `json:"suite_versions,omitempty"`
	Source        string            `json:"source,omitempty"`
}

// SuiteVersion es la versión del paquete en una suite.
func (v Version) SuiteVersion(suite string) string {
	if sv, ok := v.SuiteVersions[suite]; ok {
		return sv
	}
	return v.Version
}

// Constantes de la fase 002 (docs/usuario.md del repositorio de paquetes).
const (
	KeyringFile      = "debfactory.gpg"
	KeyringInstalled = "/etc/apt/keyrings/debfactory.gpg"
	SourcesFile      = "/etc/apt/sources.list.d/debfactory.sources"
)

// distroName es el nombre visible de una distribución: "debian" → "Debian".
func distroName(id string) string {
	if id == "" {
		return id
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

var tepacheRev = regexp.MustCompile(`-0~tepache[0-9]+$`)

func siteErr(format string, args ...any) *report.Error {
	return report.Newf(report.Site, format, args...)
}

// Load construye el modelo desde la configuración, los manifiestos de root y
// el estado de referencia st, que MUST describir la generación gen (su
// SHA-256 es gen.StateSHA256; data-model §1). En CI st lo entrega la
// ejecución, aunque venga de un PR de estado sin integrar. warn recibe los
// avisos (un paquete publicado sin manifiesto); puede ser nil.
func Load(cfg *config.Config, root string, st *repo.State, gen *repo.GenInfo, warn func(string)) (*Model, error) {
	if gen == nil {
		return nil, siteErr("no hay generación publicada")
	}
	sha, err := st.SHA256()
	if err != nil {
		return nil, siteErr("%v", err)
	}
	if sha != gen.StateSHA256 {
		return nil, siteErr("el estado (serial %d) no describe la generación vigente (serial %d)", st.Serial, gen.Serial)
	}
	if cfg.Repo.URL == "" {
		return nil, siteErr("repo.url está vacía")
	}
	m := &Model{
		Schema: 1,
		Name:   cfg.Site.Name,
		URL:    strings.TrimSuffix(cfg.Repo.URL, "/"),
		Serial: gen.Serial,
		Repo: Repo{
			Architectures: slices.Clone(cfg.Arches),
			Component:     cfg.Repo.Component,
			Keyring:       Keyring{Path: KeyringFile, InstalledAs: KeyringInstalled},
		},
		Packages: []Package{},
	}
	if m.Name == "" {
		m.Name = cfg.Repo.Label
	}
	if cfg.CI.Repository != "" {
		m.Source = "https://github.com/" + cfg.CI.Repository
	}
	for _, s := range cfg.Suites {
		m.Repo.Suites = append(m.Repo.Suites, Suite{Name: s.Name, Distro: s.Distro, Label: s.Label,
			Version: s.Version, Catchall: s.Catchall})
	}
	for _, f := range cfg.Repo.Signing.Masters {
		m.Repo.Keyring.Fingerprints = append(m.Repo.Keyring.Fingerprints, GroupFingerprint(f))
	}
	m.Commands = commands(m)
	for _, name := range st.PackageNames() {
		published := st.Packages[name]
		if len(published) == 0 {
			continue
		}
		if slices.Contains(manifest.ReservedNames, name) {
			return nil, siteErr("el paquete %q usa un nombre reservado por el sitio", name)
		}
		p, err := loadPackage(cfg, root, name, published, warn)
		if err != nil {
			return nil, err
		}
		for i := range p.Versions {
			p.Versions[i].Available, p.Versions[i].Missing = availability(m.Repo.Suites, p.Versions[i].Suites)
			if p.Versions[i].Published > m.Updated {
				m.Updated = p.Versions[i].Published
			}
		}
		m.Packages = append(m.Packages, p)
	}
	return m, nil
}

func loadPackage(cfg *config.Config, root, name string, published []repo.Published, warn func(string)) (Package, error) {
	p := Package{Name: name, Excluded: []Exclusion{}, Install: "sudo apt install " + name}
	if _, err := os.Stat(filepath.Join(root, manifest.Path(name))); errors.Is(err, fs.ErrNotExist) {
		if warn != nil {
			warn(fmt.Sprintf("site: %s está publicado pero no tiene manifiesto; se muestra sin descripción", name))
		}
		p.Description.EN = []string{}
	} else if mf, err := manifest.Load(root, name); err != nil {
		return p, err // ya es un error de validate con paquete y campo
	} else {
		p.Summary.EN = mf.Summary
		p.Description.EN = paragraphs(mf.Description)
		p.License = mf.License.SPDX
		p.Homepage = mf.Homepage
		p.Upstream = mf.Upstream
		if es := mf.I18n.ES; es != nil {
			if es.Summary != "" {
				s := es.Summary
				p.Summary.ES = &s
			}
			if es.Description != "" {
				p.Description.ES = paragraphs(es.Description)
			}
		}
		for _, s := range cfg.Suites {
			if reason, ok := mf.Exclude.Suites[s.Name]; ok {
				p.Excluded = append(p.Excluded, Exclusion{Suite: s.Name, Reason: strings.TrimSpace(reason)})
			}
		}
	}
	for _, pub := range published {
		v := Version{
			Version:   pub.Version,
			Upstream:  tepacheRev.ReplaceAllString(pub.Version, ""),
			Published: pub.Published.UTC().Format("2006-01-02"),
			Install:   "sudo apt install --allow-downgrades " + name + "=" + pub.Version,
		}
		for _, s := range cfg.Suites {
			if slices.Contains(pub.Suites, s.Name) {
				v.Suites = append(v.Suites, s.Name)
			}
		}
		for _, a := range cfg.Arches {
			if pub.DebSHA(firstSuite(pub), a) != "" {
				v.Architectures = append(v.Architectures, a)
			}
		}
		if pub.Level2() {
			v.SuiteVersions = map[string]string{}
			for _, su := range pub.Suites {
				v.SuiteVersions[su] = pub.SuiteVersion(su)
			}
			v.Install = "sudo apt install --allow-downgrades '" + name + "=" + pub.Version + "+*'"
			v.Source = "apt-get source " + name
		}
		p.Versions = append(p.Versions, v)
	}
	slices.SortStableFunc(p.Versions, func(a, b Version) int { return CompareDebian(b.Version, a.Version) })
	return p, nil
}

// firstSuite es la primera suite de una versión ("" si no tiene).
func firstSuite(p repo.Published) string {
	if len(p.Suites) == 0 {
		return ""
	}
	return p.Suites[0]
}

// availability describe en texto dónde está una versión: las suites por
// distribución con su versión corta ("Debian 12, 13, 14, sid · Ubuntu 24.04,
// 26.04") y, aparte, las configuradas donde no está ("Debian 12"). Las
// distribuciones van en el orden en que aparecen en la configuración.
func availability(all []Suite, in []string) (available, missing string) {
	var groups, gone, distros []string
	for _, s := range all {
		if !slices.Contains(distros, s.Distro) {
			distros = append(distros, s.Distro)
		}
	}
	for _, d := range distros {
		var versions []string
		for _, s := range all {
			if s.Distro != d {
				continue
			}
			if slices.Contains(in, s.Name) {
				versions = append(versions, s.Version)
			} else {
				gone = append(gone, s.Label)
			}
		}
		if len(versions) > 0 {
			groups = append(groups, distroName(d)+" "+strings.Join(versions, ", "))
		}
	}
	return strings.Join(groups, " · "), strings.Join(gone, ", ")
}

// CompareDebian compara dos versiones de paquete de la forma
// <upstream>-<revisión> con el algoritmo de dpkg: primero la parte upstream y
// después la revisión (constitución VI).
func CompareDebian(a, b string) int {
	ua, ra := splitRevision(a)
	ub, rb := splitRevision(b)
	if c := watch.Compare(ua, ub); c != 0 {
		return c
	}
	return watch.Compare(ra, rb)
}

func splitRevision(v string) (upstream, revision string) {
	if i := strings.LastIndex(v, "-"); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

// paragraphs divide un texto en párrafos separados por líneas en blanco y une
// las líneas de cada párrafo con un espacio. Texto plano: el escape lo hace la
// plantilla (FR-010).
func paragraphs(s string) []string {
	out := []string{}
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

// GroupFingerprint agrupa un fingerprint de 40 hex como lo muestra gpg:
// grupos de 4 con un espacio doble en medio.
func GroupFingerprint(f string) string {
	f = strings.ToUpper(strings.ReplaceAll(f, " ", ""))
	var groups []string
	for i := 0; i+4 <= len(f); i += 4 {
		groups = append(groups, f[i:i+4])
	}
	if len(groups) != 10 {
		return f
	}
	return strings.Join(groups[:5], " ") + "  " + strings.Join(groups[5:], " ")
}

// FromData carga el modelo de la generación vigente de dataDir con el estado
// de referencia st y lo verifica contra sus índices (research D2). Es lo que
// hacen `site data`, `site sync` y el paso site de ci.
func FromData(cfg *config.Config, root string, st *repo.State, dataDir string, warn func(string)) (*Model, error) {
	gen, err := repo.Current(dataDir)
	if err != nil {
		return nil, siteErr("%v", err)
	}
	m, err := Load(cfg, root, st, gen, warn)
	if err != nil {
		return nil, err
	}
	if err := Verify(m, gen.Dir(dataDir)); err != nil {
		return nil, err
	}
	return m, nil
}
