//go:build e2e

package repotest

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/repo"
	reposync "debfactory/internal/sync"
)

var candidate = regexp.MustCompile(`(?m)^\s*Candidate: (\S+)`)

// TestE2ESyncLocal sincroniza al destino local, lo sirve, interrumpe una
// sincronización tras la fase pool y comprueba que el cliente sigue viendo
// el estado anterior completo y que repetirla lo completa (US5, SC-004).
func TestE2ESyncLocal(t *testing.T) {
	ctx := context.Background()
	e := e2eEnv(t)
	st := publishAll(t, ctx, e)
	dest := t.TempDir() + "/repo"
	e.Cfg.Repo.Destinations["local"] = config.Destination{Remote: dest}
	if err := reposync.Sync(ctx, e, "local", false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := reposync.VerifyDest(ctx, e, "local"); err != nil {
		t.Fatalf("verify --dest: %v", err)
	}
	srv, err := StartServer(ctx, e.Cfg, dest, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	blocks := guide(t, e)
	trixie, _ := e.Cfg.Suite("trixie")
	c, err := StartClient(ctx, trixie, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	addRepo(t, ctx, c, e, blocks)
	v1 := packagesIn(st, "trixie")["fnm"]
	policy := func() string {
		out, err := c.Script(ctx, "apt-get update >/dev/null && apt-cache policy fnm")
		if err != nil {
			t.Fatalf("apt: %v", err)
		}
		m := candidate.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("sin candidato:\n%s", out)
		}
		return m[1]
	}
	if got := policy(); got != v1 {
		t.Fatalf("candidato inicial %s, se esperaba %s", got, v1)
	}

	v2 := strings.Replace(v1, "~tepache1", "~tepache2", 1)
	var all []string
	for _, s := range e.Cfg.Suites {
		all = append(all, s.Name)
	}
	repack(t, e, "fnm", v1, v2, all)
	if err := e.Publish(ctx, []string{"fnm"}, map[string]string{"fnm": v2}, false); err != nil {
		t.Fatal(err)
	}
	cut := errors.New("interrupción simulada")
	reposync.Hook = func(phase string) error {
		if phase == reposync.PhasePool {
			return cut
		}
		return nil
	}
	err = reposync.Sync(ctx, e, "local", false)
	reposync.Hook = nil
	if !errors.Is(err, cut) {
		t.Fatalf("sync interrumpido: %v", err)
	}
	if got := policy(); got != v1 {
		t.Fatalf("tras la interrupción el cliente ve %s, se esperaba %s", got, v1)
	}
	if err := reposync.Sync(ctx, e, "local", false); err != nil {
		t.Fatalf("repetir sync: %v", err)
	}
	if got := policy(); got != v2 {
		t.Fatalf("tras repetir el cliente ve %s, se esperaba %s", got, v2)
	}
	cur, _ := repo.Current(e.DataDir)
	files, _ := repo.TreeFiles(dest)
	for p, sha := range cur.Files {
		if files[p] != sha {
			t.Errorf("el destino no coincide en %s", p)
		}
	}
	if err := reposync.VerifyDest(ctx, e, "local"); err != nil {
		t.Fatalf("verify --dest final: %v", err)
	}
}
