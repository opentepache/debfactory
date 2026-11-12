package license

import (
	"os"
	"slices"
	"testing"
)

// TestDetectRealTexts es el spike de research.md §3 riesgo 4: licensecheck
// identifica los textos reales de los paquetes iniciales, al tag exacto, con
// cobertura suficiente.
func TestDetectRealTexts(t *testing.T) {
	cases := map[string]string{
		"fnm-v1.39.0-LICENSE":  "GPL-3.0",
		"bob-v4.3.0-LICENSE":   "MIT",
		"buf-v1.75.0-LICENSE":  "Apache-2.0",
		"aqua-v2.66.1-LICENSE": "MIT",
	}
	for file, want := range cases {
		text, err := os.ReadFile("../../testdata/licenses/" + file)
		if err != nil {
			t.Fatal(err)
		}
		d := Detect(text)
		t.Logf("%s: %v cobertura %.1f %%", file, d.IDs, d.Coverage)
		if !slices.Contains(d.IDs, want) || len(d.IDs) != 1 {
			t.Errorf("%s: detectado %v; se esperaba solo %s", file, d.IDs, want)
		}
		if d.Coverage < MinCoverage {
			t.Errorf("%s: cobertura %.1f %% < %v %%", file, d.Coverage, MinCoverage)
		}
	}
}
