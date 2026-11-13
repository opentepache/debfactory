//go:build e2e

package repotest

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/repo"
)

// addRepo ejecuta el bloque add de la guía y falla si apt avisa del
// repositorio.
func addRepo(t *testing.T, ctx context.Context, c *Client, e *repo.Env, blocks map[string]string) {
	t.Helper()
	out, err := c.Script(ctx, Render(blocks["add"], e.Cfg.Repo.URL, TestURL, map[string]string{"suite": c.Suite}))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if bad := AptWarnings(out, TestURL); len(bad) > 0 {
		t.Fatalf("add: apt avisa del repositorio:\n%s", strings.Join(bad, "\n"))
	}
}

// installAll ejecuta el bloque install con los paquetes de la suite y
// comprueba sus versiones.
func installAll(t *testing.T, ctx context.Context, c *Client, e *repo.Env, blocks map[string]string, st *repo.State) {
	t.Helper()
	want := packagesIn(st, c.Suite)
	var names []string
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	script := Render(blocks["install"], e.Cfg.Repo.URL, TestURL, map[string]string{"packages": strings.Join(names, " ")})
	if _, err := c.Script(ctx, script); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := c.Installed(ctx, names...)
	if err != nil {
		t.Fatal(err)
	}
	for n, v := range want {
		if got[n] != v {
			t.Errorf("%s: instalada %q, se esperaba %q", n, got[n], v)
		}
	}
}

// TestE2ERepo sigue la guía en las 5 suites × 2 arquitecturas (SC-001,
// SC-002, FR-032).
func TestE2ERepo(t *testing.T) {
	ctx := context.Background()
	e := e2eEnv(t)
	st := publishAll(t, ctx, e)
	serveCurrent(t, ctx, e)
	blocks := guide(t, e)

	type combo struct {
		suite config.Suite
		arch  string
	}
	var combos []combo
	for _, s := range e.Cfg.Suites {
		for _, a := range e.Cfg.Arches {
			combos = append(combos, combo{s, a})
		}
	}
	start := time.Now()
	var mu sync.Mutex
	times := map[string]time.Duration{}
	sem := make(chan struct{}, 4)
	t.Run("matriz", func(t *testing.T) {
		for _, cb := range combos {
			t.Run(cb.suite.Name+"-"+cb.arch, func(t *testing.T) {
				t.Parallel()
				sem <- struct{}{}
				defer func() { <-sem }()
				t0 := time.Now()
				c, err := StartClient(ctx, cb.suite, cb.arch)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				addRepo(t, ctx, c, e, blocks)
				addTime := time.Since(t0)
				installAll(t, ctx, c, e, blocks, st)
				out, err := c.Script(ctx, Render(blocks["remove"], e.Cfg.Repo.URL, TestURL, nil))
				if err != nil {
					t.Fatalf("remove: %v", err)
				}
				if strings.Contains(out, TestURL) {
					t.Errorf("remove: apt-get update sigue consultando %s:\n%s", TestURL, out)
				}
				if left, _ := c.Script(ctx, "ls /etc/apt/sources.list.d/debfactory.sources /etc/apt/keyrings/debfactory.gpg 2>/dev/null || true"); strings.TrimSpace(left) != "" {
					t.Errorf("remove dejó: %s", left)
				}
				mu.Lock()
				times[cb.suite.Name+"/"+cb.arch] = time.Since(t0)
				mu.Unlock()
				t.Logf("%s/%s: add %s, total %s", cb.suite.Name, cb.arch, addTime.Round(time.Second), time.Since(t0).Round(time.Second))
			})
		}
	})
	total := time.Since(start)
	t.Logf("matriz completa: %s", total.Round(time.Second))
	if total > 20*time.Minute {
		t.Errorf("la matriz tardó %s (> 20 min, research §3)", total.Round(time.Second))
	}
}
