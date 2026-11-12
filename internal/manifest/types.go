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
	Schema           int               `yaml:"schema"`
	Name             string            `yaml:"name"`
	Version          string            `yaml:"version"`
	Pav              int               `yaml:"pav"` // N en <upstream>-0~pav<N>
	Level            int               `yaml:"level"`
	Summary          string            `yaml:"summary"`
	Description      string            `yaml:"description"`
	Homepage         string            `yaml:"homepage"`
	Section          string            `yaml:"section"`
	Upstream         string            `yaml:"upstream"`
	Tag              string            `yaml:"tag"`
	License          License           `yaml:"license"`
	Value            string            `yaml:"value"`
	ArchMap          map[string]string `yaml:"arch_map"`
	Artifact         Artifact          `yaml:"artifact"`
	Checksums        *Checksums        `yaml:"checksums"`
	Install          []InstallItem     `yaml:"install"`
	Completions      Completions       `yaml:"completions"`
	Man              []string          `yaml:"man"`
	Depends          []string          `yaml:"depends"`
	Recommends       []string          `yaml:"recommends"`
	Conflicts        []string          `yaml:"conflicts"`
	Replaces         []string          `yaml:"replaces"`
	Provides         []string          `yaml:"provides"`
	Exclude          Exclude           `yaml:"exclude"`
	Test             Test              `yaml:"test"`
	LintianOverrides []LintianOverride `yaml:"lintian_overrides"`

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

// Exclude: solo se pueden excluir suites (constitución, Alcance).
type Exclude struct {
	Suites map[string]string `yaml:"suites"`
}

// Test es la orden de comprobación de la prueba de instalación.
type Test struct {
	Command string `yaml:"command"`
	Expect  string `yaml:"expect"`
}

// LintianOverride silencia una etiqueta con motivo (constitución VII).
type LintianOverride struct {
	Tag    string `yaml:"tag"`
	Path   string `yaml:"path"`
	Reason string `yaml:"reason"`
}

// DebVersion es la versión Debian: <upstream>-0~pav<N> (constitución VI).
func (m *Manifest) DebVersion() string {
	return fmt.Sprintf("%s-0~pav%d", m.Version, m.Pav)
}

// DebBase es el nombre de los archivos de salida sin extensión:
// <name>_<upstream>-0~pav<N>_<arch>.
func (m *Manifest) DebBase(arch string) string {
	return fmt.Sprintf("%s_%s_%s", m.Name, m.DebVersion(), arch)
}

// FileName es el nombre del manifiesto sin directorio ni extensión.
func FileName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".yaml")
}
