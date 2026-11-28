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
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// TestE2EGhosttyLocal (specs/009, T019): lock real de ghostty 1.4.0 (firma
// minisign, tarball contra tag, Zig 0.17.0 verificado, dependencias de Zig
// una por una) y compilación sin red en trixie/amd64, dos veces igual; la
// prueba de instalación ejecuta `ghostty +version`. Usa la red real; tarda
// (receta just e2e-ghostty).
func TestE2EGhosttyLocal(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyDir(t, filepath.Join(testutil.ProjectRoot(), "config"), filepath.Join(root, "config"))
	copyDir(t, filepath.Join(realRoot, "testdata", "fixtures", "ghostty"), filepath.Join(root, "packages", "ghostty"))
	os.Chmod(filepath.Join(root, "packages/ghostty/debian/rules"), 0o755)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	w := captureWriter{testWriter{t}}
	e := &build.Env{Root: root, Workdir: t.TempDir(), Cfg: cfg,
		Fetcher: fetch.New(filepath.Join(os.TempDir(), "debfactory-e2e-ghostty-cache"), 10*time.Minute, nil),
		GitHub:  &fetch.GitHub{Token: os.Getenv("GITHUB_TOKEN")},
		Out:     &report.Printer{Out: w, Err: w}}
	e.Defaults()
	start := time.Now()
	if err := e.Lock(ctx, "ghostty", false); err != nil {
		t.Fatal(err)
	}
	lk, _ := lock.Read(root, "ghostty")
	en := lk.Versions["1.4.0"]
	if len(en.Deps) < 20 || len(en.Toolchain) != 2 || len(en.ExtraFiles) != 4 || en.ZigdepsSHA256 == "" {
		t.Fatalf("lockfile incompleto: deps %d, toolchain %v, extras %v", len(en.Deps), en.Toolchain, en.ExtraFiles)
	}
	locked := time.Since(start)
	if err := e.Build(ctx, "ghostty", build.Options{Suite: "trixie", Arch: "amd64", Disk: true, Parallel: true}); err != nil {
		t.Fatal(err)
	}
	built := time.Since(start)
	m, _ := manifest.Validate(root, cfg, "ghostty")
	s, _ := cfg.Suite("trixie")
	deb := filepath.Join(root, "dist", build.Level2Base(m, s, "amd64")+".deb")
	out, err := exec.Command("dpkg-deb", "-f", deb, "Version", "Depends").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "1.4.0-0~tepache1+deb13") || !strings.Contains(string(out), "libgtk-4") {
		t.Fatalf("dpkg-deb: %v\n%s", err, out)
	}
	if _, err := installtest.TestPackage(ctx, root, cfg, m, "amd64", installtest.Options{Only: []string{"trixie"}}, e.Out); err != nil {
		// Conserva el .deb y el informe para revisar el fallo.
		keep := filepath.Join(os.TempDir(), "debfactory-e2e-ghostty-fallo")
		os.RemoveAll(keep)
		os.MkdirAll(keep, 0o755)
		for _, f := range []string{deb, strings.TrimSuffix(deb, ".deb") + ".test.json"} {
			if b, rerr := os.ReadFile(f); rerr == nil {
				os.WriteFile(filepath.Join(keep, filepath.Base(f)), b, 0o644)
				if strings.HasSuffix(f, ".json") {
					t.Logf("%s:\n%s", filepath.Base(f), b)
				}
			}
		}
		t.Fatalf("%v (copia en %s)", err, keep)
	}
	t.Logf("lock %s, compilación (2 veces) %s, total %s", locked.Round(time.Second), (built - locked).Round(time.Second),
		time.Since(start).Round(time.Second))
}
