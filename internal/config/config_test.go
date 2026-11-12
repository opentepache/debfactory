package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRepoConfig(t *testing.T) {
	c, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range c.Suites {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "bookworm,trixie,sid,noble,resolute" {
		t.Errorf("orden de suites = %s", got)
	}
	if s, _ := c.Suite("noble"); s.Image != "ubuntu:noble" || s.Level2Suffix != "u2404" {
		t.Errorf("noble = %+v", s)
	}
	if c.Lintian.Overridable("embedded-library") || c.Lintian.Overridable("copyright-foo") {
		t.Error("etiquetas protegidas marcadas como silenciables")
	}
	if !c.Lintian.Overridable("statically-linked-binary") {
		t.Error("statically-linked-binary debería poder silenciarse")
	}
	if !c.LicenseAllowed("MIT") || c.LicenseAllowed("BUSL-1.1") {
		t.Error("allowlist incorrecta")
	}
}

func TestLoadRejects(t *testing.T) {
	base, err := os.ReadFile("../../" + MainFile)
	if err != nil {
		t.Fatal(err)
	}
	lic, err := os.ReadFile("../../" + LicensesFile)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ old, new, want string }{
		"regex inválida":     {`"^dep5-"`, `"^dep5-("`, "never_override_patterns[2]: regex inválida"},
		"marcador":           {`libgcc-s1`, `libgcc-s1 (>= {gcc})`, "marcador desconocido {gcc}"},
		"campo desconocido":  {`completions_image:`, "colour: red\ncompletions_image:", "field colour not found"},
		"arquitecturas":      {`arches: [amd64, arm64]`, `arches: [amd64]`, "arches: debe ser [amd64, arm64]"},
		"imagen vacía":       {`image: "debian:sid"`, `image: ""`, "suites.sid.image: no puede estar vacío"},
		"suite desconocida":  {`level2_suffix: deb12}`, `level2_suffix: deb12, extra: x}`, `suites.bookworm: propiedad desconocida "extra"`},
		"fail_on incorrecto": {`fail_on: error`, `fail_on: always`, "lintian.fail_on"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(string(base), tc.old) {
				t.Fatalf("el caso no aplica: falta %q", tc.old)
			}
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, MainFile), strings.Replace(string(base), tc.old, tc.new, 1))
			mustWrite(t, filepath.Join(root, LicensesFile), string(lic))
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; se esperaba que contuviera %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), ": validate: config/debfactory.yaml: ") {
				t.Errorf("formato inesperado: %v", err)
			}
		})
	}
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
