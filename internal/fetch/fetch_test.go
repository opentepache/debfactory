package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"debfactory/internal/testutil"
)

type redirect = testutil.Redirect

func newServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.Host == "github.com" && r.URL.Path == "/o/r/releases/download/v1/a.zip":
			w.Write([]byte("contenido"))
		case r.Host == "github.com" && r.URL.Path == "/o/r/releases/download/v1/redir.zip":
			http.Redirect(w, r, "https://objects.githubusercontent.com/blob/1", http.StatusFound)
		case r.Host == "objects.githubusercontent.com":
			w.Write([]byte("contenido"))
		case r.Host == "github.com" && r.URL.Path == "/o/r/releases/download/v1/evil.zip":
			http.Redirect(w, r, "https://evil.example.com/x", http.StatusFound)
		case r.Host == "github.com" && r.URL.Path == "/o/r/releases/download/v1/plain.zip":
			http.Redirect(w, r, "http://objects.githubusercontent.com/blob/1", http.StatusFound)
		case r.Host == "github.com" && r.URL.Path == "/o/r/releases/download/v1/raw.zip":
			http.Redirect(w, r, "https://raw.githubusercontent.com/o/r/v1/LICENSE", http.StatusFound)
		case r.Host == "raw.githubusercontent.com":
			w.Write([]byte("licencia"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetCachesOnlyVerified(t *testing.T) {
	var hits atomic.Int32
	srv := newServer(t, &hits)
	f := New(t.TempDir(), time.Minute, redirect{Server: srv})
	ctx := context.Background()
	url := "https://github.com/o/r/releases/download/v1/a.zip"
	want := SHA256([]byte("contenido"))

	// Sin hash esperado: temporal fuera de la caché.
	tmp, sha, err := f.Get(ctx, url, "")
	if err != nil || sha != want {
		t.Fatalf("Get sin hash = %q, %q, %v", tmp, sha, err)
	}
	if strings.Contains(tmp, "sha256") || exists(f.cachePath(sha)) {
		t.Fatal("un archivo sin verificar entró en la caché")
	}
	if _, err := f.Commit(tmp, sha); err != nil || !exists(f.cachePath(sha)) {
		t.Fatalf("Commit: %v", err)
	}
	// Con hash correcto y ya en caché: sin petición.
	before := hits.Load()
	p, _, err := f.Get(ctx, url, want)
	if err != nil || p != f.cachePath(want) || hits.Load() != before {
		t.Fatalf("Get desde caché = %q, %v (peticiones %d → %d)", p, err, before, hits.Load())
	}
	// Con hash incorrecto: *HashMismatch y nada en caché.
	bad := strings.Repeat("0", 64)
	_, _, err = f.Get(ctx, url, bad)
	var hm *HashMismatch
	if !errors.As(err, &hm) || hm.Expected != bad || hm.Got != want {
		t.Fatalf("error = %v", err)
	}
	if exists(f.cachePath(bad)) {
		t.Fatal("un archivo con hash incorrecto entró en la caché")
	}
	if entries, _ := os.ReadDir(filepath.Join(f.Cache, "tmp")); len(entries) != 0 {
		t.Errorf("quedan temporales: %v", entries)
	}
}

// TestRedirects cubre T039: solo https hacia los hosts de GitHub permitidos.
func TestRedirects(t *testing.T) {
	var hits atomic.Int32
	srv := newServer(t, &hits)
	f := New(t.TempDir(), time.Minute, redirect{Server: srv})
	ctx := context.Background()
	base := "https://github.com/o/r/releases/download/v1/"

	if _, _, err := f.Get(ctx, base+"redir.zip", SHA256([]byte("contenido"))); err != nil {
		t.Errorf("redirección a objects.githubusercontent.com: %v", err)
	}
	for _, c := range []struct{ file, host string }{
		{"evil.zip", "https://evil.example.com"},
		{"plain.zip", "http://objects.githubusercontent.com"},
		{"raw.zip", "https://raw.githubusercontent.com"},
	} {
		_, _, err := f.Get(ctx, base+c.file, "")
		var he *HostError
		if !errors.As(err, &he) || he.Host != c.host {
			t.Errorf("%s: error = %v; se esperaba destino no permitido %s", c.file, err, c.host)
		}
	}
	// La licencia sí puede venir de raw.githubusercontent.com.
	if _, _, err := f.Get(ctx, LicenseURL("o/r", "v1", "LICENSE"), ""); err != nil {
		t.Errorf("licencia: %v", err)
	}
	// Orígenes no permitidos: ninguna petición.
	before := hits.Load()
	for _, u := range []string{"http://github.com/o/r/releases/download/v1/a.zip", "https://example.com/a.zip"} {
		var he *HostError
		if _, _, err := f.Get(ctx, u, ""); !errors.As(err, &he) {
			t.Errorf("%s: error = %v", u, err)
		}
	}
	if hits.Load() != before {
		t.Error("se hizo una petición a un origen no permitido")
	}
}

func TestPublishedAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Schniz/fnm/releases/tags/v1.39.0" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secreto" {
			t.Errorf("falta el token")
		}
		w.Write([]byte(`{"tag_name":"v1.39.0","published_at":"2026-03-06T15:23:47Z"}`))
	}))
	defer srv.Close()
	g := &GitHub{API: srv.URL, Token: "secreto"}
	got, err := g.PublishedAt(context.Background(), "Schniz/fnm", "v1.39.0")
	if err != nil || got.Unix() != 1772810627 {
		t.Fatalf("PublishedAt = %v (%d), %v", got, got.Unix(), err)
	}
	if _, err := g.PublishedAt(context.Background(), "Schniz/fnm", "v0"); err == nil {
		t.Error("se esperaba error para un tag inexistente")
	}
}

func TestLicenseURL(t *testing.T) {
	if got := LicenseURL("MordechaiHadad/bob", "v4.3.0", "LICENSE"); got != "https://raw.githubusercontent.com/MordechaiHadad/bob/v4.3.0/LICENSE" {
		t.Errorf("LicenseURL = %s", got)
	}
}
