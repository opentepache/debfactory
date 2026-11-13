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
	"debfactory/internal/fetch"
	"debfactory/internal/repo"
	"debfactory/internal/restore"
)

// TestE2ERebuildFromGit (SC-009): desde un clon limpio, sin dist/ ni caché,
// con la subllave restaurada, repo rebuild reconstruye los .deb desde el
// historial de git y el árbol tiene los mismos .deb y los mismos Packages.
func TestE2ERebuildFromGit(t *testing.T) {
	ctx := context.Background()
	e := e2eEnv(t)
	publishAll(t, ctx, e)
	orig, _ := repo.Current(e.DataDir)

	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "--quiet", realRoot, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	state, _ := os.ReadFile(filepath.Join(e.Root, repo.StateFile))
	os.MkdirAll(filepath.Join(clone, "repo"), 0o755)
	os.WriteFile(filepath.Join(clone, repo.StateFile), state, 0o644)

	data := t.TempDir()
	home, _ := filepath.EvalSymlinks(filepath.Join(e.DataDir, "gnupg"))
	os.Symlink(home, filepath.Join(data, "gnupg"))
	cache, work := t.TempDir(), t.TempDir()
	e2 := &repo.Env{Root: clone, DataDir: data, Cfg: e.Cfg, Out: e.Out}
	e2.Restore = restore.Func(clone, func(root string) *build.Env {
		b := &build.Env{Root: root, Workdir: work, Cfg: e.Cfg, Out: e.Out,
			Fetcher: fetch.New(cache, fetch.DefaultTimeout, nil),
			GitHub:  &fetch.GitHub{Token: os.Getenv("GITHUB_TOKEN")}}
		b.Defaults()
		return b
	})
	if err := e2.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild desde git: %v", err)
	}
	got, _ := repo.Current(data)
	n := 0
	for p, sha := range orig.Files {
		if !strings.HasPrefix(p, "pool/") && !strings.HasSuffix(p, "/Packages") {
			continue
		}
		n++
		if got.Files[p] != sha {
			t.Errorf("%s difiere tras reconstruir", p)
		}
	}
	t.Logf("comparados %d archivos de pool/ y Packages", n)
}
