//go:build integration

package ci

import (
	"slices"
	"strings"
	"testing"

	"debfactory/internal/ci/citest"
)

// TestCILevel2 (US2, FR-011, FR-015; specs/008-source-builds): main publica
// el paquete de nivel 2 junto a los de nivel 1, con orig y builds en la
// auditoría; al dejar de excluir una suite, la ejecución siguiente la
// amplía con su compilación y las demás no cambian; un fallo deja la
// ampliación pendiente y notificada, y la siguiente la reintenta.
func TestCILevel2(t *testing.T) {
	f := newMainFixtureWith(t, func(files map[string]string) { citest.InstallLevel2(t, files) })
	if err := f.main(); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	st := f.state()
	p, ok := st.Find("tepache-hello", "1.0.0-0~tepache1")
	if !ok || !p.Level2() || !slices.Equal(p.Suites, []string{"bookworm", "trixie", "noble"}) {
		t.Fatalf("estado de nivel 2: %+v\n%s", p, f.out.String())
	}
	audit := f.g.RemoteFile("debfactory/state", AuditFile)
	if !strings.Contains(audit, `"op":"publish","package":"tepache-hello"`) || !strings.Contains(audit, `"builds":{"bookworm":`) {
		t.Errorf("auditoría:\n%s", audit)
	}
	f.mergeStatePR()

	// Dejar de excluir resolute (un PR que cambia el manifiesto); el gate
	// de tepache-hello falla la primera vez.
	f.checkout()
	man := f.g.ReadFile("packages/tepache-hello.yaml")
	f.g.Write("packages/tepache-hello.yaml", strings.Replace(man, "    resolute: acotar la prueba de nivel 2 (tasks T003)\n", "", 1))
	f.g.Commit("tepache-hello en resolute")
	f.g.Git("push", "-q", "origin", "HEAD:main")
	f.failGate = map[string]bool{"tepache-hello": true}
	if err := f.main(); err == nil {
		t.Fatalf("se esperaba el fallo de tepache-hello\n%s", f.out.String())
	}
	if p, _ := f.state().Find("tepache-hello", "1.0.0-0~tepache1"); slices.Contains(p.Suites, "resolute") || !openIssueFor(f, "tepache-hello") {
		t.Fatalf("la ampliación debía quedar pendiente y notificada: %+v", p)
	}
	f.failGate = map[string]bool{}
	if err := f.main(); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	after, _ := f.state().Find("tepache-hello", "1.0.0-0~tepache1")
	if !slices.Equal(after.Suites, []string{"bookworm", "trixie", "noble", "resolute"}) ||
		after.Builds["resolute"].Version != "1.0.0-0~tepache1+u2604" {
		t.Fatalf("ampliación: %+v\n%s", after, f.out.String())
	}
	for _, su := range []string{"bookworm", "trixie", "noble"} {
		if !after.Builds[su].Equal(p.Builds[su]) {
			t.Errorf("%s cambió al ampliar", su)
		}
	}
	audit = f.g.RemoteFile("debfactory/state", AuditFile)
	if !strings.Contains(audit, `"op":"extend","package":"tepache-hello","version":"1.0.0-0~tepache1","suites":["resolute"]`) ||
		!strings.Contains(audit, `"builds":{"resolute":`) {
		t.Errorf("auditoría de la ampliación:\n%s", audit)
	}
	if openIssueFor(f, "tepache-hello") {
		t.Error("el aviso debía cerrarse")
	}
}
