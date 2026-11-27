package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/repo"
	"debfactory/internal/testutil"
)

// fixture es un proyecto de prueba: la configuración y los manifiestos de
// testdata/project (copiados para poder modificarlos), su estado con una
// versión anterior de bob, y la generación que ese estado describe.
type fixture struct {
	cfg  *config.Config
	root string
	st   *repo.State
	gen  *repo.GenInfo
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	src := testutil.ProjectRoot()
	root := t.TempDir()
	for _, dir := range []string{"config", "packages"} {
		entries, err := os.ReadDir(filepath.Join(src, dir))
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Join(root, dir), 0o755)
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(src, dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(root, dir, e.Name()), data, 0o644)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := repo.ReadState(src)
	if err != nil {
		t.Fatal(err)
	}
	// Los paquetes ya están ampliados a forky (specs/007), que va después de
	// trixie en la configuración.
	for name, versions := range st.Packages {
		for i := range versions {
			var suites []string
			for _, su := range versions[i].Suites {
				suites = append(suites, su)
				if su == "trixie" {
					suites = append(suites, "forky")
				}
			}
			st.Packages[name][i].Suites = suites
		}
	}
	old := st.Packages["bob"][0]
	old.Version = "4.2.1-0~tepache1"
	old.Published = time.Date(2026, 10, 2, 23, 30, 0, 0, time.FixedZone("CST", -6*3600))
	st.Packages["bob"] = append(st.Packages["bob"], old)
	f := &fixture{cfg: cfg, root: root, st: st}
	f.sync(t)
	return f
}

// sync recalcula la generación tras modificar el estado.
func (f *fixture) sync(t *testing.T) {
	t.Helper()
	sha, err := f.st.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	f.gen = &repo.GenInfo{Name: "gen-1", Serial: f.st.Serial, StateSHA256: sha}
}

func (f *fixture) load(t *testing.T) *Model {
	t.Helper()
	m, err := Load(f.cfg, f.root, f.st, f.gen, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// genDir escribe los índices Packages que corresponden al modelo.
func genDir(t *testing.T, m *Model) string {
	t.Helper()
	dir := t.TempDir()
	for _, s := range m.Repo.Suites {
		for _, a := range m.Repo.Architectures {
			var b strings.Builder
			for _, p := range m.Packages {
				for _, v := range p.Versions {
					if contains(v.Suites, s.Name) && contains(v.Architectures, a) {
						fmt.Fprintf(&b, "Package: %s\nVersion: %s\nArchitecture: %s\n\n", p.Name, v.SuiteVersion(s.Name), a)
					}
				}
			}
			p := filepath.Join(dir, "dists", s.Name, m.Repo.Component, "binary-"+a, "Packages")
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// build genera el sitio del modelo en un directorio nuevo.
func build(t *testing.T, m *Model) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "site")
	if err := Build(m, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
