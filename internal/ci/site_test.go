//go:build integration

package ci

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/report"
	reposync "debfactory/internal/sync"
)

// TestCISite recorre el paso site (specs/006-repo-website, US3): la primera
// publicación sube el sitio; sin nada que publicar no se sube nada; un fallo
// del sitio se notifica sin afectar a la publicación y el siguiente run lo
// repara; retire y refresh lo mantienen al día.
func TestCISite(t *testing.T) {
	f := newMainFixture(t)
	remote := f.cfg.Repo.Destinations["ci-local"].Remote
	page := func(name string) string {
		data, _ := os.ReadFile(filepath.Join(remote, filepath.FromSlash(name)))
		return string(data)
	}

	if err := f.main(); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "site: serial 1, 10 páginas, ") || !strings.Contains(f.out.String(), "ci-local: subidas ") {
		t.Errorf("salida:\n%s", f.out.String())
	}
	for _, name := range []string{"index.html", "es/index.html", "packages/bob/index.html", "es/packages/fnm/index.html", "site.json", "dists/trixie/InRelease"} {
		if page(name) == "" {
			t.Errorf("falta %s en el destino", name)
		}
	}
	if !strings.Contains(page("packages/fnm/index.html"), "1.39.0-0~tepache1") {
		t.Error("la página de fnm no muestra la versión publicada")
	}
	rec, _ := reposync.ReadSiteRecord(f.data, "ci-local")
	if rec == nil || rec.Serial != 1 {
		t.Fatalf("registro del sitio = %+v", rec)
	}

	// Sin nada que publicar (y con el PR de estado todavía sin integrar).
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "ci: nada que publicar") || !strings.Contains(f.out.String(), "site: serial 1 sin cambios; nada que subir") {
		t.Errorf("sin cambios:\n%s", f.out.String())
	}
	f.mergeStatePR()

	// Fallo del paso site: la publicación de fnm queda hecha, el sitio sigue
	// con la versión anterior y se notifica.
	f.bump("fnm")
	TestHookSite = func() error { return errors.New("disco lleno") }
	err := f.main()
	TestHookSite = nil
	if err == nil || !strings.Contains(err.Error(), "site: disco lleno") {
		t.Fatalf("se esperaba el fallo del sitio: %v", err)
	}
	if f.state().Serial != 2 || f.g.RemoteBranch("debfactory/state") == "" {
		t.Errorf("la publicación debía completarse (serial %d)", f.state().Serial)
	}
	if strings.Contains(page("packages/fnm/index.html"), "tepache2") {
		t.Error("el sitio no debía cambiar tras el fallo")
	}
	if !siteIssue(f) {
		t.Errorf("falta la notificación del sitio: %+v", f.gh.OpenIssues())
	}

	// El siguiente run lo repara, aunque no haya nada que publicar.
	if err := f.main(); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if !strings.Contains(page("packages/fnm/index.html"), "1.39.0-0~tepache2") ||
		!strings.Contains(page("packages/fnm/index.html"), "--allow-downgrades fnm=1.39.0-0~tepache1") {
		t.Error("el sitio no se puso al día")
	}
	f.mergeStatePR()

	// retire quita la versión del sitio.
	if err := f.dispatch("retire", Options{Package: "fnm", Version: "1.39.0-0~tepache1", Reason: "prueba"}, time.Time{}); err != nil {
		t.Fatalf("retire: %v\n%s", err, f.out.String())
	}
	if strings.Contains(page("packages/fnm/index.html"), "fnm=1.39.0-0~tepache1") {
		t.Error("la versión retirada sigue en el sitio")
	}
	f.mergeStatePR()

	// refresh repara un sitio atrasado (aquí, borrado del destino).
	os.Remove(filepath.Join(remote, "index.html"))
	os.Remove(reposync.SiteRecordPath(f.data, "ci-local"))
	if err := f.dispatch("refresh", Options{}, time.Time{}); err != nil {
		t.Fatalf("refresh: %v\n%s", err, f.out.String())
	}
	if page("index.html") == "" {
		t.Error("refresh no repuso el sitio")
	}
}

// siteIssue indica si hay un issue abierto de la etapa site.
func siteIssue(f *mainFixture) bool {
	for _, is := range f.gh.OpenIssues() {
		if strings.Contains(is.Title, "en "+string(report.Site)) {
			return true
		}
	}
	return false
}

// TestCISiteDisabled: con site.enabled: false no se genera ni se sube nada.
func TestCISiteDisabled(t *testing.T) {
	f := newMainFixture(t)
	f.cfg.Site.Enabled = false
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.Repo.Destinations["ci-local"].Remote, "index.html")); err == nil ||
		strings.Contains(f.out.String(), "site:") {
		t.Error("el sitio no debía subirse")
	}
}
