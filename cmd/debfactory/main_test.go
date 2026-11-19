package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

func tempRoot(t *testing.T, manifests ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"config/debfactory.yaml", "config/licenses.yaml"} {
		data, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), f))
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Join(root, "config"), 0o755)
		os.WriteFile(filepath.Join(root, f), data, 0o644)
	}
	os.MkdirAll(filepath.Join(root, "packages"), 0o755)
	for _, m := range manifests {
		data, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), m))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(root, "packages", filepath.Base(m)), data, 0o644)
	}
	return root
}

func TestExitCodes(t *testing.T) {
	root := tempRoot(t, "testdata/negative/missing-license-file.yaml", "testdata/fixtures/ok/example.yaml")
	cases := []struct {
		args []string
		code int
		err  string
	}{
		{[]string{"validate", "--root", root, "example"}, 0, ""},
		{[]string{"validate", "--root", root}, 1, `validate: packages/missing-license-file.yaml: license: falta la propiedad "file"`},
		{[]string{"validate", "--root", root, "nope"}, 1, "packages/nope.yaml: no existe el manifiesto"},
		{[]string{"frob"}, 2, `orden desconocida "frob"`},
		{[]string{"build", "--root", root, "--arch", "x86", "example"}, 2, "--arch"},
		{[]string{"build", "--root", root, "--all", "example"}, 2, "build --all no admite"},
		{[]string{"lock", "--root", root}, 2, "lock necesita exactamente un paquete"},
		{[]string{"validate", "--root", root, "../etc"}, 2, "nombre de paquete inválido"},
		{[]string{"build", "--bogus"}, 2, "flag provided but not defined"},
		{[]string{"version", "x"}, 2, "version no admite argumentos"},
		// ci watch (specs/005-upstream-release-watch, contracts/cli.md).
		{[]string{"ci", "watch", "--root", root, "--package", "../etc"}, 2, "nombre de paquete inválido"},
		{[]string{"ci", "watch", "extra", "--root", root}, 2, "ci watch no admite argumentos posicionales"},
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		cases = append(cases, struct {
			args []string
			code int
			err  string
		}{[]string{"ci", "watch", "--root", root}, 3, "en local usa --dry-run"})
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		code := run(context.Background(), c.args, &out, &errb)
		if code != c.code || !strings.Contains(errb.String(), c.err) {
			t.Errorf("%v: código %d, stderr %q; se esperaba %d y %q", c.args, code, errb.String(), c.code, c.err)
		}
	}
	var out bytes.Buffer
	if code := run(context.Background(), []string{"version"}, &out, io.Discard); code != 0 || out.String() != "dev\n" {
		t.Errorf("version: código %d, salida %q", code, out.String())
	}
	if exitCode(&report.EnvError{Cause: "sin docker"}) != 3 {
		t.Error("un error de entorno debe dar 3")
	}
}
