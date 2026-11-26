package site

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHomePage(t *testing.T) {
	m := newFixture(t).load(t)
	out := build(t, m)
	home := read(t, out, "index.html")
	for _, want := range []string{
		`<html lang="en">`,
		"<title>OpenTepache: recent developer tools for Debian and Ubuntu</title>",
		"7267 1CD2 2168 4BF7 FDCD  FB39 65B2 7897 5D4F E04B",
		`data-debfactory-step="add"`, `data-debfactory-step="add-debian"`, `data-debfactory-step="add-ubuntu"`,
		`data-debfactory-step="verify-key"`, `data-debfactory-step="remove"`,
		`<input type="radio" name="distro" id="d-detect" checked>`,
		`<a class="lang" href="/es/" hreflang="es" lang="es">Español</a>`,
		`<link rel="alternate" hreflang="x-default" href="https://apt.opentepache.org/">`,
		`<link rel="canonical" href="https://apt.opentepache.org/">`,
		// Tabla: sin descripción, disponibilidad en texto y excepciones aparte (FR-004).
		`<tr><th scope="row"><a href="/packages/bob/">bob</a></th><td class="ver">4.3.0</td><td class="when"><time datetime="2026-11-13">2026-11-13</time></td><td>Debian 13, 14, sid · Ubuntu 24.04, 26.04<span class="except">not on Debian 12</span></td><td class="arch">amd64, arm64</td></tr>`,
		`<td>Debian 12, 13, 14, sid · Ubuntu 24.04, 26.04</td>`,
		`<button class="copy" type="button" hidden data-copy="cmd-add"`,
		`<button class="theme" type="button" hidden`,
	} {
		if !strings.Contains(home, want) {
			t.Errorf("falta %q en la portada", want)
		}
	}
	if strings.Contains(home, "version manager for Neovim") {
		t.Error("la tabla de la portada no debe llevar descripciones (FR-004)")
	}
}

func TestHomeEmpty(t *testing.T) {
	f := newFixture(t)
	for k := range f.st.Packages {
		delete(f.st.Packages, k)
	}
	f.sync(t)
	out := build(t, f.load(t))
	home := read(t, out, "index.html")
	if !strings.Contains(home, "No packages are published yet.") || !strings.Contains(home, `data-debfactory-step="add"`) {
		t.Error("portada vacía incompleta")
	}
	if _, err := os.Stat(filepath.Join(out, "packages")); !os.IsNotExist(err) {
		t.Error("no debería haber páginas de paquete")
	}
}

// TestEscape: los textos de los manifiestos salen como texto (FR-010).
func TestEscape(t *testing.T) {
	m := newFixture(t).load(t)
	m.Packages[0].Summary.EN = `<script>alert(1)</script>`
	m.Packages[0].Description.EN = []string{`<img src=x onerror=alert(1)>`}
	m.Packages[0].Homepage = `javascript:alert(1)`
	out := build(t, m)
	page := read(t, out, "packages/aqua/index.html")
	if strings.Contains(page, "<script>alert") || strings.Contains(page, "<img src=x") || strings.Contains(page, `href="javascript:`) {
		t.Error("contenido sin escapar")
	}
	if !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("el resumen no se escapó como texto")
	}
	// En el JSON-LD, < y > van como \u003c y \u003e: no pueden cerrar el script.
	ld := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(page)
	if ld == nil || strings.Contains(ld[1], "<") {
		t.Errorf("JSON-LD inseguro: %v", ld)
	}
}

