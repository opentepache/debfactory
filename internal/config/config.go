// Package config carga y valida la configuración compartida de config/
// (contracts/config.md): config/debfactory.yaml y config/licenses.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"go.yaml.in/yaml/v3"

	"debfactory/internal/report"
)

// Rutas relativas a la raíz del repositorio.
const (
	MainFile     = "config/debfactory.yaml"
	LicensesFile = "config/licenses.yaml"
)

// Config son los datos comunes a todos los paquetes (FR-009).
type Config struct {
	Schema           int               `yaml:"schema"`
	Maintainer       string            `yaml:"maintainer"`
	Arches           []string          `yaml:"arches"`
	Suites           Suites            `yaml:"suites"`
	Lintian          Lintian           `yaml:"lintian"`
	ELFDependencies  map[string]string `yaml:"elf_dependencies"`
	CommunityRepos   []Repo            `yaml:"community_repos"`
	CompletionsImage string            `yaml:"completions_image"`

	// Licenses es la allowlist de config/licenses.yaml (constitución IV).
	Licenses []string `yaml:"-"`
}

// Suite es una distribución soportada.
type Suite struct {
	Name         string
	Image        string
	Level2Suffix string
}

// Suites conserva el orden del YAML, que es el orden del informe de pruebas.
type Suites []Suite

// Lintian configura la etapa lintian.
type Lintian struct {
	Image                 string   `yaml:"image"`
	FailOn                string   `yaml:"fail_on"`
	NeverOverridePatterns []string `yaml:"never_override_patterns"`

	neverOverride []*regexp.Regexp
}

// Repo es un repositorio APT comunitario conocido (constitución V).
type Repo struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type licensesFile struct {
	Schema  int      `yaml:"schema"`
	Allowed []string `yaml:"allowed"`
}

// Load lee y valida los dos archivos de config/ bajo root.
func Load(root string) (*Config, error) {
	var c Config
	if err := decodeStrict(filepath.Join(root, MainFile), &c); err != nil {
		return nil, configErr(MainFile, err.Error())
	}
	if err := c.validate(); err != nil {
		return nil, configErr(MainFile, err.Error())
	}
	var l licensesFile
	if err := decodeStrict(filepath.Join(root, LicensesFile), &l); err != nil {
		return nil, configErr(LicensesFile, err.Error())
	}
	if l.Schema != 1 {
		return nil, configErr(LicensesFile, "schema: debe ser 1")
	}
	if len(l.Allowed) == 0 {
		return nil, configErr(LicensesFile, "allowed: la lista está vacía")
	}
	c.Licenses = l.Allowed
	return &c, nil
}

// Suite devuelve la suite con ese nombre.
func (c *Config) Suite(name string) (Suite, bool) {
	for _, s := range c.Suites {
		if s.Name == name {
			return s, true
		}
	}
	return Suite{}, false
}

// LicenseAllowed indica si el identificador SPDX está en la allowlist.
func (c *Config) LicenseAllowed(id string) bool {
	return slices.Contains(c.Licenses, id)
}

// Overridable indica si una etiqueta de lintian se puede silenciar
// (constitución VII: nunca las de seguridad, licencia o copyright).
func (l *Lintian) Overridable(tag string) bool {
	for _, re := range l.neverOverride {
		if re.MatchString(tag) {
			return false
		}
	}
	return true
}

var glibcPlaceholder = regexp.MustCompile(`\{[^}]*\}`)

func (c *Config) validate() error {
	if c.Schema != 1 {
		return errors.New("schema: debe ser 1")
	}
	if c.Maintainer == "" {
		return errors.New("maintainer: no puede estar vacío")
	}
	if !slices.Equal(c.Arches, []string{"amd64", "arm64"}) {
		return fmt.Errorf("arches: debe ser [amd64, arm64] (hay %v)", c.Arches)
	}
	if len(c.Suites) == 0 {
		return errors.New("suites: la lista está vacía")
	}
	for _, s := range c.Suites {
		if s.Image == "" {
			return fmt.Errorf("suites.%s.image: no puede estar vacío", s.Name)
		}
		if s.Level2Suffix == "" {
			return fmt.Errorf("suites.%s.level2_suffix: no puede estar vacío", s.Name)
		}
	}
	if c.Lintian.Image == "" {
		return errors.New("lintian.image: no puede estar vacío")
	}
	if !slices.Contains([]string{"error", "warning", "info", "pedantic"}, c.Lintian.FailOn) {
		return fmt.Errorf("lintian.fail_on: %q no es error, warning, info ni pedantic", c.Lintian.FailOn)
	}
	c.Lintian.neverOverride = nil
	for i, p := range c.Lintian.NeverOverridePatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("lintian.never_override_patterns[%d]: regex inválida: %v", i, err)
		}
		c.Lintian.neverOverride = append(c.Lintian.neverOverride, re)
	}
	if len(c.ELFDependencies) == 0 {
		return errors.New("elf_dependencies: la lista está vacía")
	}
	for lib, dep := range c.ELFDependencies {
		for _, m := range glibcPlaceholder.FindAllString(dep, -1) {
			if m != "{glibc}" {
				return fmt.Errorf("elf_dependencies.%s: marcador desconocido %s (solo {glibc})", lib, m)
			}
		}
	}
	if c.CompletionsImage == "" {
		return errors.New("completions_image: no puede estar vacío")
	}
	return nil
}

// UnmarshalYAML lee el mapa de suites conservando su orden y rechazando
// claves desconocidas.
func (s *Suites) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return errors.New("suites: debe ser un mapa nombre -> {image, level2_suffix}")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		name, body := n.Content[i].Value, n.Content[i+1]
		if body.Kind != yaml.MappingNode {
			return fmt.Errorf("suites.%s: debe ser un mapa", name)
		}
		su := Suite{Name: name}
		for j := 0; j+1 < len(body.Content); j += 2 {
			key, val := body.Content[j].Value, body.Content[j+1].Value
			switch key {
			case "image":
				su.Image = val
			case "level2_suffix":
				su.Level2Suffix = val
			default:
				return fmt.Errorf("suites.%s: propiedad desconocida %q", name, key)
			}
		}
		*s = append(*s, su)
	}
	return nil
}

func decodeStrict(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("el archivo está vacío")
		}
		return err
	}
	return nil
}

func configErr(file, msg string) error {
	return report.New(report.Validate, file+": "+msg)
}
