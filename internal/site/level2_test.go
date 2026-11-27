package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/repo"
)

// addLevel2 publica en el estado del fixture el paquete de prueba de nivel 2
// en bookworm y trixie, con su manifiesto.
func (f *fixture) addLevel2(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/level2/tepache-hello.yaml")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(f.root, "packages", "tepache-hello.yaml"), data, 0o644)
	h := strings.Repeat("a", 64)
	b := func(v string) repo.Build {
		return repo.Build{Version: v, DSC: h, DebianTar: h, Debs: map[string]string{"amd64": h, "arm64": h},
			Buildinfo: map[string]string{"amd64": h, "arm64": h}}
	}
	f.st.Packages["tepache-hello"] = []repo.Published{{Version: "1.0.0-0~tepache1", Level: 2,
		Published: time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC), Suites: []string{"bookworm", "trixie"},
		Orig:   &repo.Orig{File: "tepache-hello_1.0.0.orig.tar.gz", SHA256: h},
		Builds: map[string]repo.Build{"bookworm": b("1.0.0-0~tepache1+deb12"), "trixie": b("1.0.0-0~tepache1+deb13")}}}
	f.sync(t)
}

// TestLevel2Page (FR-012, research D13): la página de un paquete de nivel 2
// muestra la versión de cada suite y cómo bajar el código fuente; Verify
// compara cada suite con su versión.
func TestLevel2Page(t *testing.T) {
	f := newFixture(t)
	f.addLevel2(t)
	m := f.load(t)
	if err := Verify(m, genDir(t, m)); err != nil {
		t.Fatal(err)
	}
	page := read(t, build(t, m), "packages/tepache-hello/index.html")
	for _, want := range []string{
		`(bookworm)</span> <code>1.0.0-0~tepache1&#43;deb12</code></th><td class="yes">`, // html/template escapa "+"
		`(trixie)</span> <code>1.0.0-0~tepache1&#43;deb13</code></th>`,
		`<code id="cmd-source">apt-get source tepache-hello</code>`,
		`<h2 id="h-source">source code</h2>`, "<code>1.0.0-0~tepache1+…</code>",
		`<code id="cmd-install">sudo apt install tepache-hello</code>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("falta %q en la página de tepache-hello", want)
		}
	}
	if strings.Contains(read(t, build(t, m), "packages/bob/index.html"), "cmd-source") {
		t.Error("un paquete de nivel 1 no muestra código fuente")
	}
}
