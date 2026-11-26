//go:build integration

package repo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// testEnv prepara una raíz y un <data> temporales con una llave de prueba.
func testEnv(t *testing.T) (*Env, *bytes.Buffer) {
	t.Helper()
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	home, master, sub := testutil.TestKey(t)
	cfg.Repo.Signing.Masters = []string{master}
	cfg.Repo.Signing.Subkey = sub
	data := t.TempDir()
	if err := os.Symlink(home, filepath.Join(data, "gnupg")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	return &Env{Root: t.TempDir(), DataDir: data, Cfg: cfg,
		Out: &report.Printer{Out: &out, Err: &out}}, &out
}

func (e *Env) suites() []string {
	var s []string
	for _, su := range e.Cfg.Suites {
		s = append(s, su.Name)
	}
	return s
}

func poolFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(filepath.Join(dir, "pool"), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			out = append(out, filepath.Base(p))
		}
		return nil
	})
	return out
}

func TestPublish(t *testing.T) {
	ctx := context.Background()
	e, out := testEnv(t)
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.0.0-0~tepache1"))
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "bar", "2.0.0-0~tepache1", "bookworm"))

	versions := map[string]string{"foo": "1.0.0-0~tepache1", "bar": "2.0.0-0~tepache1"}
	if err := e.Publish(ctx, []string{"foo", "bar"}, versions, false); err != nil {
		t.Fatalf("publish: %v\n%s", err, out)
	}
	st, _ := ReadState(e.Root)
	if st.Serial != 1 || len(st.Packages) != 2 {
		t.Fatalf("estado: %+v", st)
	}
	cur, err := Current(e.DataDir)
	if err != nil || cur == nil {
		t.Fatal(err)
	}
	// US1-1: un solo archivo por .deb en pool/, compartido por todas las suites.
	if got := poolFiles(t, cur.Dir(e.DataDir)); len(got) != 4 {
		t.Errorf("pool = %v", got)
	}
	// US1-2: bar no está en bookworm (VerifyTree compara con el estado).
	if p, _ := st.Find("bar", "2.0.0-0~tepache1"); strings.Join(p.Suites, ",") != strings.Join(suiteNames(t)[1:], ",") {
		t.Errorf("suites de bar = %v", p.Suites)
	}
	pk, _ := os.ReadFile(filepath.Join(cur.Dir(e.DataDir), "dists/bookworm/main/binary-amd64/Packages"))
	if strings.Contains(string(pk), "Package: bar") {
		t.Error("bar está en bookworm")
	}
	if err := VerifyTree(ctx, e.Cfg, st, cur.Dir(e.DataDir), time.Now()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := CheckConsistent(e.Root, e.DataDir, st); err != nil {
		t.Fatalf("consistencia: %v", err)
	}

	// US1-4: republicar el mismo .deb no cambia nada.
	out.Reset()
	if err := e.Publish(ctx, []string{"foo"}, versions, false); err != nil || !strings.Contains(out.String(), "sin cambios") {
		t.Fatalf("republicar: %v\n%s", err, out)
	}
	// un .deb distinto con la misma versión falla
	writeRelease(t, e.Root, e.suites(), release{name: "foo", version: "1.0.0-0~tepache1",
		debs: map[string][]byte{"amd64": fakeDeb(t, "foo", "1.0.0-0~tepache1", "amd64"),
			"arm64": fakeDeb(t, "foo", "1.0.0-0~tepache1", "all")}})
	err = e.Publish(ctx, []string{"foo"}, versions, false)
	if err == nil || !strings.Contains(err.Error(), "ya publicada con otro sha256") {
		t.Fatalf("reescribir: %v", err)
	}

	// US1-3: un test.json no aprobado deja estado y current sin cambios.
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "baz", "0.1.0-0~tepache1"))
	editJSON(t, filepath.Join(e.Root, "dist/baz_0.1.0-0~tepache1_amd64.test.json"), func(m map[string]any) { m["overall"] = "fail" })
	before, _ := os.ReadFile(filepath.Join(e.Root, StateFile))
	err = e.Publish(ctx, []string{"baz"}, map[string]string{"baz": "0.1.0-0~tepache1"}, false)
	after, _ := os.ReadFile(filepath.Join(e.Root, StateFile))
	cur2, _ := Current(e.DataDir)
	if err == nil || string(before) != string(after) || cur2.Name != cur.Name {
		t.Fatalf("baz no aprobado: err=%v, estado cambió=%v, current %s→%s", err, string(before) != string(after), cur.Name, cur2.Name)
	}
}

// TestPublishTiming mide publish con 4 paquetes × 3 versiones (plan.md:
// objetivo orientativo < 60 s).
func TestPublishTiming(t *testing.T) {
	ctx := context.Background()
	e, out := testEnv(t)
	names := []string{"p1", "p2", "p3", "p4"}
	var last time.Duration
	for v := 1; v <= 3; v++ {
		versions := map[string]string{}
		for _, n := range names {
			ver := "1.0." + string(rune('0'+v)) + "-0~tepache1"
			writeRelease(t, e.Root, e.suites(), fakeRelease(t, n, ver))
			versions[n] = ver
		}
		start := time.Now()
		if err := e.Publish(ctx, names, versions, false); err != nil {
			t.Fatalf("publish %d: %v\n%s", v, err, out)
		}
		last = time.Since(start)
	}
	t.Logf("publish con 4 paquetes × 3 versiones: %s", last.Round(time.Millisecond))
	if last > 60*time.Second {
		t.Errorf("publish tardó %s (> 60 s)", last)
	}
}
