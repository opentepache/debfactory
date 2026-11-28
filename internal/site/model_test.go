package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/repo"
)

func TestLoad(t *testing.T) {
	f := newFixture(t)
	m := f.load(t)
	var names []string
	for _, p := range m.Packages {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "aqua,bob,buf,fnm" {
		t.Errorf("paquetes = %s", got)
	}
	if m.Name != "OpenTepache" || m.URL != "https://apt.opentepache.org" || m.Serial != f.st.Serial ||
		m.Source != "https://github.com/opentepache/debfactory" {
		t.Errorf("cabecera = %q %q %d %q", m.Name, m.URL, m.Serial, m.Source)
	}
	if got := m.Repo.Keyring.Fingerprints; len(got) != 1 || got[0] != "7267 1CD2 2168 4BF7 FDCD  FB39 65B2 7897 5D4F E04B" {
		t.Errorf("fingerprints = %q", got)
	}
	if m.Repo.Suites[3] != (Suite{Name: "sid", Distro: "debian", Label: "Debian sid", Version: "sid", Catchall: true}) ||
		m.Repo.Suites[4] != (Suite{Name: "noble", Distro: "ubuntu", Label: "Ubuntu 24.04", Version: "24.04"}) {
		t.Errorf("suites = %+v", m.Repo.Suites)
	}
	bob := m.Packages[1]
	if len(bob.Versions) != 2 || bob.Versions[0].Version != "4.3.0-0~tepache1" || bob.Versions[1].Version != "4.2.1-0~tepache1" {
		t.Fatalf("versiones de bob = %+v", bob.Versions)
	}
	cur, old := bob.Versions[0], bob.Versions[1]
	if cur.Upstream != "4.3.0" || cur.Published != "2026-11-13" || strings.Join(cur.Suites, ",") != "trixie,forky,sid,noble,resolute" ||
		strings.Join(cur.Architectures, ",") != "amd64,arm64" {
		t.Errorf("actual = %+v", cur)
	}
	if cur.Available != "Debian 13, 14, sid · Ubuntu 24.04, 26.04" || cur.Missing != "Debian 12" {
		t.Errorf("disponibilidad = %q, faltan %q", cur.Available, cur.Missing)
	}
	if m.Updated != "2026-11-13" || bob.Upstream != "MordechaiHadad/bob" {
		t.Errorf("updated %q, upstream %q", m.Updated, bob.Upstream)
	}
	if old.Published != "2026-10-03" { // 23:30 CST es el día siguiente en UTC
		t.Errorf("fecha UTC = %s", old.Published)
	}
	if old.Install != "sudo apt install --allow-downgrades bob=4.2.1-0~tepache1" || bob.Install != "sudo apt install bob" {
		t.Errorf("install = %q, %q", old.Install, bob.Install)
	}
	if len(bob.Excluded) != 1 || bob.Excluded[0].Suite != "bookworm" || !strings.Contains(bob.Excluded[0].Reason, "glibc") {
		t.Errorf("excluded = %+v", bob.Excluded)
	}
	if bob.License != "MIT" || bob.Homepage != "https://github.com/MordechaiHadad/bob" || bob.Summary.ES != nil ||
		bob.Description.ES != nil || len(bob.Description.EN) == 0 {
		t.Errorf("bob = %+v", bob)
	}
}

func TestLoadI18nAndParagraphs(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.root, "packages", "fnm", "fnm.yaml")
	data, _ := os.ReadFile(path)
	extra := "i18n:\n  es:\n    summary: gestor de versiones de Node.js\n    description: |\n      Primer párrafo\n      en dos líneas.\n\n      Segundo párrafo.\n"
	os.WriteFile(path, append(data, extra...), 0o644)
	m := f.load(t)
	fnm := m.Packages[3]
	if fnm.Summary.ES == nil || *fnm.Summary.ES != "gestor de versiones de Node.js" {
		t.Errorf("summary.es = %v", fnm.Summary.ES)
	}
	if strings.Join(fnm.Description.ES, "|") != "Primer párrafo en dos líneas.|Segundo párrafo." {
		t.Errorf("description.es = %q", fnm.Description.ES)
	}
}

