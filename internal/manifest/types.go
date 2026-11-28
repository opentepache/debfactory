// Package manifest carga, valida y resuelve los manifiestos de paquete
// (packages/<name>.yaml). Los structs reflejan 1:1 schema/manifest.v1.schema.json.
package manifest

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Manifest es un manifiesto de paquete.
type Manifest struct {
	Schema            int                `yaml:"schema"`
	Name              string             `yaml:"name"`
	Version           string             `yaml:"version"`
	Revision          int                `yaml:"revision"` // N en <upstream>-0~tepache<N>
	Level             int                `yaml:"level"`
	Summary           string             `yaml:"summary"`
	Description       string             `yaml:"description"`
	Homepage          string             `yaml:"homepage"`
	Section           string             `yaml:"section"`
	Upstream          string             `yaml:"upstream"`
	Tag               string             `yaml:"tag"`
	License           License            `yaml:"license"`
	Value             string             `yaml:"value"`
	Justification     string             `yaml:"justification"` // nivel 2 (constitución II)
	Source            Source             `yaml:"source"`        // nivel 2
	BuildOpts         BuildOpts          `yaml:"build"`         // nivel 2
	Signing           *Signing           `yaml:"signing"`       // llave de upstream fijada (constitución III)
	Toolchain         *Toolchain         `yaml:"toolchain"`     // nivel 2: toolchain de upstream
	Deps              *Deps              `yaml:"deps"`          // nivel 2: dependencias del lenguaje
	LicenseExceptions []LicenseException `yaml:"license_exceptions"`
	ArchMap           map[string]string  `yaml:"arch_map"`
	Artifact          Artifact           `yaml:"artifact"`
	Checksums         *Checksums         `yaml:"checksums"`
	Install           []InstallItem      `yaml:"install"`
	Completions       Completions        `yaml:"completions"`
	Man               []string           `yaml:"man"`
	Depends           []string           `yaml:"depends"`
	Recommends        []string           `yaml:"recommends"`
	Conflicts         []string           `yaml:"conflicts"`
	Replaces          []string           `yaml:"replaces"`
	Provides          []string           `yaml:"provides"`
	Exclude           Exclude            `yaml:"exclude"`
	Test              Test               `yaml:"test"`
	LintianOverrides  []LintianOverride  `yaml:"lintian_overrides"`
	Watch             Watch              `yaml:"watch"`
	I18n              I18n               `yaml:"i18n"`

	// File es la ruta del manifiesto relativa a la raíz (packages/<name>.yaml).
	File string `yaml:"-"`
}

// License declara la licencia esperada y la fuente de su texto.
type License struct {
	SPDX      string      `yaml:"spdx"`
	File      LicenseFile `yaml:"file"`
	Copyright string      `yaml:"copyright"`
}

// LicenseFile tiene exactamente uno de sus campos.
type LicenseFile struct {
	Archive string `yaml:"archive"` // ruta dentro del artefacto
	Repo    string `yaml:"repo"`    // ruta en el repo upstream, al tag exacto
}

// Artifact es el asset oficial del release.
type Artifact struct {
	URL    string `yaml:"url"`
	Format string `yaml:"format"` // binary | zip | tar.gz
}

// Source es el tarball del código fuente de un paquete de nivel 2
// (specs/008-source-builds, data-model §1).
type Source struct {
	URL        string      `yaml:"url"`
	Format     string      `yaml:"format"` // tar.gz | tar.xz | tar.bz2
	ExtraFiles []ExtraFile `yaml:"extra_files"`
}

// ExtraFile es un archivo del tarball de release que no está en el tag de git
// (o difiere), declarado por ruta exacta con su motivo (FR-014).
type ExtraFile struct {
	Path   string `yaml:"path"`
	Reason string `yaml:"reason"`
}

// Signing fija la llave o identidad con la que firma upstream (FR-015);
// exactamente uno de sus campos.
type Signing struct {
	Minisign *MinisignSig `yaml:"minisign"`
	GPG      *GPGSig      `yaml:"gpg"`
	Sigstore *SigstoreSig `yaml:"sigstore"`
}

// MinisignSig es una firma minisign: la llave pública y la URL de la firma.
type MinisignSig struct {
	Key  string `yaml:"key"`
	File string `yaml:"file"`
}

// GPGSig es una firma OpenPGP separada.
type GPGSig struct {
	Fingerprint string `yaml:"fingerprint"`
	KeyFile     string `yaml:"key_file"` // relativo a packages/<p>/
	File        string `yaml:"file"`
}

