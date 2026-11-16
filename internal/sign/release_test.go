package sign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAddValidUntil: el campo va justo después de Date: y el resto del
// Release queda igual byte a byte (FR-030 de la fase 003).
func TestAddValidUntil(t *testing.T) {
	orig, err := os.ReadFile("../../testdata/repo/release/Release")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Release")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	signed := time.Date(2026, 11, 13, 15, 47, 39, 0, time.UTC)
	for range 2 { // idempotente: la segunda vez sustituye, no duplica
		if err := AddValidUntil(path, signed, 30); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	line := "Valid-Until: Sun, 13 Dec 2026 15:47:39 UTC\n"
	if strings.Count(string(got), "Valid-Until:") != 1 {
		t.Fatalf("Valid-Until repetido:\n%s", got)
	}
	want := strings.Replace(string(orig), "UTC\nArchitectures", "UTC\n"+line+"Architectures", 1)
	if string(got) != want {
		t.Errorf("Release =\n%s\nse esperaba\n%s", got, want)
	}
	vu, err := ValidUntil(got)
	if err != nil || !vu.Equal(signed.AddDate(0, 0, 30)) {
		t.Errorf("ValidUntil = %v, %v", vu, err)
	}
	if vu, _ := ValidUntil(orig); !vu.IsZero() {
		t.Errorf("ValidUntil sin campo = %v", vu)
	}
	if err := AddValidUntil(filepath.Join(t.TempDir(), "x"), signed, 30); err == nil {
		t.Error("se esperaba error sin archivo")
	}
}