func TestPackagePage(t *testing.T) {
	m := newFixture(t).load(t)
	out := build(t, m)
	for _, p := range m.Packages {
		if _, err := os.Stat(filepath.Join(out, "packages", p.Name, "index.html")); err != nil {
			t.Errorf("falta la página de %s", p.Name)
		}
	}
	bob := read(t, out, "packages/bob/index.html")
	for _, want := range []string{
		"<title>Install bob 4.3.0 on Debian and Ubuntu (.deb) | OpenTepache</title>",
		`<meta name="description" content="Signed .deb of bob (version manager for Neovim) for Debian 13, 14, sid · Ubuntu 24.04, 26.04.`,
		"<h1>Install bob on Debian and Ubuntu</h1>",
		`<span>version manager for Neovim</span>`,
		`<dd class="ver">4.3.0</dd>`, "<code>4.3.0-0~tepache1</code>", `<time datetime="2026-11-13">2026-11-13</time>`,
		"<dd>MIT</dd>", `<a href="https://github.com/MordechaiHadad/bob" rel="noopener">MordechaiHadad/bob</a>`,
		`<code id="cmd-install">sudo apt install bob</code>`,
		`<tr><th scope="row">Debian 12 <span class="when">(bookworm)</span></th><td class="no">no</td><td class="no">no</td></tr>`,
		`<tr><th scope="row">Debian 13 <span class="when">(trixie)</span></th><td class="yes">yes</td><td class="yes">yes</td></tr>`,
		`not on Debian 12: <span lang="en">Upstream builds on Ubuntu 24.04 and requires glibc &gt;= 2.39`,
		"Built from the official 4.3.0 release of MordechaiHadad/bob, checked with lintian and tested by installing it on Debian 13, 14, sid · Ubuntu 24.04, 26.04",
		"PREVIOUS VERSIONS", "<code>sudo apt install --allow-downgrades bob=4.2.1-0~tepache1</code>",
		`<a href="/#add">`, `<li aria-current="page">bob</li>`, "BOB(1)",
	} {
		if !strings.Contains(bob, want) {
			t.Errorf("falta %q en la página de bob", want)
		}
	}
	if strings.Contains(read(t, out, "packages/aqua/index.html"), "PREVIOUS VERSIONS") {
		t.Error("aqua no tiene versiones anteriores")
	}
}

func TestPackageWithoutManifest(t *testing.T) {
	m := newFixture(t).load(t)
	p := &m.Packages[0]
	p.Summary.EN, p.Description.EN, p.License, p.Homepage, p.Upstream = "", []string{}, "", "", ""
	out := build(t, m)
	page := read(t, out, "packages/aqua/index.html")
	if strings.Contains(page, `class="desc"`) || strings.Contains(page, "Built from") || !strings.Contains(page, `<dd class="ver">2.66.1</dd>`) {
		t.Error("página sin manifiesto incorrecta")
	}
}

// TestSEO: títulos, JSON-LD, sitemap y robots (FR-025, FR-026).
func TestSEO(t *testing.T) {
	m := newFixture(t).load(t)
	out := build(t, m)
	// JSON-LD válido en cada página.
	files, _ := Files(out)
	for _, n := range files {
		if !strings.HasSuffix(n, ".html") {
			continue
		}
		page := read(t, out, n)
		ld := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(page)
		var v map[string]any
		if ld == nil || json.Unmarshal([]byte(ld[1]), &v) != nil {
			t.Errorf("%s: JSON-LD ausente o inválido", n)
		}
		if !strings.Contains(page, `<meta property="og:image" content="https://apt.opentepache.org/assets/og.`) {
			t.Errorf("%s: falta og:image", n)
		}
	}
	bob := read(t, out, "es/packages/bob/index.html")
	for _, want := range []string{
		"<title>Instalar bob 4.3.0 en Debian y Ubuntu (.deb) | OpenTepache</title>",
		`"@type":"SoftwareApplication"`, `"softwareVersion":"4.3.0"`, `"license":"https://spdx.org/licenses/MIT"`,
		`"operatingSystem":"Debian 13, Debian 14 (testing), Debian sid, Ubuntu 24.04, Ubuntu 26.04"`, `"@type":"BreadcrumbList"`,
		`<link rel="canonical" href="https://apt.opentepache.org/es/packages/bob/">`,
	} {
		if !strings.Contains(bob, want) {
			t.Errorf("falta %q en es/packages/bob", want)
		}
	}
	// sitemap.xml: XML válido, todas las páginas en los dos idiomas.
	var sm struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal([]byte(read(t, out, "sitemap.xml")), &sm); err != nil {
		t.Fatal(err)
	}
	if len(sm.URLs) != 2*(1+len(m.Packages)) {
		t.Errorf("sitemap con %d URL", len(sm.URLs))
	}
	locs := map[string]string{}
	for _, u := range sm.URLs {
		locs[u.Loc] = u.LastMod
	}
	if locs["https://apt.opentepache.org/es/packages/bob/"] != "2026-11-13" || locs["https://apt.opentepache.org/"] != m.Updated {
		t.Errorf("sitemap: %v", locs)
	}
	robots := read(t, out, "robots.txt")
	if !strings.Contains(robots, "Sitemap: https://apt.opentepache.org/sitemap.xml") || !strings.Contains(robots, "Disallow: /pool/") {
		t.Errorf("robots.txt:\n%s", robots)
	}
}

