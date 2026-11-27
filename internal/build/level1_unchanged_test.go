package build

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"debfactory/internal/fetch"
)

// TestLevel1Unchanged (SC-004, FR-014; specs/008-source-builds): con el
// nivel 2 en la herramienta, el paquete de ejemplo de nivel 1 da el mismo
// .deb y el mismo build.json que antes de la fase 008 (valores obtenidos con
// la herramienta en 7cdb8e1). Si cambia a propósito el formato de nivel 1,
// esta prueba lo hace visible.
func TestLevel1Unchanged(t *testing.T) {
	golden := map[string][2]string{ // arch -> {.deb, build.json}
		"amd64": {"b99494af2443d85c48a8aa4d9481f5a3f4370374e8c5b8ee10abd936853f048c",
			"d2369a2e307dfb475bb9c83a3b2a06120a82b3a395346bdac9b6c35639b622be"},
		"arm64": {"2e52ed1d49a67cd8b77695f6be87dc9aeab17bff3799744b8c710809aa17101e",
			"f9fb285fd1c06d0a8c02f2898384c1b241a2e091e346f4a19a4759ef7267917d"},
	}
	h := newHarness(t)
	h.loadOK()
	if err := h.env.Build(context.Background(), "example", Options{}); err != nil {
		t.Fatal(err)
	}
	for arch, want := range golden {
		base := filepath.Join(h.root, "dist", "example_1.0.0-0~tepache1_"+arch)
		deb, err := fetch.SHA256File(base + ".deb")
		if err != nil {
			t.Fatal(err)
		}
		bj, err := os.ReadFile(base + ".build.json")
		if err != nil {
			t.Fatal(err)
		}
		if deb != want[0] || fetch.SHA256(bj) != want[1] {
			t.Errorf("%s: .deb %s y build.json %s; antes de la fase 008: %s y %s\n%s", arch, deb, fetch.SHA256(bj),
				want[0], want[1], bj)
		}
	}
}
