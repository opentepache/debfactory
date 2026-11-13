//go:build e2e

package repotest

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/repo"
	reposync "debfactory/internal/sync"
)

// TestE2ESyncR2 sincroniza a un destino S3 real (T048, research §3 riesgo
// 2) con una llave de prueba: verify --dest, encabezados Cache-Control del
// .deb más grande, un segundo sync sin cambios y un cliente que agrega el
// repositorio por HTTPS siguiendo la guía. Necesita las credenciales del
// remote de rclone, así que solo corre con DEBFACTORY_E2E_R2=<destino>.
func TestE2ESyncR2(t *testing.T) {
	name := os.Getenv("DEBFACTORY_E2E_R2")
	if name == "" {
		t.Skip("DEBFACTORY_E2E_R2 no está definida (p. ej. r2test)")
	}
	ctx := context.Background()
	e := e2eEnv(t)
	dest, ok := e.Cfg.Repo.Destinations[name]
	if !ok || dest.PublicURL == "" {
		t.Fatalf("%s: destino desconocido o sin public_url", name)
	}
	st := publishAll(t, ctx, e)
	// La guía usa el marcador mientras repo.url esté vacía; Render lo
	// sustituye por la URL del destino.
	guideURL := e.Cfg.Repo.URL
	e.Cfg.Repo.URL = dest.PublicURL

	start := time.Now()
	if err := reposync.Sync(ctx, e, name, false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	t.Logf("primer sync: %s", time.Since(start).Round(time.Second))
	if err := reposync.VerifyDest(ctx, e, name); err != nil {
		t.Fatalf("verify --dest: %v", err)
	}

	// El .deb más grande (buf, ~30 MB) y su Cache-Control.
	cur, _ := repo.Current(e.DataDir)
	var big string
	var bigSize int64
	for p := range cur.Files {
		if !strings.HasPrefix(p, "pool/") {
			continue
		}
		if fi, err := os.Stat(filepath.Join(repo.PublicDir(e.DataDir), "current", p)); err == nil && fi.Size() > bigSize {
			big, bigSize = p, fi.Size()
		}
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	head := func(p string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, http.MethodHead, strings.TrimSuffix(dest.PublicURL, "/")+"/"+p, nil)
		req.Header.Set("User-Agent", "debfactory")
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("HEAD %s: %v", p, err)
		}
		res.Body.Close()
		return res
	}
	res := head(big)
	if res.StatusCode != 200 || res.ContentLength != bigSize {
		t.Fatalf("%s: estado %d, tamaño %d (se esperaba %d)", big, res.StatusCode, res.ContentLength, bigSize)
	}
	if got := res.Header.Get("Cache-Control"); got != e.Cfg.Repo.CacheControl.Immutable {
		t.Errorf("%s: Cache-Control %q", big, got)
	}
	t.Logf("%s: %d bytes, Cache-Control %q", big, bigSize, res.Header.Get("Cache-Control"))
	if got := head("dists/trixie/InRelease").Header.Get("Cache-Control"); got != e.Cfg.Repo.CacheControl.Mutable {
		t.Errorf("InRelease: Cache-Control %q", got)
	}

	start = time.Now()
	if err := reposync.Sync(ctx, e, name, false); err != nil {
		t.Fatalf("segundo sync: %v", err)
	}
	t.Logf("segundo sync (sin cambios): %s", time.Since(start).Round(time.Second))

	// Un cliente real agrega el repositorio por HTTPS siguiendo la guía.
	blocks := guide(t, e)
	trixie, _ := e.Cfg.Suite("trixie")
	c, err := StartClient(ctx, trixie, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	host := strings.TrimPrefix(strings.TrimPrefix(dest.PublicURL, "https://"), "http://")
	out, err := c.Script(ctx, Render(blocks["add"], guideURL, dest.PublicURL, map[string]string{"suite": "trixie"}))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if bad := AptWarnings(out, host); len(bad) > 0 {
		t.Fatalf("add: apt avisa del repositorio:\n%s", strings.Join(bad, "\n"))
	}
	want := packagesIn(st, "trixie")["fnm"]
	if _, err := c.Script(ctx, "apt-get install -y fnm"); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := c.Installed(ctx, "fnm")
	if err != nil || got["fnm"] != want {
		t.Fatalf("fnm instalado %v (%v), se esperaba %s", got, err, want)
	}
}
