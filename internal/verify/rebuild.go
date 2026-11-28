package verify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
)

var (
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	commitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// rebuildFromSource clona el repositorio de paquetes al commit del manifiesto
// y construye el paquete con esta herramienta, en contenedores; devuelve el
// sha256 del .deb de la suite y arquitectura.
func rebuildFromSource(ctx context.Context, pv *Provenance, suite, arch, work string) (string, error) {
	if !repoRe.MatchString(pv.Repository) || !commitRe.MatchString(pv.ManifestCommit) {
		return "", fmt.Errorf("repositorio %q o commit %q no válidos", pv.Repository, pv.ManifestCommit)
	}
	root := filepath.Join(work, "packages")
	for _, args := range [][]string{
		{"init", "-q", root},
		{"-C", root, "fetch", "-q", "--depth", "1", "https://github.com/" + pv.Repository + ".git", pv.ManifestCommit},
		{"-C", root, "checkout", "-q", "FETCH_HEAD"},
	} {
		if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git %v: %v: %s", args[len(args)-1], err, out)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return "", err
	}
	out := filepath.Join(work, "rebuild")
	e := &build.Env{Root: root, Workdir: filepath.Join(work, "tmp"), Cfg: cfg,
		Fetcher: fetch.New(filepath.Join(work, "cache"), fetch.DefaultTimeout, nil),
		GitHub:  &fetch.GitHub{}, Out: &report.Printer{Out: os.Stderr, Err: os.Stderr}, Dist: out}
	e.Defaults()
	m, err := manifest.Validate(root, cfg, pv.Package)
	if err != nil {
		return "", err
	}
	if !m.Level2() {
		if err := e.Build(ctx, pv.Package, build.Options{Arch: arch}); err != nil {
			return "", err
		}
		return fetch.SHA256File(filepath.Join(out, m.DebBase(arch)+".deb"))
	}
	src := filepath.Join(work, "source")
	if err := e.Level2Source(ctx, pv.Package, src); err != nil {
		return "", err
	}
	bout := filepath.Join(work, "binary")
	if err := e.Level2BuildOne(ctx, pv.Package, suite, arch, 0, filepath.Join(src, suite), bout, build.Options{Disk: true}); err != nil {
		return "", err
	}
	var hb build.HostedBuild
	if err := readJSONFile(filepath.Join(bout, "build.json"), &hb); err != nil {
		return "", err
	}
	return hb.SHA256, nil
}
