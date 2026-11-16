package ci

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/repo"
)

func stateWith(pubs map[string][]string, retired ...[2]string) *repo.State {
	st := repo.Empty()
	st.Serial = 1
	for name, versions := range pubs {
		for _, v := range versions {
			st.Packages[name] = append(st.Packages[name], repo.Published{Version: v, Published: time.Now(),
				Suites: []string{"trixie"}, Debs: map[string]string{"amd64": "a", "arm64": "b"}})
		}
	}
	for _, r := range retired {
		st.Retired = append(st.Retired, repo.Retired{Package: r[0], Version: r[1], Reason: "x"})
	}
	return st
}

// TestClassify: una fila de la tabla de conciliación de data-model.md §2 por caso.
func TestClassify(t *testing.T) {
	st := stateWith(map[string][]string{"fnm": {"1.39.0-0~tepache2", "1.39.0-0~tepache1"}},
		[2]string{"fnm", "1.40.0-0~tepache1"})
	cases := []struct {
		pkg, version, reason, skip, err string
	}{
		{"nuevo", "1.0.0-0~tepache1", NewPackage, "", ""},
		{"fnm", "1.41.0-0~tepache1", NewVersion, "", ""},
		{"fnm", "1.39.0-0~tepache3", NewRevision, "", ""},
		{"fnm", "1.39.0-0~tepache2", "", "publicado", ""},
		{"fnm", "1.39.0-0~tepache1", "", "publicado", ""},
		{"fnm", "1.40.0-0~tepache1", "", "", "versión retirada; sube revision"},
		{"fnm", "1.38.0-0~tepache5", "", "", "versión anterior a la publicada"},
	}
	for _, c := range cases {
		reason, skip, perr := Classify(st, c.pkg, c.version)
		if reason != c.reason || skip != c.skip {
			t.Errorf("%s %s: reason=%q skip=%q", c.pkg, c.version, reason, skip)
		}
		switch {
		case c.err == "" && perr != nil:
			t.Errorf("%s %s: error inesperado %v", c.pkg, c.version, perr.Reason)
		case c.err != "" && (perr == nil || !strings.Contains(perr.Reason, c.err) || perr.Stage != "state"):
			t.Errorf("%s %s: error = %+v; se esperaba %q", c.pkg, c.version, perr, c.err)
		}
	}
}

// TestPlanMainRepo: con el estado real, los 4 paquetes ya están publicados.
func TestPlanMainRepo(t *testing.T) {
	cfg := testConfig(t)
	st, err := repo.ReadState("../..")
	if err != nil {
		t.Fatal(err)
	}
	p, err := PlanMain("../..", cfg, st, Reference{Source: "main"}, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Publish) != 0 || len(p.Errors) != 0 || len(p.Skipped) != 4 {
		t.Errorf("plan = %s (%+v)", p.Summary(), p.Errors)
	}
}

// fixtureRoot copia config/, packages/ y repo/ a un directorio temporal.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"config", "packages", "repo", "schema"} {
		if out, err := exec.Command("cp", "-a", filepath.Join("../..", d), root).CombinedOutput(); err != nil {
			t.Fatalf("cp %s: %v %s", d, err, out)
		}
	}
	return root
}

func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s no contiene %q", path, old)
	}
	os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644)
}

// TestPlanMainFixture: revision nueva, lockfile sin la versión e
// independencia entre paquetes (FR-004).
func TestPlanMainFixture(t *testing.T) {
	cfg := testConfig(t)
	root := fixtureRoot(t)
	editFile(t, filepath.Join(root, "packages/fnm.yaml"), "revision: 1", "revision: 2")
	editFile(t, filepath.Join(root, "packages/bob.yaml"), "version: 4.3.0", "version: 9.9.9")
	st, _ := repo.ReadState(root)
	p, err := PlanMain(root, cfg, st, Reference{Source: "main"}, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Publish) != 1 || p.Publish[0] != (AffectedPackage{"fnm", "1.39.0-0~tepache2", "abc", NewRevision}) {
		t.Errorf("publish = %+v", p.Publish)
	}
	if len(p.Errors) != 1 || p.Errors[0].Package != "bob" || !strings.Contains(p.Errors[0].Reason, "regenera el lockfile") {
		t.Errorf("errors = %+v", p.Errors)
	}
	if len(p.Skipped) != 2 {
		t.Errorf("skipped = %+v", p.Skipped)
	}
}

// TestPlanPR: solo manifiesto, solo código, código + paquete y nada relevante.
func TestPlanPR(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	root := fixtureRoot(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "base")
	st, _ := repo.ReadState(root)
	plan := func() *Plan {
		t.Helper()
		p, err := PlanPR(ctx, root, cfg, "main", st)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	git("checkout", "-qb", "pr")

	if p := plan(); len(p.Test)+len(p.Compare)+len(p.Errors) != 0 {
		t.Errorf("sin cambios: %s", p.Summary())
	}
	editFile(t, filepath.Join(root, "packages/fnm.yaml"), "revision: 1", "revision: 2")
	git("commit", "-qam", "fnm")
	if p := plan(); len(p.Test) != 1 || p.Test[0].Package != "fnm" || len(p.Compare) != 0 {
		t.Errorf("solo manifiesto: %s", p.Summary())
	}
	os.MkdirAll(filepath.Join(root, "internal/x"), 0o755)
	os.WriteFile(filepath.Join(root, "internal/x/x.go"), []byte("package x\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "code")
	p := plan()
	if len(p.Test) != 1 || len(p.Compare) != 3 || len(p.CodeDiff) != 1 {
		t.Errorf("código + paquete: %s (%v)", p.Summary(), p.CodeDiff)
	}
	git("checkout", "-q", "main")
	git("checkout", "-qb", "solo-codigo")
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	editFile(t, filepath.Join(root, "config/debfactory.yaml"), "keep: 3 ", "keep: 3  ")
	git("commit", "-qam", "config")
	if p := plan(); len(p.Test) != 0 || len(p.Compare) != 4 {
		t.Errorf("solo código: %s", p.Summary())
	}
	// Un lockfile cambiado sin subir revision: se compara (FR-010).
	git("checkout", "-q", "main")
	git("checkout", "-qb", "lock")
	f, _ := os.OpenFile(filepath.Join(root, "packages/aqua.lock.yaml"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("# cambio\n")
	f.Close()
	git("commit", "-qam", "lock")
	if p := plan(); len(p.Test) != 0 || len(p.Compare) != 1 || p.Compare[0].Package != "aqua" {
		t.Errorf("lockfile de una versión publicada: %s", p.Summary())
	}
}
