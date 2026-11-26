//go:build e2e

package repotest

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/repo"
	"debfactory/internal/site"
)

// serveSite publica todo, genera el sitio de la generación vigente y sirve
// los dos juntos en http://repo, como el hosting (FR-023). Devuelve el
// directorio servido y el estado.
func serveSite(t *testing.T, ctx context.Context) (*repo.Env, string, *repo.State) {
	t.Helper()
	e := e2eEnv(t)
	st := publishAll(t, ctx, e)
	m, err := site.FromData(e.Cfg, e.Root, st, e.DataDir, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "site")
	if err := site.Build(m, tmp); err != nil {
		t.Fatal(err)
	}
	www := filepath.Join(t.TempDir(), "www")
	gen, err := filepath.EvalSymlinks(filepath.Join(repo.PublicDir(e.DataDir), "current"))
	if err != nil {
		t.Fatal(err)
	}
	copyDir(t, gen, www)
	copyDir(t, tmp, www)
	srv, err := StartServer(ctx, e.Cfg, www, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return e, www, st
}

// TestE2ESite ejecuta tal cual los bloques de la portada, extraídos del HTML
// generado, en todas las suites configuradas × 2 arquitecturas, e instala todos los paquetes
// publicados en cada suite (SC-002).
func TestE2ESite(t *testing.T) {
	ctx := context.Background()
	e, www, st := serveSite(t, ctx)
	blocks, err := SiteBlocks(filepath.Join(www, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"add", "add-debian", "add-ubuntu", "verify-key", "remove"} {
		if blocks[step] == "" {
			t.Fatalf("falta el bloque %s en la portada", step)
		}
	}
	fpr := strings.ToUpper(e.Cfg.Repo.Signing.Masters[0])
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
	sem := make(chan struct{}, 4)
	var mu sync.Mutex
	t.Run("matriz", func(t *testing.T) {
		for _, cb := range combos {
			t.Run(cb.suite.Name+"-"+cb.arch, func(t *testing.T) {
				t.Parallel()
				sem <- struct{}{}
				defer func() { <-sem }()
				c, err := StartClient(ctx, cb.suite, cb.arch)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				render := func(step string) string { return Render(blocks[step], e.Cfg.Repo.URL, TestURL, nil) }
				// La suite esperada: la que declara el sistema si está
				// configurada (en sid, el codename de testing: forky), o la
				// propia suite del contenedor (specs/007, FR-009, FR-010).
				want := cb.suite.Name
				codename, _ := c.Script(ctx, `. /etc/os-release; printf %s "${UBUNTU_CODENAME:-${DEBIAN_CODENAME:-$VERSION_CODENAME}}"`)
				if s, ok := e.Cfg.Suite(strings.TrimSpace(codename)); ok && !s.Catchall {
					want = s.Name
				}
				// El bloque de la distribución y después el universal (el que
				// se muestra primero): los dos eligen la misma suite.
				for _, step := range []string{"add-" + cb.suite.Distro, "add"} {
					out, err := c.Script(ctx, render(step))
					if err != nil {
						t.Fatalf("%s: %v", step, err)
					}
					if bad := AptWarnings(out, TestURL); len(bad) > 0 {
						t.Fatalf("%s: apt avisa del repositorio:\n%s", step, strings.Join(bad, "\n"))
					}
					if src, _ := c.Script(ctx, "cat /etc/apt/sources.list.d/debfactory.sources"); !strings.Contains(src, "Suites: "+want+"\n") {
						t.Errorf("%s: la suite elegida no es %s:\n%s", step, want, src)
					}
				}
				out, err := c.Script(ctx, render("verify-key"))
				if err != nil || !strings.Contains(strings.ReplaceAll(out, " ", ""), fpr) {
					t.Errorf("verify-key no muestra el fingerprint %s: %v\n%s", fpr, err, out)
				}
				pkgs := packagesIn(st, want)
				var names []string
				for n := range pkgs {
					names = append(names, n)
				}
				sort.Strings(names)
				if _, err := c.Script(ctx, "apt-get install -y "+strings.Join(names, " ")); err != nil {
					t.Fatalf("install: %v", err)
				}
				got, err := c.Installed(ctx, names...)
				if err != nil {
					t.Fatal(err)
				}
				for n, v := range pkgs {
					if got[n] != v {
						t.Errorf("%s: instalada %q, se esperaba %q", n, got[n], v)
					}
				}
				out, err = c.Script(ctx, render("remove"))
				if err != nil {
					t.Fatalf("remove: %v", err)
				}
				if strings.Contains(out, TestURL) {
					t.Errorf("remove: apt-get update sigue consultando %s", TestURL)
				}
				mu.Lock()
				t.Logf("%s/%s: %d paquetes", cb.suite.Name, cb.arch, len(names))
				mu.Unlock()
			})
		}
	})
	if _, err := os.Stat(filepath.Join(www, "dists")); err != nil {
		t.Error("el sitio pisó el repositorio")
	}
}
