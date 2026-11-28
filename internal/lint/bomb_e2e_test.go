//go:build e2e

package lint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/testutil"
)

// TestLintianXMLBomb: lintian termina con un fuente que trae un XML malicioso.
func TestLintianXMLBomb(t *testing.T) {
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Lintian.Timeout = config.Duration(3 * time.Minute)
	dir := t.TempDir()
	src := filepath.Join(dir, "bomb-1.0")
	files := map[string]string{
		"debian/source/format": "3.0 (quilt)\n",
		"debian/control": "Source: bomb\nSection: misc\nPriority: optional\nMaintainer: A B <a@b.example>\n" +
			"Build-Depends: debhelper-compat (= 13)\nStandards-Version: 4.7.0\n\nPackage: bomb\nArchitecture: all\n" +
			"Description: test\n test\n",
		"debian/changelog": "bomb (1.0-1) unstable; urgency=medium\n\n  * x\n\n -- A B <a@b.example>  Thu, 01 Jan 2026 00:00:00 +0000\n",
		"debian/rules":     "#!/usr/bin/make -f\n%:\n\tdh $@\n",
		"debian/copyright": "Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\n\nFiles: *\n" +
			"Copyright: 2026 A B\nLicense: MIT\n Text.\n",
	}
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(src, p)), 0o755)
		os.WriteFile(filepath.Join(src, p), []byte(c), 0o644)
	}
	// test/recurse/ de libxml2: la "billion laughs" real (testdata/lint/recurse).
	bombs, _ := filepath.Glob(filepath.Join(testutil.RepoRoot(), "testdata", "lint", "recurse", "*"))
	if len(bombs) < 5 {
		t.Fatal("faltan los archivos de testdata/lint/recurse")
	}
	for _, b := range bombs {
		data, _ := os.ReadFile(b)
		os.MkdirAll(filepath.Join(src, "test", "recurse"), 0o755)
		os.WriteFile(filepath.Join(src, "test", "recurse", filepath.Base(b)), data, 0o644)
	}
	if out, err := exec.Command("tar", "--exclude=debian", "-C", dir, "-czf", filepath.Join(dir, "bomb_1.0.orig.tar.gz"), "bomb-1.0").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	cmd := exec.Command("dpkg-source", "-b", "bomb-1.0")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	start := time.Now()
	_, err = Run(context.Background(), cfg, filepath.Join(dir, "bomb_1.0-1.dsc"))
	if d := time.Since(start); d > 2*time.Minute {
		t.Fatalf("lintian tardó %s (err %v)", d, err)
	}
	if err != nil && strings.Contains(err.Error(), "límite") {
		t.Fatal(err)
	}
}
