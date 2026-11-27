//go:build e2e

package srcbuild

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/srcpkg"
	"debfactory/internal/testutil"
)

// TestE2ESourceAndDoubleBuild genera el paquete fuente de tepache-hello para
// bookworm y lo compila dos veces con entornos distintos (research D1′, D8):
// los dos .deb son idénticos y el .dsc no cambia al regenerarlo.
// DEBFACTORY_SRCBUILD_SUITE y _ARCH eligen otra combinación.
func TestE2ESourceAndDoubleBuild(t *testing.T) {
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	suiteName, arch := os.Getenv("DEBFACTORY_SRCBUILD_SUITE"), os.Getenv("DEBFACTORY_SRCBUILD_ARCH")
	if suiteName == "" {
		suiteName = "bookworm"
	}
	if arch == "" {
		arch = HostArch()
	}
	s, ok := cfg.Suite(suiteName)
	if !ok {
		t.Fatalf("suite %s", suiteName)
	}
	fix := filepath.Join(testutil.ProjectRoot(), "..", "fixtures", "level2")
	version := "1.0.0-0~tepache1+" + s.Level2Suffix
	in := srcpkg.Input{Name: "tepache-hello", Upstream: "1.0.0", Version: version, Suite: s.Name,
		Maintainer: cfg.Maintainer, Epoch: 1767225600,
		Orig: filepath.Join(fix, "tepache-hello-1.0.0.tar.gz"), OrigName: "tepache-hello_1.0.0.orig.tar.gz",
		DebianDir: filepath.Join(fix, "tepache-hello", "debian")}
	ctx := context.Background()
	src, err := GenerateSource(ctx, s, in, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	again, err := GenerateSource(ctx, s, in, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{src.DSC, src.DebianTar, src.Orig} {
		a, _ := fetch.SHA256File(filepath.Join(src.Dir, f))
		b, _ := fetch.SHA256File(filepath.Join(again.Dir, f))
		if a != b {
			t.Errorf("%s no es reproducible: %s ≠ %s", f, a, b)
		}
	}
	shas := []string{}
	for variant := range 2 {
		bin, err := Build(ctx, Spec{Suite: s, Arch: arch, Source: src, Tree: in.TreeDir(), Package: in.Name,
			Version: version, Epoch: in.Epoch, Tmpfs: "2g", Variant: variant}, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		sha, _ := fetch.SHA256File(bin.Deb)
		shas = append(shas, sha)
		bi, _ := os.ReadFile(bin.Buildinfo)
		if !strings.Contains(string(bi), "Build-Path: "+BuildPath) {
			t.Errorf(".buildinfo sin Build-Path fijo:\n%s", bi)
		}
	}
	if shas[0] != shas[1] {
		t.Fatalf("no reproducible: %s ≠ %s", shas[0], shas[1])
	}
	t.Logf("%s/%s: %s", s.Name, arch, shas[0])
}
