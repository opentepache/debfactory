//go:build e2e

package repotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/build"
	"debfactory/internal/installtest"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/testutil"
)

// TestE2ERebuildToolchain (specs/009, T051, FR-024a): un paquete de nivel 2
// compilado con el toolchain oficial de Zig (zig cc, sha256 y firma minisign
// fijados) se compila dos veces igual, lleva debian/upstream/toolchain en su
// paquete fuente, se publica, y en un contenedor limpio la guía de
// recompilación (con el paso del toolchain) da el mismo .deb.
func TestE2ERebuildToolchain(t *testing.T) {
	ctx := context.Background()
	e := level2EnvFrom(t, filepath.Join(realRoot, "testdata", "fixtures", "level2-zig"))
	start := time.Now()
	if err := e.Build(ctx, "tepache-hello", build.Options{Parallel: true}); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Validate(e.Root, e.Cfg, "tepache-hello")
	if err != nil {
		t.Fatal(err)
	}
	suites := build.Level2Suites(e.Cfg, m)
	if len(suites) != 1 || suites[0].Name != "trixie" {
		t.Fatalf("suites = %v", suites)
	}
	s := suites[0]
	v := m.SuiteVersion(s.Level2Suffix)
	tarball := filepath.Join(e.Root, "dist", "tepache-hello_"+v+".debian.tar.xz")
	out, err := exec.Command("tar", "-xJOf", tarball, "debian/upstream/toolchain").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "minisign_key: RWSGOq2NVecA2UPNdBUZykf1CCb147pkmdtYxgb3Ti+JO/wCYvhbAb/U") ||
		!strings.Contains(string(out), "amd64: {url: https://ziglang.org/download/0.17.0/zig-x86_64-linux-0.17.0.tar.xz") {
		t.Fatalf("debian/upstream/toolchain: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(e.Root, "dist", build.Level2Base(m, s, "arm64")+".deb")); err == nil {
		t.Error("se construyó arm64, que el manifiesto excluye")
	}
	if _, err := installtest.TestPackage(ctx, e.Root, e.Cfg, m, "amd64", installtest.Options{}, e.Out); err != nil {
		t.Fatal(err)
	}

	rcfg := *e.Cfg
	home, master, sub := testutil.TestKey(t)
	rcfg.Repo.Signing.Masters, rcfg.Repo.Signing.Subkey = []string{master}, sub
	data := t.TempDir()
	if err := os.Symlink(home, filepath.Join(data, "gnupg")); err != nil {
		t.Fatal(err)
	}
	re := &repo.Env{Root: e.Root, DataDir: data, Cfg: &rcfg, Out: e.Out}
	if err := re.Publish(ctx, []string{"tepache-hello"}, nil, false); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.ReadState(e.Root)
	if p, ok := st.Find("tepache-hello", m.DebVersion()); !ok || strings.Join(p.ArchList(e.Cfg.Arches), ",") != "amd64" {
		t.Errorf("estado: %+v", p)
	}
	serveCurrent(t, ctx, re)
	user := guide(t, re)
	rebuild, err := Blocks(filepath.Join(e.Root, "docs", "recompilar-nivel2.md"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := StartClient(ctx, s, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	addRepo(t, ctx, c, re, user)
	run := func(step, block string) string {
		t.Helper()
		out, err := c.Script(ctx, Render(block, rcfg.Repo.URL, TestURL, nil))
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return out
	}
	run("install", "apt-get install -y tepache-hello")
	if out := run("run", "tepache-hello"); !strings.Contains(out, "OpenSSL") {
		t.Errorf("tepache-hello: %s", out)
	}
	for _, step := range []string{"srcadd", "source", "builddeps", "toolchain", "rebuild"} {
		out := run(step, rebuild[step])
		if step == "toolchain" && !strings.Contains(out, "Signature and comment signature verified") {
			t.Errorf("el paso del toolchain no verificó la firma:\n%s", out)
		}
	}
	if out := run("compare", rebuild["compare"]); !strings.Contains(out, "idéntico al publicado") {
		t.Errorf("la recompilación con el toolchain no dio el mismo .deb:\n%s", out)
	}
	t.Logf("total %s", time.Since(start).Round(time.Second))
}
