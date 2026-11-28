package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/testutil"
)

func realLicense(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), "testdata/licenses", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestLicenseChange: research D8 con los textos reales de los paquetes.
func TestLicenseChange(t *testing.T) {
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	mit := realLicense(t, "bob-v4.3.0-LICENSE")
	mitOtherYear := []byte(strings.Replace(testutil.MITText, "2026", "2027", 1))
	apache := realLicense(t, "buf-v1.75.0-LICENSE")
	gpl := realLicense(t, "fnm-v1.39.0-LICENSE")
	cases := []struct {
		name, declared string
		text           []byte
		kind           LicenseKind
		spdx, reason   string
	}{
		{"misma licencia", "MIT", mit, LicenseSame, "", ""},
		{"mismo texto con otro año", "MIT", mitOtherYear, LicenseSame, "", ""},
		{"GPL declarada -only", "GPL-3.0-only", gpl, LicenseSame, "", ""},
		{"cambio a Apache-2.0 permitido", "MIT", apache, LicenseChanged, "Apache-2.0", ""},
		{"cambio a WTFPL fuera de la allowlist", "MIT", []byte(testutil.WTFPLText), LicenseBlocked, "", "licencia WTFPL fuera de config/licenses.yaml"},
		{"cambio a GPL sin -only/-or-later", "MIT", gpl, LicenseBlocked, "", "no distingue -only de -or-later"},
		{"no identificable", "MIT", []byte("Todos los derechos reservados.\n"), LicenseBlocked, "", "no identificable"},
		{"varias licencias", "MIT", append(append([]byte{}, mit...), apache...), LicenseBlocked, "", "varias licencias"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := LicenseChange(c.text, c.declared, cfg)
			if got.Kind != c.kind || got.SPDX != c.spdx || !strings.Contains(got.Reason, c.reason) {
				t.Fatalf("= %+v; se esperaba %s %q %q", got, c.kind, c.spdx, c.reason)
			}
			if got.Declared != c.declared || (c.kind != LicenseBlocked && got.Coverage < 90) {
				t.Errorf("datos incompletos: %+v", got)
			}
		})
	}
}
