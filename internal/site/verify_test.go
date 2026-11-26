package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerify(t *testing.T) {
	f := newFixture(t)
	m := f.load(t)
	dir := genDir(t, m)
	if err := Verify(m, dir); err != nil {
		t.Fatalf("coherente: %v", err)
	}

	// Falta bob 4.3.0 en trixie/arm64 y sobra un paquete en noble/amd64.
	p := filepath.Join(dir, "dists", "trixie", "main", "binary-arm64", "Packages")
	data, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(data), "Package: bob\nVersion: 4.3.0-0~tepache1\n", "Package: nobob\nVersion: 4.3.0-0~tepache1\n", 1)), 0o644)
	q := filepath.Join(dir, "dists", "noble", "main", "binary-amd64", "Packages")
	extra, _ := os.ReadFile(q)
	os.WriteFile(q, append(extra, "Package: intruso\nVersion: 9.9-0~tepache1\n\n"...), 0o644)

	err := Verify(m, dir)
	if err == nil {
		t.Fatal("se esperaba error")
	}
	for _, want := range []string{
		"debfactory: bob 4.3.0-0~tepache1 -: site: el estado la publica en trixie/arm64 pero no está en dists/trixie/main/binary-arm64/Packages",
		"debfactory: intruso 9.9-0~tepache1 -: site: está en dists/noble/main/binary-amd64/Packages pero el estado no la publica en noble/amd64",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("falta %q en:\n%v", want, err)
		}
	}
}

func TestVerifyMissingIndex(t *testing.T) {
	f := newFixture(t)
	m := f.load(t)
	dir := genDir(t, m)
	os.Remove(filepath.Join(dir, "dists", "sid", "main", "binary-amd64", "Packages"))
	if err := Verify(m, dir); err == nil || !strings.Contains(err.Error(), "falta dists/sid/main/binary-amd64/Packages") {
		t.Errorf("error = %v", err)
	}
}
