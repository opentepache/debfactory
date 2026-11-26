//go:build integration

package ci

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"debfactory/internal/config"
)

// withoutSuite quita una suite de la configuración y devuelve la función que
// la vuelve a poner en su sitio, como un PR que la agrega.
func (f *mainFixture) withoutSuite(name string) (restore func()) {
	f.t.Helper()
	i := slices.IndexFunc(f.cfg.Suites, func(s config.Suite) bool { return s.Name == name })
	if i < 0 {
		f.t.Fatalf("no hay suite %s", name)
	}
	su := f.cfg.Suites[i]
	f.cfg.Suites = slices.Delete(slices.Clone(f.cfg.Suites), i, i+1)
	return func() { f.cfg.Suites = slices.Insert(f.cfg.Suites, i, su) }
}

func openIssueFor(f *mainFixture, pkg string) bool {
	for _, is := range f.gh.OpenIssues() {
		if strings.Contains(is.Title, pkg) {
			return true
		}
	}
	return false
}

// TestCIExtend recorre US2 (specs/007-configurable-suites): agregar una suite
// a la configuración y ejecutar main amplía la versión actual de cada paquete
// con los mismos .deb; una segunda ejecución no hace nada; un fallo deja ese
// paquete pendiente, notificado, y la ejecución siguiente lo reintenta.
func TestCIExtend(t *testing.T) {
	f := newMainFixture(t)
	addForky := f.withoutSuite("forky")
	if err := f.main(); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	before := f.state()
	f.mergeStatePR()

	addForky()
	f.failGate = map[string]bool{"fnm": true}
	err := f.main()
	if err == nil || !strings.Contains(err.Error(), "fnm") {
		t.Fatalf("se esperaba el fallo de fnm: %v\n%s", err, f.out.String())
	}
	out := f.out.String()
	if !strings.Contains(out, "ampliar 4 (aqua → forky, bob → forky, buf → forky, fnm → forky)") ||
		!strings.Contains(out, "repo: bob 4.3.0-0~tepache1: + forky") {
		t.Errorf("salida:\n%s", out)
	}
	st := f.state()
	for _, name := range []string{"aqua", "bob", "buf"} {
		p, prev := st.Packages[name][0], before.Packages[name][0]
		if !slices.Contains(p.Suites, "forky") || p.Debs["amd64"] != prev.Debs["amd64"] || !p.Published.Equal(prev.Published) {
			t.Errorf("%s no ampliada con los mismos .deb: %+v", name, p)
		}
	}
	if slices.Contains(st.Packages["fnm"][0].Suites, "forky") || !openIssueFor(f, "fnm") {
		t.Errorf("fnm debía quedar pendiente y notificada: %+v", st.Packages["fnm"][0])
	}
	audit := f.g.RemoteFile("debfactory/state", AuditFile)
	if strings.Count(audit, `"op":"extend"`) != 3 || !strings.Contains(audit, `"package":"bob","version":"4.3.0-0~tepache1","suites":["forky"]`) {
		t.Errorf("auditoría:\n%s", audit)
	}
	remote := f.cfg.Repo.Destinations["ci-local"].Remote
	if page, _ := os.ReadFile(filepath.Join(remote, "packages", "bob", "index.html")); !strings.Contains(string(page), "Debian 14 (testing)") {
		t.Error("el sitio no muestra forky")
	}

	// La ejecución siguiente reintenta fnm y cierra el aviso (FR-014).
	f.failGate = map[string]bool{}
	if err := f.main(); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "ampliar 1 (fnm → forky)") || !slices.Contains(f.state().Packages["fnm"][0].Suites, "forky") {
		t.Errorf("reintento:\n%s", f.out.String())
	}
	if openIssueFor(f, "fnm") {
		t.Error("el aviso de fnm debía cerrarse")
	}
	f.mergeStatePR()

	// Ya en todas las suites: nada que hacer (SC-004).
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "ci: nada que publicar") || strings.Contains(f.out.String(), "ampliar") {
		t.Errorf("sin cambios:\n%s", f.out.String())
	}
}

// TestCIRebuildExtend: ci rebuild de una versión a la que le falta una suite
// la amplía; repetirlo termina en "nada que publicar" (contracts/pipeline.md).
func TestCIRebuildExtend(t *testing.T) {
	f := newMainFixture(t)
	addForky := f.withoutSuite("forky")
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	f.mergeStatePR()
	addForky()
	if err := f.dispatch("rebuild", Options{Package: "fnm", Version: "1.39.0-0~tepache1"}, time.Time{}); err != nil {
		t.Fatalf("rebuild: %v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "se amplía a forky") || !slices.Contains(f.state().Packages["fnm"][0].Suites, "forky") {
		t.Errorf("rebuild no amplió:\n%s", f.out.String())
	}
	if slices.Contains(f.state().Packages["aqua"][0].Suites, "forky") {
		t.Error("rebuild de fnm no debía tocar aqua")
	}
	f.mergeStatePR()
	if err := f.dispatch("rebuild", Options{Package: "fnm", Version: "1.39.0-0~tepache1"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "nada que publicar") {
		t.Errorf("segundo rebuild:\n%s", f.out.String())
	}
}
