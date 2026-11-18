package ci

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"debfactory/internal/ci/citest"
	"debfactory/internal/forge"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

type stateFixture struct {
	t    *testing.T
	g    *citest.Git
	gh   *citest.FakeGitHub
	s    *StatePR
	base string // state.yaml de main
	seq  int
}

func newStateFixture(t *testing.T) *stateFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), "repo/state.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	g := citest.GitFixture(t, map[string]string{"repo/state.yaml": string(data), "README.md": "x\n"})
	gh := citest.NewFakeGitHub(t)
	client, err := forge.New(gh.URL, citest.Repo, secretToken)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t)
	var out bytes.Buffer
	s := &StatePR{Root: g.Work, Cfg: cfg, Dir: filepath.Join(t.TempDir(), "ci"), GH: client,
		Out: &report.Printer{Out: &out, Err: &out}}
	return &stateFixture{t: t, g: g, gh: gh, s: s, base: string(data)}
}

// publish simula una publicación: el checkout recibe un state.yaml con el
// serial dado y una entrada de auditoría más, y se guarda la copia.
func (f *stateFixture) publish(serial int) {
	f.t.Helper()
	st := strings.Replace(f.base, "serial: 1\n", "serial: "+itoa(serial)+"\n", 1)
	f.g.Write("repo/state.yaml", st)
	f.seq++
	if _, err := AppendAudit(f.g.Work, Entry{Time: time.Now(), Op: OpPublish, Package: "fnm",
		Version: "1.40.0-0~tepache" + itoa(serial), Commit: strings.Repeat("a", 40),
		Debs: map[string]string{"amd64": sha1, "arm64": sha2}, Serial: serial, Run: runURL("u")}); err != nil {
		f.t.Fatal(err)
	}
	if err := f.s.SaveCopy(); err != nil {
		f.t.Fatal(err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func (f *stateFixture) reconcile() (*Ref, error) {
	f.t.Helper()
	return f.s.Reconcile(context.Background())
}

func (f *stateFixture) mustReconcile(source string) *Ref {
	f.t.Helper()
	ref, err := f.reconcile()
	if err != nil {
		f.t.Fatalf("Reconcile: %v", err)
	}
	if ref.Source != source {
		f.t.Fatalf("referencia %s; se esperaba %s", ref.Source, source)
	}
	return ref
}

func wantStateErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), ": state: ") || !strings.Contains(err.Error(), msg) {
		t.Fatalf("err = %v; se esperaba la etapa state con %q", err, msg)
	}
}

// TestStatePRLifecycle recorre las transiciones de data-model.md §3.
func TestStatePRLifecycle(t *testing.T) {
	f := newStateFixture(t)
	ctx := context.Background()

	// Sin copia ni rama: la referencia es main.
	f.mustReconcile("main")

	// Publicar: se abre el único PR de estado con los dos archivos.
	f.publish(2)
	pr, err := f.s.Push(ctx)
	if err != nil || pr == nil {
		t.Fatalf("Push: %v %v", pr, err)
	}
	if got := f.g.RemoteFile("debfactory/state", "repo/state.yaml"); !strings.Contains(got, "serial: 2") {
		t.Fatalf("rama de estado sin el estado nuevo:\n%s", got)
	}
	if prs := f.gh.OpenPRs(); len(prs) != 1 || !strings.HasPrefix(prs[0].Title, "repo: estado tras serial 2 (fnm 1.40.0-0~tepache2)") {
		t.Fatalf("PRs = %+v", prs)
	}
	// pr.yml no corre en un PR del GITHUB_TOKEN: la ejecución marca verify
	// sobre la punta de la rama para que el ruleset deje integrarlo.
	tip := f.g.RemoteBranch("debfactory/state")
	if got := f.gh.Statuses[tip]["verify"]; got != "success" {
		t.Fatalf("verify sobre %s = %q; estados %v", tip, got, f.gh.Statuses)
	}
	// La rama coincide con la copia: la referencia es el PR (FR-007a).
	ref := f.mustReconcile("state-pr")
	if !strings.Contains(string(ref.State), "serial: 2") || ref.PR == nil {
		t.Fatalf("ref = %s %v", ref.State, ref.PR)
	}

	// Otra publicación con el PR abierto: se actualiza el mismo PR.
	f.publish(3)
	if _, err := f.s.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if prs := f.gh.OpenPRs(); len(prs) != 1 || !strings.Contains(prs[0].Title, "serial 3") {
		t.Fatalf("nunca dos PR de estado: %+v", prs)
	}
	if tip := f.g.RemoteBranch("debfactory/state"); f.gh.Statuses[tip]["verify"] != "success" {
		t.Fatalf("el PR actualizado no tiene verify en su punta %s", tip)
	}
	// Un estado que no llegó (fallo de la API) se completa en la siguiente
	// conciliación con el PR abierto.
	delete(f.gh.Statuses, f.g.RemoteBranch("debfactory/state"))
	f.mustReconcile("state-pr")
	if tip := f.g.RemoteBranch("debfactory/state"); f.gh.Statuses[tip]["verify"] != "success" {
		t.Fatal("la conciliación no repuso verify")
	}

	// Push interrumpido: la copia va por delante de la rama; se completa (FR-029).
	f.publish(4)
	ref = f.mustReconcile("state-pr")
	if got := f.g.RemoteFile("debfactory/state", "repo/audit.jsonl"); strings.Count(got, "\n") != 3 {
		t.Fatalf("la rama no se volvió a empujar:\n%s", got)
	}

	// El mantenedor integra el PR: la referencia vuelve a ser main y la rama se borra.
	f.g.MergeBranch("debfactory/state")
	f.gh.ClosePR(f.gh.OpenPRs()[0].Number, true)
	f.mustReconcile("main")
	if f.g.RemoteBranch("debfactory/state") != "" {
		t.Error("la rama de estado no se borró")
	}
}

