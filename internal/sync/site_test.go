package sync

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// siteEnv devuelve un entorno con el destino "local" apuntando a un
// directorio vacío.
func siteEnv(t *testing.T) (*repo.Env, string) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/rclone"); err != nil {
		t.Skip("rclone no está instalado")
	}
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	remote := t.TempDir()
	cfg.Repo.Destinations["local"] = config.Destination{Remote: remote}
	return &repo.Env{DataDir: t.TempDir(), Cfg: cfg, Out: &report.Printer{Out: io.Discard, Err: io.Discard}}, remote
}

// tree escribe un sitio mínimo con los archivos dados.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
	return err == nil
}

func TestSyncSite(t *testing.T) {
	e, remote := siteEnv(t)
	ctx := context.Background()
	v1 := tree(t, map[string]string{
		"assets/site.aaa.css": "a", "index.html": "home 1", "packages/bob/index.html": "bob",
		"packages/old/index.html": "old", "site.json": "{}",
	})

	// Orden: tras la fase assets, los estilos están y las páginas todavía no.
	var seen []string
	SiteHook = func(phase string) error {
		seen = append(seen, phase)
		if phase == SitePhaseAssets && (!exists(remote, "assets/site.aaa.css") || exists(remote, "index.html")) {
			t.Error("assets no se subió antes que las páginas")
		}
		return nil
	}
	defer func() { SiteHook = nil }()
	res, err := SyncSite(ctx, e, "local", v1, 1, false)
	SiteHook = nil
	if err != nil {
		t.Fatal(err)
	}
	if res.Unchanged || res.Uploaded != 5 || res.Deleted != 0 || strings.Join(seen, ",") != "assets,pages" {
		t.Errorf("primera subida = %+v, fases %v", res, seen)
	}
	rec, _ := ReadSiteRecord(e.DataDir, "local")
	if rec == nil || rec.Serial != 1 || len(rec.Files) != 5 || rec.Digest == "" {
		t.Fatalf("registro = %+v", rec)
	}

	// El mismo árbol no sube nada.
	if res, err := SyncSite(ctx, e, "local", v1, 1, false); err != nil || !res.Unchanged {
		t.Errorf("sin cambios = %+v, %v", res, err)
	}

	// v2: cambia la portada y el CSS, desaparece old. Borrado diferido: old y
	// el CSS viejo siguen en el destino tras esta subida.
	v2 := tree(t, map[string]string{
		"assets/site.bbb.css": "b", "index.html": "home 2", "packages/bob/index.html": "bob", "site.json": "{}",
	})
	if res, err = SyncSite(ctx, e, "local", v2, 2, false); err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 || res.Deleted != 0 || !exists(remote, "packages/old/index.html") || !exists(remote, "assets/site.aaa.css") {
		t.Errorf("v2 = %+v", res)
	}
	// v3: ahora sí se borra lo que salió en v2.
	v3 := tree(t, map[string]string{
		"assets/site.bbb.css": "b", "index.html": "home 3", "packages/bob/index.html": "bob", "site.json": "{}",
	})
	if res, err = SyncSite(ctx, e, "local", v3, 3, false); err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 2 || exists(remote, "packages/old/index.html") || exists(remote, "assets/site.aaa.css") ||
		!exists(remote, "assets/site.bbb.css") {
		t.Errorf("v3 = %+v", res)
	}
	if body, _ := os.ReadFile(filepath.Join(remote, "index.html")); string(body) != "home 3" {
		t.Errorf("portada = %q", body)
	}
}

// TestSyncSiteFailure: si falla antes de terminar las páginas, el registro no
// cambia y la siguiente subida repite (FR-013).
func TestSyncSiteFailure(t *testing.T) {
	e, _ := siteEnv(t)
	ctx := context.Background()
	v1 := tree(t, map[string]string{"assets/site.a.css": "a", "index.html": "1"})
	SiteHook = func(phase string) error {
		if phase == SitePhaseAssets {
			return errors.New("corte simulado")
		}
		return nil
	}
	_, err := SyncSite(ctx, e, "local", v1, 1, false)
	SiteHook = nil
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if rec, _ := ReadSiteRecord(e.DataDir, "local"); rec != nil {
		t.Fatalf("no debería haber registro: %+v", rec)
	}
	res, err := SyncSite(ctx, e, "local", v1, 1, false)
	if err != nil || res.Unchanged || res.Uploaded != 2 {
		t.Errorf("reintento = %+v, %v", res, err)
	}
}

func TestSyncSiteErrors(t *testing.T) {
	e, _ := siteEnv(t)
	_, err := SyncSite(context.Background(), e, "nada", t.TempDir(), 1, false)
	if err == nil || !strings.Contains(err.Error(), "site: nada: destino desconocido") {
		t.Errorf("error = %v", err)
	}
	e.Cfg.Repo.Destinations["roto"] = config.Destination{Remote: "noexiste:bucket"}
	_, err = SyncSite(context.Background(), e, "roto", tree(t, map[string]string{"index.html": "x"}), 1, false)
	if err == nil || !strings.Contains(err.Error(), ": site: roto: rclone:") {
		t.Errorf("error = %v", err)
	}
}

func TestSiteDeletions(t *testing.T) {
	last := &SiteRecord{Files: map[string]string{"a": "1", "b": "1"}, PreviousFiles: map[string]string{"a": "0", "c": "0", "d": "0"},
		Pending: []string{"e"}}
	got := SiteDeletions(map[string]string{"a": "2", "d": "1"}, last)
	if strings.Join(got, ",") != "c,e" {
		t.Errorf("borrados = %v", got)
	}
	if SiteDeletions(map[string]string{}, nil) != nil {
		t.Error("sin registro no se borra nada")
	}
}
