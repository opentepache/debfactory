package manifest

import (
	"errors"
	"os"
	"strings"
	"testing"

	"debfactory/internal/report"
)

const examples = "../../specs/001-manifest-deb-builder/contracts/examples/"

func readExample(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(examples + name + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseExampleDefaults(t *testing.T) {
	m, err := Parse([]byte(readExample(t, "fnm")), "packages/fnm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Tag != "v{{version}}" || m.Section != "devel" || m.Test.Expect != "{{version}}" {
		t.Errorf("defaults = %q %q %q", m.Tag, m.Section, m.Test.Expect)
	}
	if m.DebVersion() != "1.39.0-0~tepache1" || m.DebBase("arm64") != "fnm_1.39.0-0~tepache1_arm64" {
		t.Errorf("versión = %s, base = %s", m.DebVersion(), m.DebBase("arm64"))
	}
	if m.License.File.Repo != "LICENSE" || len(m.Completions.List()) != 3 || len(m.LintianOverrides) != 2 {
		t.Errorf("decodificación incompleta: %+v", m)
	}
	// Sin bloque watch: el detector lo revisa, sin auto-merge y confiando en
	// la marca de pre-release (specs/005-upstream-release-watch, data-model.md §1).
	if !m.Watch.Watched() || m.Watch.AutoMerge || m.Watch.PrereleaseFlag != "trust" || m.Watch.IgnorePrereleaseFlag() {
		t.Errorf("watch por defecto = %+v", m.Watch)
	}
}

func TestParseWatch(t *testing.T) {
	src := readExample(t, "fnm") + "watch:\n  enabled: false\n  auto_merge: true\n  prerelease_flag: ignore\n"
	m, err := Parse([]byte(src), "packages/fnm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Watch.Watched() || !m.Watch.AutoMerge || !m.Watch.IgnorePrereleaseFlag() {
		t.Errorf("watch = %+v", m.Watch)
	}
}

func TestParseI18n(t *testing.T) {
	src := readExample(t, "fnm") + "i18n:\n  es:\n    summary: gestor de versiones de Node.js\n"
	m, err := Parse([]byte(src), "packages/fnm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m.I18n.ES == nil || m.I18n.ES.Summary != "gestor de versiones de Node.js" || m.I18n.ES.Description != "" {
		t.Errorf("i18n = %+v", m.I18n.ES)
	}
	m, err = Parse([]byte(readExample(t, "fnm")), "packages/fnm.yaml")
	if err != nil || m.I18n.ES != nil {
		t.Errorf("sin i18n = %+v, %v", m.I18n, err)
	}
}

// TestParseReserved: los nombres que chocan con rutas del sitio web se
// rechazan en el schema y en Check (006 research D3).
func TestParseReserved(t *testing.T) {
	for _, name := range ReservedNames {
		src := strings.Replace(readExample(t, "fnm"), "name: fnm", "name: "+name, 1)
		_, err := Parse([]byte(src), "packages/"+name+".yaml")
		if err == nil || !strings.Contains(err.Error(), "name: valor reservado") {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

// TestParseErrors comprueba la traducción al español de cada tipo de error
// del schema y la forma "packages/<f>.yaml: <campo>: <mensaje>".
func TestParseErrors(t *testing.T) {
	fnm := readExample(t, "fnm")
	cases := map[string]struct{ old, new, want string }{
		"required":     {"  file: {repo: LICENSE}\n", "", `packages/fnm.yaml: license: falta la propiedad "file"`},
		"additional":   {"revision: 1\n", "revision: 1\nhomepgae: x\n", `packages/fnm.yaml: propiedad desconocida "homepgae"`},
		"enum":         {"format: zip", "format: rar", "packages/fnm.yaml: artifact.format: valor no permitido (se admite: binary, zip, tar.gz)"},
		"level enum":   {"level: 1", "level: 3", "packages/fnm.yaml: level: valor no permitido (se admite: 1, 2)"},
		"pattern":      {"upstream: Schniz/fnm", "upstream: Schniz", "packages/fnm.yaml: upstream: no cumple el formato"},
		"type":         {"revision: 1", "revision: uno", "packages/fnm.yaml: revision: tipo incorrecto (se esperaba integer, hay string)"},
		"minimum":      {"revision: 1", "revision: 0", "packages/fnm.yaml: revision: demasiado pequeño (mínimo 1)"},
		"minLength":    {"summary: fast and simple Node.js version manager", "summary: ab", "packages/fnm.yaml: summary: demasiado corto (mínimo 3 caracteres)"},
		"exactlyOne":   {"file: {repo: LICENSE}", "file: {repo: LICENSE, archive: LICENSE}", "packages/fnm.yaml: license.file: debe tener exactamente una de: archive, repo"},
		"array index":  {"{src: fnm, dst: /usr/bin/}", "{src: fnm, dst: /bin/}", "packages/fnm.yaml: install[0].dst: no cumple el formato"},
		"format":       {"homepage: https://github.com/Schniz/fnm", "homepage: https://exa mple.com", "packages/fnm.yaml: homepage: formato inválido"},
		"duplicada":    {"revision: 1\n", "revision: 1\nrevision: 2\n", `clave duplicada "revision"`},
		"YAML roto":    {"revision: 1\n", "revision: [1\n", "YAML inválido"},
		"completions1": {"bash: {generate: fnm completions --shell bash}", "bash: {}", "packages/fnm.yaml: completions.bash: debe tener exactamente una de: file, generate"},
		"watch enum":   {"revision: 1\n", "revision: 1\nwatch: {prerelease_flag: maybe}\n", "packages/fnm.yaml: watch.prerelease_flag: valor no permitido (se admite: trust, ignore)"},
		"watch tipo":   {"revision: 1\n", "revision: 1\nwatch: {auto_merge: si}\n", "packages/fnm.yaml: watch.auto_merge: tipo incorrecto (se esperaba boolean, hay string)"},
		"watch campo":  {"revision: 1\n", "revision: 1\nwatch: {automerge: true}\n", `packages/fnm.yaml: watch: propiedad desconocida "automerge"`},
		"i18n idioma":  {"revision: 1\n", "revision: 1\ni18n: {fr: {summary: gestionnaire}}\n", `packages/fnm.yaml: i18n: propiedad desconocida "fr"`},
		"i18n punto":   {"revision: 1\n", "revision: 1\ni18n: {es: {summary: gestor de versiones.}}\n", "packages/fnm.yaml: i18n.es.summary: no cumple el formato"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(fnm, tc.old) {
				t.Fatalf("el caso no aplica: falta %q", tc.old)
			}
			_, err := Parse([]byte(strings.Replace(fnm, tc.old, tc.new, 1)), "packages/fnm.yaml")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v\nse esperaba que contuviera %q", err, tc.want)
			}
			// Con YAML roto no se puede leer la versión: queda "-".
			if !strings.HasPrefix(err.Error(), "debfactory: fnm ") ||
				!strings.Contains(err.Error(), " -: validate: packages/fnm.yaml: ") {
				t.Errorf("formato inesperado: %v", err)
			}
		})
	}
}

func TestParseReportsAllErrors(t *testing.T) {
	src := strings.Replace(readExample(t, "fnm"), "level: 1", "level: 3\ncolour: red", 1)
	_, err := Parse([]byte(src), "packages/fnm.yaml")
	var list report.Errors
	if !errors.As(err, &list) || len(list) != 2 {
		t.Fatalf("se esperaban 2 errores, hay: %v", err)
	}
}