func TestStatePRDiscrepancies(t *testing.T) {
	ctx := context.Background()

	t.Run("PR cerrado sin integrar", func(t *testing.T) {
		f := newStateFixture(t)
		f.publish(2)
		pr, err := f.s.Push(ctx)
		if err != nil {
			t.Fatal(err)
		}
		f.gh.ClosePR(pr.Number, false)
		_, err = f.reconcile()
		wantStateErr(t, err, "restore-state-pr")
	})
	t.Run("rama editada a mano", func(t *testing.T) {
		f := newStateFixture(t)
		f.publish(2)
		if _, err := f.s.Push(ctx); err != nil {
			t.Fatal(err)
		}
		m := f.g.Clone()
		m.Git("checkout", "-q", "-b", "edit", "origin/debfactory/state")
		m.Write("repo/state.yaml", strings.Replace(f.base, "serial: 1", "serial: 9", 1))
		m.Commit("edición a mano")
		m.Git("push", "-q", "-f", "origin", "HEAD:refs/heads/debfactory/state")
		_, err := f.reconcile()
		wantStateErr(t, err, "no coincide con lo publicado")
	})
	t.Run("main con otro estado", func(t *testing.T) {
		f := newStateFixture(t)
		f.publish(2)
		if _, err := f.s.Push(ctx); err != nil {
			t.Fatal(err)
		}
		m := f.g.Clone()
		m.Write("repo/audit.jsonl", `{"seq":1}`+"\n")
		m.Commit("audit ajeno")
		m.Push()
		_, err := f.reconcile()
		wantStateErr(t, err, "no es prefijo")
	})
	t.Run("rama sin copia", func(t *testing.T) {
		f := newStateFixture(t)
		f.g.Git("push", "-q", "origin", "HEAD:refs/heads/debfactory/state")
		_, err := f.reconcile()
		wantStateErr(t, err, "no hay copia del pipeline")
	})
	t.Run("PR que no llegó a abrirse", func(t *testing.T) {
		f := newStateFixture(t)
		f.publish(2)
		if _, err := f.s.Push(ctx); err != nil {
			t.Fatal(err)
		}
		// Simula que la ejecución murió entre el push y el PR.
		f.gh.ClosePR(f.gh.OpenPRs()[0].Number, false)
		p := f.s.ReadPushed()
		p.PRNumber = 0
		f.s.writePushed(p)
		ref := f.mustReconcile("state-pr")
		if ref.PR == nil || len(f.gh.OpenPRs()) != 1 {
			t.Fatalf("no se abrió el PR pendiente")
		}
	})
}