// SigstoreSig es un bundle de sigstore (cosign) de firma sin llave.
type SigstoreSig struct {
	Identity string `yaml:"identity"` // regex de la identidad del certificado
	Issuer   string `yaml:"issuer"`
	Bundle   string `yaml:"bundle"`
}

// Kind es el tipo de firma: minisign, gpg o sigstore.
func (s *Signing) Kind() string {
	switch {
	case s == nil:
		return ""
	case s.Minisign != nil:
		return "minisign"
	case s.GPG != nil:
		return "gpg"
	case s.Sigstore != nil:
		return "sigstore"
	}
	return ""
}

// URL es la plantilla de la URL de la firma o del bundle.
func (s *Signing) URL() string {
	switch s.Kind() {
	case "minisign":
		return s.Minisign.File
	case "gpg":
		return s.GPG.File
	case "sigstore":
		return s.Sigstore.Bundle
	}
	return ""
}

// Toolchain es un toolchain oficial de upstream para nivel 2 (FR-001).
type Toolchain struct {
	Name    string            `yaml:"name"`
	Version string            `yaml:"version"`
	URL     string            `yaml:"url"` // plantilla: {{version}}, {{arch}}
	ArchMap map[string]string `yaml:"arch_map"`
	Signing *Signing          `yaml:"signing"`
}

// RenderURL devuelve la URL del toolchain para una arquitectura Debian.
func (t *Toolchain) RenderURL(tmpl, arch string) (string, error) {
	token, ok := t.ArchMap[arch]
	if !ok {
		return "", fmt.Errorf("toolchain.arch_map: falta %s", arch)
	}
	return Render(tmpl, Vars{"version": t.Version, "arch": token})
}

// Deps describe las dependencias del gestor de paquetes del lenguaje (FR-002).
type Deps struct {
	Manager      string   `yaml:"manager"` // zig
	ExcludePaths []string `yaml:"exclude_paths"`
	// Skip son dependencias (nombre en build.zig.zon) que el build del
	// paquete no usa, con su motivo: no se bajan ni van al fuente.
	Skip map[string]string `yaml:"skip"`
}

// PackageExceptionLicenses son las licencias de la excepción del propio
// paquete (sin dependency), que el gate suma a la allowlist solo para él.
func (m *Manifest) PackageExceptionLicenses() []string {
	for _, x := range m.LicenseExceptions {
		if x.Dependency == "" {
			return x.Licenses
		}
	}
	return nil
}

// LicenseException es una excepción de licencia que vale solo para este
// paquete (constitución IV, 1.12.0): para el propio paquete (Dependency
// vacío) o para una dependencia del lenguaje de su fuente.
type LicenseException struct {
	Dependency string   `yaml:"dependency"` // nombre en build.zig.zon; vacío = el paquete
	Licenses   []string `yaml:"licenses"`   // SPDX permitidas por la excepción
	File       string   `yaml:"file"`       // texto de licencia dentro de la dependencia
	RepoFile   string   `yaml:"repo_file"`  // texto guardado en packages/<p>/ si upstream no lo incluye
	Reason     string   `yaml:"reason"`
}

// BuildOpts son los ajustes de compilación de nivel 2.
type BuildOpts struct {
	Tmpfs string `yaml:"tmpfs"` // p. ej. 8g; vacío = level2.tmpfs de la configuración
}

// Checksums es el archivo de checksums publicado por upstream.
type Checksums struct {
	URL    string `yaml:"url"`
	Format string `yaml:"format"` // sha256sum
}

// InstallItem es un archivo (o glob) a instalar.
type InstallItem struct {
	Src  string `yaml:"src"`
	Dst  string `yaml:"dst"`
	Mode string `yaml:"mode"`
}

// Completion tiene exactamente uno de sus campos.
type Completion struct {
	File     string `yaml:"file"`
	Generate string `yaml:"generate"`
}

// Completions por shell.
type Completions struct {
	Bash *Completion `yaml:"bash"`
	Zsh  *Completion `yaml:"zsh"`
	Fish *Completion `yaml:"fish"`
}

// ShellCompletion es una completion con su shell.
type ShellCompletion struct {
	Shell string
	*Completion
}

// List devuelve las completions declaradas en orden fijo: bash, zsh, fish.
func (c Completions) List() []ShellCompletion {
	var out []ShellCompletion
	for _, sc := range []ShellCompletion{{"bash", c.Bash}, {"zsh", c.Zsh}, {"fish", c.Fish}} {
		if sc.Completion != nil {
			out = append(out, sc)
		}
	}
	return out
}

// Exclude son las suites y arquitecturas excluidas, cada una con su motivo.
type Exclude struct {
	Suites map[string]string `yaml:"suites"`
	Arches map[string]string `yaml:"arches"` // constitución 1.9.0
}

