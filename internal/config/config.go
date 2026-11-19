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
	"strconv"
	"strings"
	"time"

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
	Repo             RepoConfig        `yaml:"repo"`
	CI               CI                `yaml:"ci"`
	Watch            Watch             `yaml:"watch"`

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

// RepoConfig es la sección repo: el repositorio APT publicado
// (specs/002-signed-apt-repo/contracts/config.md).
type RepoConfig struct {
	URL          string                 `yaml:"url"`
	Origin       string                 `yaml:"origin"`
	Label        string                 `yaml:"label"`
	Component    string                 `yaml:"component"`
	Keep         int                    `yaml:"keep"`
	Signing      Signing                `yaml:"signing"`
	Validity     Validity               `yaml:"validity"`
	Destinations map[string]Destination `yaml:"destinations"`
	CacheControl CacheControl           `yaml:"cache_control"`
}

// Signing identifica la llave del repositorio. Los fingerprints son públicos;
// la parte secreta vive fuera del repositorio git.
type Signing struct {
	Masters  []string `yaml:"masters"`
	Subkey   string   `yaml:"subkey"`
	WarnDays int      `yaml:"warn_days"`
}

// Validity es la caducidad de los índices firmados (Valid-Until, FR-030 de
// la fase 003).
type Validity struct {
	Days     int `yaml:"days"`
	WarnDays int `yaml:"warn_days"`
}

// CI es la configuración del pipeline (specs/003-ci-pipeline/contracts/config.md).
type CI struct {
	Repository    string   `yaml:"repository"`
	Destination   string   `yaml:"destination"`
	StateBranch   string   `yaml:"state_branch"`
	StatePRMaxAge Duration `yaml:"state_pr_max_age"`
	FailureLabel  string   `yaml:"failure_label"`
	Labels        CILabels `yaml:"labels"`
	CodePaths     []string `yaml:"code_paths"`
	// ToolRepository es el repositorio de la herramienta, solo para el
	// enlace de diferencias al subir debfactory.version (004 research D6).
	ToolRepository string `yaml:"tool_repository"`
}

// Watch configura el detector de versiones upstream
// (specs/005-upstream-release-watch, data-model.md §2). Todos los campos son
// opcionales; los métodos devuelven el valor por defecto si faltan.
type Watch struct {
	Interval     Duration  `yaml:"interval"`      // entre ciclos; mínimo 1h; 24h
	Cooldown     *Duration `yaml:"cooldown"`      // espera desde el release; global; 48h
	RateReserve  *int      `yaml:"rate_reserve"`  // peticiones que el ciclo deja sin usar; 500
	BranchPrefix string    `yaml:"branch_prefix"` // ramas del detector; debfactory/update/
}

// Valores por defecto del bloque watch.
const (
	DefaultWatchInterval = 24 * time.Hour
	DefaultWatchCooldown = 48 * time.Hour
	DefaultRateReserve   = 500
	DefaultBranchPrefix  = "debfactory/update/"
)

// IntervalDuration es watch.interval.
func (w Watch) IntervalDuration() time.Duration {
	if w.Interval == 0 {
		return DefaultWatchInterval
	}
	return time.Duration(w.Interval)
}

// CooldownDuration es watch.cooldown (0h es un valor válido).
func (w Watch) CooldownDuration() time.Duration {
	if w.Cooldown == nil {
		return DefaultWatchCooldown
	}
	return time.Duration(*w.Cooldown)
}

// Reserve es watch.rate_reserve.
func (w Watch) Reserve() int {
	if w.RateReserve == nil {
		return DefaultRateReserve
	}
	return *w.RateReserve
}

// Prefix es watch.branch_prefix.
func (w Watch) Prefix() string {
	if w.BranchPrefix == "" {
		return DefaultBranchPrefix
	}
	return w.BranchPrefix
}

var branchPrefixRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*/$`)

func (w Watch) validate(stateBranch string) error {
	if w.Interval != 0 && time.Duration(w.Interval) < time.Hour {
		return errors.New("watch.interval: mínimo 1h")
	}
	if w.Cooldown != nil && *w.Cooldown < 0 {
		return errors.New("watch.cooldown: no puede ser negativo")
	}
	if w.RateReserve != nil && *w.RateReserve < 0 {
		return errors.New("watch.rate_reserve: no puede ser negativo")
	}
	p := w.Prefix()
	if !branchPrefixRe.MatchString(p) || strings.Contains(p, "..") || strings.HasSuffix(p, ".lock/") {
		return fmt.Errorf("watch.branch_prefix: %q no es un prefijo de rama válido (termina en /)", p)
	}
	if strings.HasPrefix(stateBranch+"/", p) || p == "main/" {
		return fmt.Errorf("watch.branch_prefix: %q choca con main o con ci.state_branch", p)
	}
	return nil
}

// CILabels son las etiquetas de los runners de cada nivel de confianza.
type CILabels struct {
	CI  string `yaml:"ci"`
	Pub string `yaml:"pub"`
}

// Duration acepta los sufijos de time.ParseDuration y además "d" (días).
type Duration time.Duration

// UnmarshalYAML lee "7d", "36h", "90m"...
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// ParseDuration es time.ParseDuration con el sufijo "d" (24 h).
func ParseDuration(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("duración %q inválida", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("duración %q inválida", s)
	}
	return v, nil
}

// Destination es un destino de sincronización: un remote de rclone, sin
// credenciales.
type Destination struct {
	Remote       string `yaml:"remote"`
	PublicURL    string `yaml:"public_url"`
	CacheControl bool   `yaml:"cache_control"`
}

// CacheControl son los encabezados que se suben con cada archivo.
type CacheControl struct {
	Immutable string `yaml:"immutable"`
	Mutable   string `yaml:"mutable"`
}

var (
	fingerprintRe = regexp.MustCompile(`^[0-9A-F]{40}$`)
	destNameRe    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	credentialRe  = regexp.MustCompile(`(?i)@|password|key=|secret`)
)

func (r *RepoConfig) validate() error {
	if r.Keep < 1 {
		return errors.New("repo.keep: mínimo 1")
	}
	if r.Origin == "" || r.Label == "" || r.Component == "" {
		return errors.New("repo: origin, label y component no pueden estar vacíos")
	}
	for i, f := range r.Signing.Masters {
		if !fingerprintRe.MatchString(f) {
			return fmt.Errorf("repo.signing.masters[%d]: fingerprint inválido", i)
		}
	}
	if r.Signing.Subkey != "" && !fingerprintRe.MatchString(r.Signing.Subkey) {
		return errors.New("repo.signing.subkey: fingerprint inválido")
	}
	if r.Signing.WarnDays < 0 {
		return errors.New("repo.signing.warn_days: no puede ser negativo")
	}
	for name, d := range r.Destinations {
		if !destNameRe.MatchString(name) {
			return fmt.Errorf("repo.destinations: nombre %q inválido", name)
		}
		if d.Remote == "" {
			return fmt.Errorf("repo.destinations.%s.remote: no puede estar vacío", name)
		}
		if credentialRe.MatchString(d.Remote) {
			return fmt.Errorf("repo.destinations.%s.remote: no puede contener credenciales", name)
		}
	}
	if r.CacheControl.Immutable == "" || r.CacheControl.Mutable == "" {
		return errors.New("repo.cache_control: immutable y mutable no pueden estar vacíos")
	}
	if r.Validity.Days == 0 && r.Validity.WarnDays == 0 {
		r.Validity = Validity{Days: 30, WarnDays: 7}
	}
	if r.Validity.Days < 8 || r.Validity.Days > 90 {
		return errors.New("repo.validity.days debe estar entre 8 y 90")
	}
	if r.Validity.WarnDays < 0 || r.Validity.WarnDays >= r.Validity.Days-7 {
		return errors.New("repo.validity.warn_days deja menos de una semana entre refirmados")
	}
	return nil
}

var repositoryRe = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

// validate comprueba la sección ci. repository puede estar vacío hasta que
// exista el repositorio en GitHub; las órdenes ci lo exigen (RequireRepository).
func (c *CI) validate(r *RepoConfig) error {
	if c.Repository != "" && !repositoryRe.MatchString(c.Repository) {
		return errors.New("ci.repository inválido")
	}
	if c.ToolRepository != "" && !repositoryRe.MatchString(c.ToolRepository) {
		return errors.New("ci.tool_repository inválido")
	}
	if c.Destination == "" {
		c.Destination = "r2"
	}
	if d, ok := r.Destinations[c.Destination]; !ok || d.PublicURL == "" {
		return fmt.Errorf("ci.destination %q no existe", c.Destination)
	}
	if c.StateBranch == "" {
		c.StateBranch = "debfactory/state"
	}
	if c.StateBranch == "main" {
		return errors.New("ci.state_branch no puede ser main")
	}
	if c.StatePRMaxAge == 0 {
		c.StatePRMaxAge = Duration(7 * 24 * time.Hour)
	}
	if c.FailureLabel == "" {
		c.FailureLabel = "pipeline-failure"
	}
	if c.Labels.CI == "" {
		c.Labels.CI = "debfactory-pr"
	}
	if c.Labels.Pub == "" {
		c.Labels.Pub = "debfactory-main"
	}
	if c.Labels.CI == c.Labels.Pub {
		return errors.New("las etiquetas de los dos niveles deben ser distintas")
	}
	return nil
}

// RequireRepository falla si ci.repository está vacío.
func (c *CI) RequireRepository() error {
	if c.Repository == "" {
		return report.New(report.Config, "ci.repository está vacío en "+MainFile)
	}
	return nil
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
	if err := c.Repo.validate(); err != nil {
		return err
	}
	if err := c.CI.validate(&c.Repo); err != nil {
		return err
	}
	return c.Watch.validate(c.CI.StateBranch)
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
