//go:build e2e

package repotest

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestE2ELifecycle recorre en cada suite (amd64) publicar → instalar →
// publicar versión nueva → upgrade → retirar → downgrade explícito (SC-003).
func TestE2ELifecycle(t *testing.T) {
	ctx := context.Background()
	e := e2eEnv(t)
	st := publishAll(t, ctx, e)
	serveCurrent(t, ctx, e)
	blocks := guide(t, e)
	v1 := packagesIn(st, "trixie")["fnm"]
	v2 := strings.Replace(v1, "~tepache1", "~tepache2", 1)

	clients := make([]*Client, len(e.Cfg.Suites))
	forEach := func(t *testing.T, step string, f func(t *testing.T, c *Client)) {
		t.Helper()
		var wg sync.WaitGroup
		for i := range clients {
			wg.Add(1)
			go func(c *Client) {
				defer wg.Done()
				f(t, c)
			}(clients[i])
		}
		wg.Wait()
		if t.Failed() {
			t.Fatalf("falló el paso %s", step)
		}
	}
	expect := func(t *testing.T, c *Client, want string) {
		got, err := c.Installed(ctx, "fnm")
		if err != nil || got["fnm"] != want {
			t.Errorf("%s: fnm %q, se esperaba %q (%v)", c.Suite, got["fnm"], want, err)
		}
	}
	run := func(t *testing.T, c *Client, block string, vars map[string]string) {
		if out, err := c.Script(ctx, Render(blocks[block], e.Cfg.Repo.URL, TestURL, vars)); err != nil {
			t.Errorf("%s: %s: %v", c.Suite, block, err)
		} else if bad := AptWarnings(out, TestURL); len(bad) > 0 {
			t.Errorf("%s: %s: %s", c.Suite, block, strings.Join(bad, "; "))
		}
	}

	for i, s := range e.Cfg.Suites {
		c, err := StartClient(ctx, s, "amd64")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		clients[i] = c
	}
	// 1. v1 publicada: agregar e instalar.
	forEach(t, "instalar v1", func(t *testing.T, c *Client) {
		run(t, c, "add", map[string]string{"suite": c.Suite})
		run(t, c, "install", map[string]string{"packages": "fnm"})
		expect(t, c, v1)
	})
	// 2. v2 publicada: apt upgrade la instala.
	var all []string
	for _, s := range e.Cfg.Suites {
		all = append(all, s.Name)
	}
	repack(t, e, "fnm", v1, v2, all)
	if err := e.Publish(ctx, []string{"fnm"}, map[string]string{"fnm": v2}, false); err != nil {
		t.Fatalf("publicar v2: %v", err)
	}
	forEach(t, "upgrade a v2", func(t *testing.T, c *Client) {
		run(t, c, "upgrade", nil)
		expect(t, c, v2)
	})
	// 3. retirar v2: upgrade no la cambia; el downgrade explícito vuelve a v1.
	if err := e.RetireVersion(ctx, "fnm", v2, "prueba de retiro", false, false); err != nil {
		t.Fatalf("retirar v2: %v", err)
	}
	forEach(t, "downgrade a v1", func(t *testing.T, c *Client) {
		run(t, c, "upgrade", nil)
		expect(t, c, v2)
		run(t, c, "downgrade", map[string]string{"version": v1})
		expect(t, c, v1)
	})
	// 4. una versión retirada no se vuelve a publicar.
	err := e.Publish(ctx, []string{"fnm"}, map[string]string{"fnm": v2}, false)
	if err == nil || !strings.Contains(err.Error(), "versión retirada") {
		t.Fatalf("republicar v2: %v", err)
	}
	// 5. Valid-Until (FR-030 de la fase 003): una generación firmada hace 31
	// días está caducada y apt la rechaza. La fecha de firma se inyecta con
	// Env.Now; nunca se cambia el reloj del host (los contenedores lo
	// comparten).
	signedAt := time.Now().AddDate(0, 0, -(e.Cfg.Repo.Validity.Days + 1))
	e.Now = func() time.Time { return signedAt }
	if err := e.Rebuild(ctx); err != nil {
		t.Fatalf("regenerar con firma antigua: %v", err)
	}
	e.Now = nil
	forEach(t, "generación caducada", func(t *testing.T, c *Client) {
		out, _ := c.Script(ctx, "apt-get update 2>&1 || true")
		if !strings.Contains(out, "is expired") {
			t.Errorf("%s: apt-get update no rechazó la generación caducada:\n%s", c.Suite, out)
		}
	})
	// Una generación firmada ahora vuelve a ser válida.
	if err := e.Rebuild(ctx); err != nil {
		t.Fatalf("regenerar: %v", err)
	}
	forEach(t, "generación renovada", func(t *testing.T, c *Client) {
		run(t, c, "upgrade", nil)
	})
}
