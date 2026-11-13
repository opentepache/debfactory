// Package restore reconstruye desde el historial de git un .deb publicado
// que ya no está ni en el almacén ni en dist/ (repo rebuild, FR-036,
// research D5): busca el commit cuyo manifiesto tiene esa versión, lo saca
// en un worktree temporal y lo construye. Como los builds son reproducibles,
// el sha256 debe coincidir con repo/state.yaml.
package restore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"debfactory/internal/build"
	"debfactory/internal/fetch"
	"debfactory/internal/repo"
)

var (
	versionRe = regexp.MustCompile(`(?m)^version:\s*"?([^"\s#]+)`)
	pavRe     = regexp.MustCompile(`(?m)^pav:\s*(\d+)`)
)

// manifestVersion devuelve la versión Debian de un manifiesto en texto.
func manifestVersion(data []byte) string {
	v, p := versionRe.FindSubmatch(data), pavRe.FindSubmatch(data)
	if v == nil || p == nil {
		return ""
	}
	return fmt.Sprintf("%s-0~pav%s", v[1], p[1])
}

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// FindCommit devuelve el commit más reciente cuyo packages/<name>.yaml
// declara version (versión Debian completa).
func FindCommit(ctx context.Context, root, name, version string) (string, error) {
	rel := "packages/" + name + ".yaml"
	out, err := git(ctx, root, "log", "--format=%H", "--", rel)
	if err != nil {
		return "", err
	}
	for _, c := range strings.Fields(string(out)) {
		data, err := git(ctx, root, "show", c+":"+rel)
		if err == nil && manifestVersion(data) == version {
			return c, nil
		}
	}
	return "", fmt.Errorf("ningún commit de %s declara la versión %s", rel, version)
}

// Func devuelve un repo.Env.Restore que construye con newEnv(root), un
// build.Env para la raíz dada (normalmente el worktree temporal).
func Func(root string, newEnv func(root string) *build.Env) func(ctx context.Context, store repo.Store, name, version, arch, sha string) error {
	return func(ctx context.Context, store repo.Store, name, version, arch, sha string) error {
		commit, err := FindCommit(ctx, root, name, version)
		if err != nil {
			return err
		}
		tmp, err := os.MkdirTemp("", "debfactory-restore-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		wt := filepath.Join(tmp, "wt")
		if _, err := git(ctx, root, "worktree", "add", "--detach", wt, commit); err != nil {
			return err
		}
		defer git(context.Background(), root, "worktree", "remove", "--force", wt)
		if err := newEnv(wt).Build(ctx, name, build.Options{Arch: arch}); err != nil {
			return fmt.Errorf("build en %s: %v", commit[:12], err)
		}
		deb := filepath.Join(wt, "dist", repo.DebFile(name, version, arch))
		got, err := fetch.SHA256File(deb)
		if err != nil {
			return err
		}
		if got != sha {
			return fmt.Errorf("el build reproducible no coincide con repo/state.yaml (sha %s…, se esperaba %s…)", got[:12], sha[:12])
		}
		return store.Put(deb, sha)
	}
}
