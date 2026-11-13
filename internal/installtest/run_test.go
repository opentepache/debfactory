//go:build integration

// Prueba de integración: requiere Docker. go test -tags integration ./internal/installtest/
package installtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"debfactory/internal/assemble"
	"debfactory/internal/config"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// fixtureDeb construye en root/dist el .deb del ejemplo mínimo (el mismo ELF
// de juguete que testdata/fixtures/ok) y su build.json.
func fixtureDeb(t *testing.T, root string) *manifest.Manifest {
	t.Helper()
	work := t.TempDir()
	bin := filepath.Join(work, "example")
	os.WriteFile(bin, testutil.TinyELF("amd64", "example 1.0.0\n"), 0o755)
	cr := filepath.Join(work, "copyright")
	os.WriteFile(cr, []byte("Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\n"), 0o644)
	m := &manifest.Manifest{Name: "example", Version: "1.0.0", Revision: 1, Tag: "v{{version}}",
		ArchMap: map[string]string{"amd64": "x86_64", "arm64": "aarch64"}}
	plan := &pkg.Plan{Name: "example", Version: "1.0.0", Revision: 1, Arch: "amd64", Maintainer: "Test <t@example.com>",
		Summary: "example tool", Description: "Integration test fixture.", Section: "devel",
		Files: []assemble.File{{Dst: "/usr/bin/example", Src: bin, Mode: 0o755},
			{Dst: "/usr/share/doc/example/copyright", Src: cr, Mode: 0o644}},
		MTime: time.Unix(1767225600, 0)}
	dist := filepath.Join(root, "dist")
	deb, err := pkg.Build(plan, dist)
	if err != nil {
		t.Fatal(err)
	}
	info := &pkg.BuildInfo{Lintian: "pass", TestCommand: "example --version"}
	data, _ := info.Marshal()
	os.WriteFile(deb[:len(deb)-len(".deb")]+".build.json", data, 0o644)
	return m
}

// TestForcedFailure (US3 escenario 3): con un test.expect incorrecto el
// resultado global es fail, se nombra la suite y el stage version, y la orden
// devuelve un error de etapa test (código de salida 1).
func TestForcedFailure(t *testing.T) {
	cfg, err := config.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	m := fixtureDeb(t, root)
	out := &report.Printer{Out: os.Stdout, Err: os.Stderr}
	ctx := context.Background()

	m.Test.Expect = "{{version}}"
	rep, err := TestPackage(ctx, root, cfg, m, "amd64", []string{"trixie"}, out)
	if err != nil || rep.Overall != "pass" {
		t.Fatalf("caso correcto: %v %+v", err, rep)
	}

	m.Test.Expect = "9.9.9"
	rep, err = TestPackage(ctx, root, cfg, m, "amd64", []string{"trixie"}, out)
	var re *report.Error
	if !errors.As(err, &re) || re.Stage != report.Test {
		t.Fatalf("error = %v", err)
	}
	if rep.Overall != "fail" || len(rep.Results) != 1 || rep.Results[0].Suite != "trixie" ||
		rep.Results[0].Status != "fail" || rep.Results[0].Stage != StageVersion {
		t.Errorf("informe = %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "example_1.0.0-0~tepache1_amd64.test.json")); err != nil {
		t.Error("falta el test.json")
	}
}
