package site

import (
	"strings"
	"testing"
)

const provJSON = `{
  "package": "bob",
  "version": "4.3.0-0~tepache1",
  "level": 1,
  "upstream": {"files": {"https://github.com/MordechaiHadad/bob/releases/download/v4.3.0/bob-linux-x86_64.zip": "aa11"}},
  "manifest_commit": "c0ffee",
  "run_url": "https://github.com/opentepache/packages/actions/runs/7",
  "attestations": [{"subject": "bob_4.3.0-0~tepache1_amd64.deb", "digest": "bb22", "rekor_url": "https://search.sigstore.dev/?hash=bb22"}],
  "tests": [{"suite": "trixie", "arch": "amd64", "status": "pass"}],
  "audit_seq": 12,
  "verified_by_pc": {"mode": "hosted", "attestations": true, "lintian": true, "rebuild_level1": true, "smoke_amd64": true},
  "supply": [{"package": "bob", "version": "4.3.0", "arch": "amd64", "signature": {"type": "none", "ok": false}, "provenance": {"present": false, "ok": false, "count": 0}}]
}
`

// TestProvenancePage (specs/009, T036): la página de procedencia de cada
// versión en los dos idiomas, provenance.json publicado byte a byte y enlaces
// desde la página del paquete.
func TestProvenancePage(t *testing.T) {
	m := newFixture(t).load(t)
	var p *Package
	for i := range m.Packages {
		if m.Packages[i].Name == "bob" {
			p = &m.Packages[i]
		}
	}
	if p == nil {
		t.Fatal("el fixture no tiene bob")
	}
	v := p.Versions[0].Version
	p.Versions[0].ProvenanceFile = []byte(provJSON)
	out := build(t, m)
	rel := "packages/bob/" + v + "/"
	if got := read(t, out, rel+"provenance.json"); got != provJSON {
		t.Errorf("provenance.json publicado no es el original:\n%s", got)
	}
	en := read(t, out, rel+"index.html")
	for _, want := range []string{
		"Provenance of bob " + v, "upstream does not sign its releases", "not published by upstream",
		"https://github.com/opentepache/packages/actions/runs/7", `href="https://search.sigstore.dev/?hash=bb22"`,
		"Built and tested on GitHub", "the maintainer&#39;s machine rebuilt the .deb", "debfactory verify bob " + v,
		`<a href="provenance.json">`, "#12",
	} {
		if !strings.Contains(en, want) {
			t.Errorf("falta %q en la página de procedencia", want)
		}
	}
	es := read(t, out, "es/"+rel+"index.html")
	if !strings.Contains(es, "Procedencia de bob "+v) || !strings.Contains(es, "upstream no firma sus versiones") {
		t.Error("falta la página de procedencia en español")
	}
	pkg := read(t, out, "packages/bob/index.html")
	if !strings.Contains(pkg, `href="/packages/bob/`+v+`/"`) {
		t.Error("la página del paquete no enlaza a la procedencia")
	}
}

// TestArchExclusionShown (specs/009, T043): la página del paquete muestra la
// arquitectura excluida y su motivo, y la versión solo sus arquitecturas.
func TestArchExclusionShown(t *testing.T) {
	m := newFixture(t).load(t)
	p := &m.Packages[0]
	p.ExcludedArches = []ArchExclusion{{Arch: "arm64", Reason: "does not fit in the standard runner"}}
	p.Versions[0].Architectures = []string{"amd64"}
	out := build(t, m)
	page := read(t, out, "packages/"+p.Name+"/index.html")
	if !strings.Contains(page, "not built for arm64") || !strings.Contains(page, "does not fit in the standard runner") {
		t.Error("falta la exclusión de arquitectura en la página del paquete")
	}
	es := read(t, out, "es/packages/"+p.Name+"/index.html")
	if !strings.Contains(es, "no se compila para arm64") {
		t.Error("falta la exclusión en español")
	}
}
