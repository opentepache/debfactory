//go:build integration

package repo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapshot captura lo que no debe cambiar tras un fallo.
type snapshot struct {
	state   string
	current string
	gens    []string
}

func takeSnapshot(t *testing.T, e *Env) snapshot {
	t.Helper()
	st, _ := os.ReadFile(filepath.Join(e.Root, StateFile))
	cur, _ := os.Readlink(filepath.Join(PublicDir(e.DataDir), "current"))
	gens, _ := filepath.Glob(filepath.Join(PublicDir(e.DataDir), "gen-*"))
	return snapshot{string(st), cur, gens}
}

func (s snapshot) equal(o snapshot) bool {
	return s.state == o.state && s.current == o.current && strings.Join(s.gens, ",") == strings.Join(o.gens, ",")
}

func TestAtomicity(t *testing.T) {
	ctx := context.Background()
	e, out := testEnv(t)
	v1 := map[string]string{"foo": "1.0.0-0~pav1"}
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.0.0-0~pav1"))
	if err := e.Publish(ctx, []string{"foo"}, v1, false); err != nil {
		t.Fatalf("publish inicial: %v\n%s", err, out)
	}
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.1.0-0~pav1"))
	v2 := map[string]string{"foo": "1.1.0-0~pav1"}
	base := takeSnapshot(t, e)

	expectUnchanged := func(t *testing.T, err error, want string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, se esperaba %q", err, want)
		}
		if now := takeSnapshot(t, e); !now.equal(base) {
			t.Fatalf("el repositorio cambió:\nantes %+v\ndespués %+v", base, now)
		}
	}

	t.Run("subllave inexistente", func(t *testing.T) {
		good := e.Cfg.Repo.Signing.Subkey
		e.Cfg.Repo.Signing.Subkey = strings.Repeat("B", 40)
		defer func() { e.Cfg.Repo.Signing.Subkey = good }()
		expectUnchanged(t, e.Publish(ctx, []string{"foo"}, v2, false), "sign: gpg: no hay llave secreta")
	})

	t.Run("verificación falla", func(t *testing.T) {
		e.AfterGenerate = func(gen string) error {
			p := filepath.Join(gen, "dists/trixie/main/binary-amd64/Packages")
			return os.WriteFile(p, []byte("Package: alterado\n"), 0o644)
		}
		defer func() { e.AfterGenerate = nil }()
		expectUnchanged(t, e.Publish(ctx, []string{"foo"}, v2, false), "verify-repo")
	})

	t.Run("otra operación en curso", func(t *testing.T) {
		release, err := Acquire(e.DataDir)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		expectUnchanged(t, e.Publish(ctx, []string{"foo"}, v2, false), "lock-repo: otra operación está en curso")
	})

	t.Run("sin escritura en public", func(t *testing.T) {
		pub := PublicDir(e.DataDir)
		os.Chmod(pub, 0o555)
		defer os.Chmod(pub, 0o755)
		expectUnchanged(t, e.Publish(ctx, []string{"foo"}, v2, false), "generate")
	})

	t.Run("current desincronizado y rebuild", func(t *testing.T) {
		st, _ := ReadState(e.Root)
		st.Serial = 99
		if err := st.Write(e.Root); err != nil {
			t.Fatal(err)
		}
		err := e.Publish(ctx, []string{"foo"}, v2, false)
		if err == nil || !strings.Contains(err.Error(), "current (serial 1) no corresponde a repo/state.yaml (serial 99); ejecuta `debfactory repo rebuild`") {
			t.Fatalf("err = %v", err)
		}
		if err := e.Rebuild(ctx); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		st2, _ := ReadState(e.Root)
		if err := CheckConsistent(e.Root, e.DataDir, st2); err != nil {
			t.Fatalf("tras rebuild: %v", err)
		}
		if err := e.Publish(ctx, []string{"foo"}, v2, false); err != nil {
			t.Fatalf("publicar tras rebuild: %v", err)
		}
	})

	t.Run("rebuild sin almacén", func(t *testing.T) {
		os.RemoveAll(StoreFor(e.DataDir).Dir)
		if err := e.Rebuild(ctx); err != nil {
			t.Fatalf("rebuild desde dist/: %v", err)
		}
		os.Remove(filepath.Join(e.Root, "dist/foo_1.0.0-0~pav1_amd64.deb"))
		os.RemoveAll(StoreFor(e.DataDir).Dir)
		err := e.Rebuild(ctx)
		var list interface{ Error() string }
		if err == nil || !errors.As(err, &list) || !strings.Contains(err.Error(), "falta foo_1.0.0-0~pav1_amd64.deb") {
			t.Fatalf("rebuild sin .deb: %v", err)
		}
	})
}
