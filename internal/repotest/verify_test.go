//go:build e2e

package repotest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/build"
	"debfactory/internal/ci"
	"debfactory/internal/container"
	"debfactory/internal/fetch"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/site"
	"debfactory/internal/testutil"
)

// TestE2EVerify (specs/009, T038, SC-007): publica los paquetes de nivel 1
// en el repositorio de prueba con su provenance.json, sirve repositorio y
// sitio, y `debfactory verify` desde un contenedor limpio sin credenciales da
// "verificado"; con el .deb servido alterado, "no verificado". La
// reconstrucción se comprueba en el host desde el árbol de prueba (no hay un
// repositorio de GitHub que clonar).
func TestE2EVerify(t *testing.T) {
	ctx := context.Background()
	e := e2eEnv(t)
	st := publishAll(t, ctx, e)
	const pkg = "bob"
	p := st.Packages[pkg][0]
	m, err := manifest.Validate(e.Root, e.Cfg, pkg)
	if err != nil {
		t.Fatal(err)
	}
	lk, _ := lock.Read(e.Root, pkg)
	pv, err := ci.BuildProvenance(e.Root, m, lk, p, strings.Repeat("a", 40), "", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	pv.Repository = e.Cfg.CI.Repository
	if _, err := ci.WriteProvenance(e.DataDir, pv); err != nil {
		t.Fatal(err)
	}
	sm, err := site.FromData(e.Cfg, e.Root, st, e.DataDir, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "site")
	if err := site.Build(sm, tmp); err != nil {
		t.Fatal(err)
	}
	www := filepath.Join(t.TempDir(), "www")
	gen, _ := filepath.EvalSymlinks(filepath.Join(repo.PublicDir(e.DataDir), "current"))
	copyDir(t, gen, www)
	copyDir(t, tmp, www)
	srv, err := StartServer(ctx, e.Cfg, www, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)

	// La herramienta, compilada para el contenedor limpio.
	bin := filepath.Join(t.TempDir(), "debfactory")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/debfactory")
	cmd.Dir = testutil.RepoRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	id, err := container.Start(ctx, container.Opts{Image: "debian:trixie", Network: Network,
		Mounts: []container.Mount{{Src: bin, Dst: "/usr/local/bin/debfactory", ReadOnly: true}}})
	if err != nil {
		t.Fatal(err)
	}
	defer container.Remove(id)
	if res, err := container.Exec(ctx, id, []string{"DEBIAN_FRONTEND=noninteractive"}, []string{"sh", "-c",
		"apt-get update -qq && apt-get install -y -qq gpg gpgv xz-utils >/dev/null"}); err != nil || res.ExitCode != 0 {
		t.Fatalf("herramientas del contenedor: %v %s", err, res.Stderr)
	}
	verifyIn := func() (string, int) {
		res, err := container.Exec(ctx, id, nil, []string{"debfactory", "verify", pkg, p.Version,
			"--site", TestURL, "--suite", "trixie", "--arch", "amd64", "--no-rebuild"})
		if err != nil {
			t.Fatal(err)
		}
		return string(res.Stdout) + string(res.Stderr), res.ExitCode
	}
	out, code := verifyIn()
	if code != 0 || !strings.Contains(out, "verificado") || strings.Contains(out, "no verificado") {
		t.Fatalf("verify (código %d):\n%s", code, out)
	}
	t.Logf("verify:\n%s", out)

	// Reconstrucción en el host desde el árbol de prueba: mismo sha256.
	b := &build.Env{Root: e.Root, Workdir: t.TempDir(), Cfg: e.Cfg,
		Fetcher: fetch.New(filepath.Join(os.TempDir(), "debfactory-e2e-cache"), fetch.DefaultTimeout, nil),
		GitHub:  &fetch.GitHub{}, Out: e.Out, Dist: t.TempDir()}
	b.Defaults()
	if err := b.Build(ctx, pkg, build.Options{Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := fetch.SHA256File(filepath.Join(b.Dist, m.DebBase("amd64")+".deb")); got != p.Debs["amd64"] {
		t.Errorf("reconstrucción: %s, publicado %s", got, p.Debs["amd64"])
	}

	// .deb servido alterado: no verificado.
	// El pool nombra los archivos por su sha256.
	var debs []string
	filepath.WalkDir(filepath.Join(www, "pool"), func(path string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == p.Debs["amd64"]+".deb" {
			debs = append(debs, path)
		}
		return nil
	})
	if len(debs) != 1 {
		t.Fatalf("debs servidos = %v", debs)
	}
	os.Chmod(debs[0], 0o644)
	if err := os.WriteFile(debs[0], []byte("alterado"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = verifyIn()
	if code == 0 || !strings.Contains(out, "no verificado") {
		t.Fatalf("alterado (código %d):\n%s", code, out)
	}
	t.Logf("alterado:\n%s", out)
}
