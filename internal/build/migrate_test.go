package build

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"debfactory/internal/manifest"
	"debfactory/internal/report"
)

// TestMigrateLayout (specs/009, research D21): el paquete pasa a
// packages/<p>/, se sigue cargando igual y construye el mismo .deb.
func TestMigrateLayout(t *testing.T) {
	h := newHarness(t)
	h.loadOK()
	ctx := context.Background()
	if err := h.env.Build(ctx, "example", Options{Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(h.root, "dist", "example_1.0.0-0~tepache1_amd64.deb"))
	var out bytes.Buffer
	if err := MigrateLayout(ctx, h.root, &report.Printer{Out: &out, Err: &out}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"packages/example/example.yaml", "packages/example/example.lock.yaml"} {
		if _, err := os.Stat(filepath.Join(h.root, p)); err != nil {
			t.Fatalf("falta %s: %v", p, err)
		}
	}
	if names, _ := ListPackages(h.root); len(names) != 1 || names[0] != "example" {
		t.Fatalf("ListPackages = %v", names)
	}
	os.RemoveAll(filepath.Join(h.root, "dist"))
	if err := h.env.Build(ctx, "example", Options{Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(h.root, "dist", "example_1.0.0-0~tepache1_amd64.deb"))
	if !bytes.Equal(before, after) {
		t.Error("el .deb cambió tras migrar")
	}
	// Las dos ubicaciones a la vez son un error.
	data, _ := os.ReadFile(filepath.Join(h.root, "packages/example/example.yaml"))
	os.WriteFile(filepath.Join(h.root, "packages/example.yaml"), data, 0o644)
	if _, err := manifest.Load(h.root, "example"); err == nil {
		t.Error("se esperaba error con las dos ubicaciones")
	}
}
