package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"debfactory/internal/config"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/supply"
)

// SupplyReview es el comentario de aviso de un PR que cambia la versión de
// un paquete (specs/009, research D13, FR-017, FR-018): diff determinista del
// fuente entre los tags de las dos versiones, resumen con GitHub Models (si
// hay token y cuota) y Scorecard. Nunca bloquea: los fallos de la IA o de
// Scorecard salen como "no disponible".
func (e *Env) SupplyReview(ctx context.Context, name, from, to, token string) (string, error) {
	m, err := manifest.Validate(e.Root, e.Cfg, name)
	if err != nil {
		return "", err
	}
	fail := func(err error) error { return report.Wrap(err, report.Fetch, m.Name, to, "") }
	work, err := e.workDir(m.Name + "-review-")
	if err != nil {
		return "", fail(err)
	}
	defer os.RemoveAll(work)
	dirs := map[string]string{}
	for _, v := range []string{from, to} {
		mm := *m
		mm.Version = v
		tag, err := mm.RenderedTag()
		if err != nil {
			return "", fail(err)
		}
		url := "https://github.com/" + m.Upstream + "/archive/refs/tags/" + tag + ".tar.gz"
		tmp, _, err := e.Fetcher.Get(ctx, url, "")
		if err != nil {
			return "", fail(fmt.Errorf("%s: %v", url, err))
		}
		dir := filepath.Join(work, v)
		os.MkdirAll(dir, 0o755)
		out, err := exec.CommandContext(ctx, "tar", "-xzf", tmp, "-C", dir, "--strip-components=1", "--no-same-owner").CombinedOutput()
		os.Remove(tmp)
		if err != nil {
			return "", fail(fmt.Errorf("extraer %s: %v: %s", url, err, out))
		}
		dirs[v] = dir
	}
	d, err := supply.DiffTrees(dirs[from], dirs[to])
	if err != nil {
		return "", fail(err)
	}
	det := d.Markdown(m.Name, from, to)
	model := e.Cfg.Watch.AIModel
	if model == "" {
		model = config.DefaultAIModel
	}
	summary, _ := supply.Summarize(ctx, nil, "", token, model, det)
	var sc *supply.Scorecard
	if e.Scorecard != nil {
		sc = e.Scorecard(ctx, m.Upstream)
	}
	return supply.ReviewMarkdown(det, summary, sc, m.Upstream), nil
}
