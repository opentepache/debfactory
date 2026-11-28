package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildExcludedArch (specs/009, T041): con exclude.arches.arm64 solo se
// construye amd64 y --arch arm64 se rechaza.
func TestBuildExcludedArch(t *testing.T) {
	h := newHarness(t)
	h.loadOK()
	p := filepath.Join(h.root, "packages", "example.yaml")
	b, _ := os.ReadFile(p)
	s := strings.Replace(string(b), "upstream:", "exclude:\n  arches:\n    arm64: no cabe en el runner estandar\nupstream:", 1)
	os.WriteFile(p, []byte(s), 0o644)
	if err := h.env.Build(context.Background(), "example", Options{}); err != nil {
		t.Fatal(err)
	}
	for _, f := range h.distFiles() {
		if strings.Contains(f, "arm64") {
			t.Errorf("se construyó una arquitectura excluida: %s", f)
		}
	}
	if len(h.distFiles()) == 0 {
		t.Fatal("no se construyó amd64")
	}
	err := h.env.Build(context.Background(), "example", Options{Arch: "arm64"})
	if err == nil || !strings.Contains(err.Error(), "excluye la arquitectura arm64") {
		t.Fatalf("--arch arm64: %v", err)
	}
}
