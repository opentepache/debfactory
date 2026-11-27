package srcpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/installtest"
)

func input(t *testing.T) Input {
	t.Helper()
	return Input{
		Name: "tepache-hello", Upstream: "1.0.0", Version: "1.0.0-0~tepache1+deb12", Suite: "bookworm",
		Maintainer: "Juan Pavon <pavon.gnu@gmail.com>", Epoch: 1767225600,
		Orig:      "../../testdata/fixtures/level2/tepache-hello-1.0.0.tar.gz",
		OrigName:  "tepache-hello_1.0.0.orig.tar.gz",
		DebianDir: "../../testdata/fixtures/level2/tepache-hello/debian",
	}
}

// TestChangelog: el formato exacto de research D6.
func TestChangelog(t *testing.T) {
	want := "tepache-hello (1.0.0-0~tepache1+deb12) bookworm; urgency=medium\n\n" +
		"  * OpenTepache build of 1.0.0 for bookworm.\n\n" +
		" -- Juan Pavon <pavon.gnu@gmail.com>  Thu, 01 Jan 2026 00:00:00 +0000\n"
	if got := string(Changelog(input(t))); got != want {
		t.Errorf("changelog =\n%s\nwant\n%s", got, want)
	}
}

// TestPrepare: el .orig es el tarball byte a byte y debian/ lleva el
// changelog de la suite; rules conserva el bit de ejecución.
func TestPrepare(t *testing.T) {
	in := input(t)
	dir := t.TempDir()
	if err := Prepare(in, dir); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(in.Orig)
	b, err := os.ReadFile(filepath.Join(dir, in.OrigName))
	if err != nil || string(a) != string(b) {
		t.Fatalf("el .orig no es el tarball de upstream: %v", err)
	}
	cl, _ := os.ReadFile(filepath.Join(dir, "debian/changelog"))
	if !strings.HasPrefix(string(cl), "tepache-hello (1.0.0-0~tepache1+deb12) bookworm;") {
		t.Errorf("changelog = %s", cl)
	}
	st, err := os.Stat(filepath.Join(dir, "debian/rules"))
	if err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Errorf("rules sin bit de ejecución: %v", err)
	}
	if f, _ := os.ReadFile(filepath.Join(dir, "debian/source/format")); strings.TrimSpace(string(f)) != "3.0 (quilt)" {
		t.Errorf("format = %q", f)
	}
	if in.TreeDir() != "tepache-hello-1.0.0" {
		t.Errorf("TreeDir = %s", in.TreeDir())
	}
}

// TestPrepareRejectsFormat: el nivel 2 usa 3.0 (quilt).
func TestPrepareRejectsFormat(t *testing.T) {
	in := input(t)
	src := t.TempDir()
	if err := os.CopyFS(src, os.DirFS(in.DebianDir)); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "source/format"), []byte("3.0 (native)\n"), 0o644)
	in.DebianDir = src
	if err := Prepare(in, t.TempDir()); err == nil || !strings.Contains(err.Error(), "3.0 (quilt)") {
		t.Fatalf("error = %v", err)
	}
}

// TestVersionOrder: el orden de las versiones con sufijo (constitución VI).
func TestVersionOrder(t *testing.T) {
	cases := []struct{ a, op, b string }{
		{"1.2.3-0~tepache1+deb12", "lt", "1.2.3-1"}, // cede ante Debian
		{"1.2.3-0~tepache1+deb12", "lt", "1.2.3-0~tepache1+deb13"},
		{"1.2.3-0~tepache2+deb12", "gt", "1.2.3-0~tepache1+deb13"},
		{"1.2.3-0~tepache1+u2404", "lt", "1.2.3-0~tepache1+u2604"},
		{"1.2.3-0~tepache1+deb13", "gt", "1.2.3-0~tepache1"},
		{"1.2.4-0~tepache1+deb12", "gt", "1.2.3-0~tepache9+debsid"},
	}
	for _, c := range cases {
		ok, err := installtest.CompareVersions(c.a, c.op, c.b)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("se esperaba %s %s %s", c.a, c.op, c.b)
		}
	}
}