// TestLoadSkips: una versión retirada (fuera del estado) y un manifiesto sin
// publicar no aparecen; un paquete publicado sin manifiesto aparece sin
// descripción y con aviso.
func TestLoadSkips(t *testing.T) {
	f := newFixture(t)
	os.WriteFile(filepath.Join(f.root, "packages", "zed.yaml"), []byte("no importa"), 0o644)
	f.st.Packages["ghost"] = []repo.Published{{Version: "1.0.0-0~tepache1", Published: time.Now(),
		Suites: []string{"trixie"}, Debs: map[string]string{"amd64": strings.Repeat("a", 64)}}}
	f.st.Packages["buf"] = nil
	f.sync(t)
	var warns []string
	m, err := Load(f.cfg, f.root, f.st, f.gen, func(s string) { warns = append(warns, s) })
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range m.Packages {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "aqua,bob,fnm,ghost" {
		t.Errorf("paquetes = %s", got)
	}
	ghost := m.Packages[3]
	if ghost.Summary.EN != "" || len(ghost.Description.EN) != 0 || strings.Join(ghost.Versions[0].Architectures, ",") != "amd64" {
		t.Errorf("ghost = %+v", ghost)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "ghost está publicado pero no tiene manifiesto") {
		t.Errorf("avisos = %q", warns)
	}
}

func TestLoadRejects(t *testing.T) {
	t.Run("estado de otra generación", func(t *testing.T) {
		f := newFixture(t)
		f.gen.StateSHA256 = strings.Repeat("0", 64)
		_, err := Load(f.cfg, f.root, f.st, f.gen, nil)
		if err == nil || !strings.Contains(err.Error(), "site: el estado (serial 1) no describe la generación vigente") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("nombre reservado", func(t *testing.T) {
		f := newFixture(t)
		f.st.Packages["assets"] = f.st.Packages["aqua"]
		f.sync(t)
		_, err := Load(f.cfg, f.root, f.st, f.gen, nil)
		if err == nil || !strings.Contains(err.Error(), `"assets" usa un nombre reservado`) {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("sin generación", func(t *testing.T) {
		f := newFixture(t)
		if _, err := Load(f.cfg, f.root, f.st, nil, nil); err == nil {
			t.Error("se esperaba error")
		}
	})
}

// TestLoadPendingStatePR: el estado de referencia puede ser más nuevo que el
// repo/state.yaml del checkout (PR de estado sin integrar); Load usa el que
// recibe y no lee el del árbol.
func TestLoadPendingStatePR(t *testing.T) {
	f := newFixture(t)
	os.MkdirAll(filepath.Join(f.root, "repo"), 0o755)
	os.WriteFile(filepath.Join(f.root, repo.StateFile), []byte("schema: 1\nserial: 0\npackages: {}\n"), 0o644)
	f.st.Serial = 7
	f.sync(t)
	m := f.load(t)
	if m.Serial != 7 || len(m.Packages) != 4 {
		t.Errorf("serial %d, %d paquetes", m.Serial, len(m.Packages))
	}
}

func TestCompareDebian(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"4.3.0-0~tepache1", "4.2.1-0~tepache1", 1},
		{"1.10.0-0~tepache1", "1.9.0-0~tepache3", 1},
		{"1.0.0-0~tepache2", "1.0.0-0~tepache10", -1},
		{"2.0-0~tepache1", "2.0-0~tepache1", 0},
	}
	for _, c := range cases {
		if got := CompareDebian(c.a, c.b); sign(got) != c.want {
			t.Errorf("CompareDebian(%s, %s) = %d", c.a, c.b, got)
		}
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// TestSuiteFromConfigOnly: una suite agregada solo en la configuración aparece
// en el sitio con sus datos, sin cambiar código (specs/007, SC-001).
func TestSuiteFromConfigOnly(t *testing.T) {
	f := newFixture(t)
	f.cfg.Suites = append(f.cfg.Suites[:len(f.cfg.Suites)-1:len(f.cfg.Suites)-1],
		config.Suite{Name: "plucky", Image: "ubuntu:plucky", Level2Suffix: "u2504", Distro: "ubuntu", Label: "Ubuntu 25.04", Version: "25.04"},
		f.cfg.Suites[len(f.cfg.Suites)-1])
	for name := range f.st.Packages {
		f.st.Packages[name][0].Suites = append(f.st.Packages[name][0].Suites, "plucky")
	}
	f.sync(t)
	m := f.load(t)
	var got []string
	for _, s := range m.Repo.Suites {
		got = append(got, s.Name+"="+s.Label)
	}
	if !strings.Contains(strings.Join(got, ","), "noble=Ubuntu 24.04,plucky=Ubuntu 25.04,resolute=Ubuntu 26.04") {
		t.Errorf("suites = %v", got)
	}
	if a := m.Packages[0].Versions[0].Available; a != "Debian 12, 13, 14, sid · Ubuntu 24.04, 25.04, 26.04" {
		t.Errorf("disponibilidad = %q", a)
	}
	if !strings.Contains(m.Commands.Detect.Script, "bookworm|trixie|forky|noble|plucky|resolute") {
		t.Errorf("detectar:\n%s", m.Commands.Detect.Script)
	}
}

// TestPendingSuite: una versión que todavía no está en una suite configurada
// la muestra como no disponible (specs/007, FR-014).
func TestPendingSuite(t *testing.T) {
	f := newFixture(t)
	v := &f.st.Packages["aqua"][0]
	var suites []string
	for _, s := range v.Suites {
		if s != "forky" {
			suites = append(suites, s)
		}
	}
	v.Suites = suites
	f.sync(t)
	cur := f.load(t).Packages[0].Versions[0]
	if cur.Available != "Debian 12, 13, sid · Ubuntu 24.04, 26.04" || cur.Missing != "Debian 14 (testing)" {
		t.Errorf("aqua = %q, faltan %q", cur.Available, cur.Missing)
	}
}