// Arches devuelve las arquitecturas de arches que el manifiesto no excluye,
// en su orden.
func (m *Manifest) Arches(arches []string) []string {
	var out []string
	for _, a := range arches {
		if _, excluded := m.Exclude.Arches[a]; !excluded {
			out = append(out, a)
		}
	}
	return out
}

// Test es la orden de comprobación de la prueba de instalación.
type Test struct {
	Command string `yaml:"command"`
	Expect  string `yaml:"expect"`
}

// Watch configura el detector de versiones para este paquete
// (specs/005-upstream-release-watch, data-model.md §1).
type Watch struct {
	Enabled        *bool  `yaml:"enabled"`         // false excluye el paquete (por defecto true)
	AutoMerge      bool   `yaml:"auto_merge"`      // FR-013; por defecto false
	PrereleaseFlag string `yaml:"prerelease_flag"` // trust | ignore (FR-003a); por defecto trust
}

// I18n son las traducciones que solo usa el sitio web (specs/006-repo-website,
// contracts/config.md). summary y description del manifiesto son los del .deb,
// en inglés.
type I18n struct {
	ES *Text `yaml:"es"`
}

// Text es un resumen y una descripción traducidos; los dos son opcionales.
type Text struct {
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
}

// ReservedNames son nombres de paquete que chocan con rutas del sitio web
// (specs/006-repo-website, research D3).
var ReservedNames = []string{"assets", "es"}

// Watched indica si el detector revisa el paquete.
func (w Watch) Watched() bool { return w.Enabled == nil || *w.Enabled }

// IgnorePrereleaseFlag indica que la marca de pre-release de upstream no es
// fiable para este paquete. Nunca desactiva el filtro de marcadores
// preliminares en la versión (constitución, Alcance).
func (w Watch) IgnorePrereleaseFlag() bool { return w.PrereleaseFlag == "ignore" }

// LintianOverride silencia una etiqueta con motivo (constitución VII).
type LintianOverride struct {
	Tag    string `yaml:"tag"`
	Path   string `yaml:"path"`
	Reason string `yaml:"reason"`
}

// ExtraHosts son los orígenes fuera de GitHub que declara el manifiesto: el
// sitio de releases de upstream (nivel 2 con firma fijada), el del toolchain
// y los de las firmas (specs/009, research D1).
func (m *Manifest) ExtraHosts() []string {
	var out []string
	add := func(u string) {
		rest, ok := strings.CutPrefix(u, "https://")
		if !ok {
			return
		}
		h, _, _ := strings.Cut(rest, "/")
		h = strings.ToLower(h)
		if h == "" || h == "github.com" || h == "raw.githubusercontent.com" {
			return
		}
		for _, x := range out {
			if x == h {
				return
			}
		}
		out = append(out, h)
	}
	if m.Level2() {
		add(m.Source.URL)
	}
	if m.Signing != nil {
		add(m.Signing.URL())
	}
	if m.Toolchain != nil {
		add(m.Toolchain.URL)
		if m.Toolchain.Signing != nil {
			add(m.Toolchain.Signing.URL())
		}
	}
	return out
}

// Level2 indica que el paquete se compila desde el código fuente.
func (m *Manifest) Level2() bool { return m.Level == 2 }

// DebianDir es la carpeta debian/ de un paquete de nivel 2, relativa a la raíz.
func (m *Manifest) DebianDir() string { return filepath.Join("packages", m.Name, "debian") }

// OrigName es el nombre del tarball original en el paquete fuente:
// <name>_<upstream>.orig.tar.<ext> (research D6).
func (m *Manifest) OrigName() string {
	return fmt.Sprintf("%s_%s.orig.%s", m.Name, m.Version, m.Source.Format)
}

// SuiteVersion es la versión Debian de un paquete de nivel 2 en una suite:
// <upstream>-0~tepache<N>+<level2_suffix> (constitución VI).
func (m *Manifest) SuiteVersion(suffix string) string { return m.DebVersion() + "+" + suffix }

// DebVersion es la versión Debian: <upstream>-0~tepache<N> (constitución VI).
func (m *Manifest) DebVersion() string {
	return fmt.Sprintf("%s-0~tepache%d", m.Version, m.Revision)
}

// DebBase es el nombre de los archivos de salida sin extensión:
// <name>_<upstream>-0~tepache<N>_<arch>.
func (m *Manifest) DebBase(arch string) string {
	return fmt.Sprintf("%s_%s_%s", m.Name, m.DebVersion(), arch)
}

// FileName es el nombre del manifiesto sin directorio ni extensión.
func FileName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".yaml")
}