func TestBuildRejects(t *testing.T) {
	m := newFixture(t).load(t)
	m.Packages[0].Name = "es"
	if err := Build(m, filepath.Join(t.TempDir(), "s")); err == nil || !strings.Contains(err.Error(), `"es" no válido`) {
		t.Errorf("nombre reservado: %v", err)
	}
	for _, p := range []string{"dists/x", "pool/main/a.deb", "debfactory.gpg", "../x", "/abs"} {
		if checkPath(p) == nil {
			t.Errorf("checkPath(%q) debería fallar", p)
		}
	}
	if checkPath("es/packages/bob/index.html") != nil || checkPath("assets/site.0123.css") != nil || checkPath("sitemap.xml") != nil {
		t.Error("rutas válidas rechazadas")
	}
	m = newFixture(t).load(t)
	m.Packages[0].Description.EN = []string{strings.Repeat("x", MaxPageBytes)}
	if err := Build(m, filepath.Join(t.TempDir(), "s")); err == nil || !strings.Contains(err.Error(), "FR-022") {
		t.Errorf("presupuesto: %v", err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x"), nil, 0o644)
	if err := Build(newFixture(t).load(t), dir); err == nil || !strings.Contains(err.Error(), "no está vacío") {
		t.Errorf("directorio con contenido: %v", err)
	}
}

func TestLanguages(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.root, "packages", "fnm.yaml")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, append(data, "i18n:\n  es:\n    summary: gestor de versiones de Node.js\n"...), 0o644)
	m := f.load(t)
	out := build(t, m)
	files, err := Files(out)
	if err != nil {
		t.Fatal(err)
	}
	var en, es []string
	for _, n := range files {
		if !strings.HasSuffix(n, ".html") {
			continue
		}
		if strings.HasPrefix(n, "es/") {
			es = append(es, strings.TrimPrefix(n, "es/"))
		} else {
			en = append(en, n)
		}
	}
	if strings.Join(en, ",") != strings.Join(es, ",") || len(en) != 1+len(m.Packages) {
		t.Fatalf("páginas en = %v, es = %v", en, es)
	}
	data2 := regexp.MustCompile(`<code[^>]*>[^<]*</code>|<time[^>]*>[^<]*</time>|<td class="ver">[^<]*</td>`)
	for _, n := range en {
		pe, ps := read(t, out, n), read(t, out, "es/"+n)
		if !strings.Contains(pe, `<html lang="en">`) || !strings.Contains(ps, `<html lang="es">`) {
			t.Errorf("%s: lang", n)
		}
		if a, b := data2.FindAllString(pe, -1), data2.FindAllString(ps, -1); strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("%s: datos distintos entre idiomas:\n%v\n%v", n, a, b)
		}
		rel := strings.TrimSuffix(n, "index.html")
		if !strings.Contains(pe, `href="/es/`+rel+`" hreflang="es"`) || !strings.Contains(ps, `href="/`+rel+`" hreflang="en"`) {
			t.Errorf("%s: falta el enlace al otro idioma", n)
		}
	}
	if !strings.Contains(read(t, out, "es/packages/fnm/index.html"), "<span>gestor de versiones de Node.js</span>") {
		t.Error("falta el resumen traducido de fnm")
	}
	esBob := read(t, out, "es/packages/bob/index.html")
	if !strings.Contains(esBob, `<span lang="en">version manager for Neovim</span>`) {
		t.Error("el resumen sin traducir de bob debe ir con lang=en")
	}
	if !strings.Contains(esBob, `<div lang="en" class="desc">`) {
		t.Error("la descripción sin traducir debe ir con lang=en")
	}
}

func TestDeterministic(t *testing.T) {
	m := newFixture(t).load(t)
	a, b := build(t, m), build(t, m)
	da, err := Digest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := Digest(b)
	if da != db {
		t.Error("dos Build del mismo modelo difieren")
	}
	m2 := newFixture(t).load(t)
	if dc, _ := Digest(build(t, m2)); dc != da {
		t.Error("dos Load del mismo estado difieren")
	}
	m.Serial++
	if dd, _ := Digest(build(t, m)); dd == da {
		t.Error("el digest no cambia con el contenido")
	}
}

func TestI18nKeys(t *testing.T) {
	strs, err := LoadI18n()
	if err != nil {
		t.Fatal(err)
	}
	if strs["en"]["lang_name"] != "English" || strs["es"]["lang_name"] != "Español" {
		t.Error("lang_name")
	}
	if verbs("a %s b %d %%") != "sd%" {
		t.Errorf("verbs = %q", verbs("a %s b %d %%"))
	}
}
