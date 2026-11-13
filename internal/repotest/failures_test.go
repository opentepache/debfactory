//go:build e2e

package repotest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/sign"
)

// TestE2EFailures provoca los fallos de quickstart §3 y comprueba que el
// repositorio y un cliente siguen en el estado anterior (SC-004).
func TestE2EFailures(t *testing.T) {
	ctx := context.Background()
	e := e2eEnv(t)
	st := publishAll(t, ctx, e)
	serveCurrent(t, ctx, e)
	blocks := guide(t, e)
	trixie, _ := e.Cfg.Suite("trixie")
	c, err := StartClient(ctx, trixie, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	addRepo(t, ctx, c, e, blocks)

	cur := packagesIn(st, "trixie")["fnm"]
	next := strings.Replace(cur, "~tepache1", "~tepache2", 1)
	var all []string
	for _, s := range e.Cfg.Suites {
		all = append(all, s.Name)
	}
	stateSHA := func() string { s, _ := repo.ReadState(e.Root); h, _ := s.SHA256(); return h }
	before, _ := repo.Current(e.DataDir)
	beforeSHA := stateSHA()

	cases := []struct {
		name  string
		setup func(t *testing.T) func()
		want  string
		code3 bool
	}{
		{"falta el .deb arm64", func(t *testing.T) func() {
			os.Remove(filepath.Join(e.Root, "dist", repo.DebFile("fnm", next, "arm64")))
			return func() {}
		}, "inputs: falta dist/fnm_" + next + "_arm64.deb", false},
		{"prueba no aprobada", func(t *testing.T) func() {
			editReport(t, filepath.Join(e.Root, "dist", "fnm_"+next+"_amd64.test.json"),
				func(m map[string]any) { m["overall"] = "fail" })
			return func() {}
		}, "inputs: test.json no aprobado (overall: fail)", false},
		{"frase de paso con permisos 644", func(t *testing.T) func() {
			pf := sign.PassphraseFile(sign.Home(e.DataDir))
			os.Chmod(pf, 0o644)
			return func() { os.Chmod(pf, 0o600) }
		}, "debe tener permisos 600", true},
		{"subllave inexistente", func(t *testing.T) func() {
			good := e.Cfg.Repo.Signing.Subkey
			e.Cfg.Repo.Signing.Subkey = strings.Repeat("C", 40)
			return func() { e.Cfg.Repo.Signing.Subkey = good }
		}, "sign: gpg: no hay llave secreta", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repack(t, e, "fnm", cur, next, all)
			undo := tc.setup(t)
			defer undo()
			err := e.Publish(ctx, []string{"fnm"}, map[string]string{"fnm": next}, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, se esperaba %q", err, tc.want)
			}
			var env *report.EnvError
			if tc.code3 != errors.As(err, &env) {
				t.Errorf("¿código 3? %v, err %T", tc.code3, err)
			}
			now, _ := repo.Current(e.DataDir)
			if now.Name != before.Name || stateSHA() != beforeSHA {
				t.Fatalf("el repositorio cambió: %s → %s", before.Name, now.Name)
			}
			out, err := c.Script(ctx, "apt-get update")
			if err != nil {
				t.Fatalf("apt-get update: %v", err)
			}
			if bad := AptWarnings(out, TestURL); len(bad) > 0 {
				t.Fatalf("apt avisa:\n%s", strings.Join(bad, "\n"))
			}
		})
	}
}
