package ci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/site"
	reposync "debfactory/internal/sync"
)

// site es el paso final de ci main, ci retire y ci refresh
// (specs/006-repo-website, research D5): genera el sitio web desde la
// generación vigente con el estado de referencia del checkout y lo sube a
// ci.destination. Corre también cuando no hay nada que publicar, para subir
// los cambios que solo tocan el sitio y reparar un sitio atrasado. Un fallo se
// notifica (etapa site) y hace terminar la ejecución con error, pero nunca
// revierte ni bloquea lo ya publicado (FR-013).
func (x *run) site(ctx context.Context) {
	if !x.Cfg.Site.Enabled {
		return
	}
	x.setStage(report.Site)
	if err := x.siteUpload(ctx); err != nil {
		x.failErr(ctx, err, report.Site, "", "")
		return
	}
	x.notify.StagePassed(ctx, report.Site)
}

func (x *run) siteUpload(ctx context.Context) error {
	cur, err := repo.Current(x.dataDir())
	if err != nil {
		return report.Newf(report.Site, "%v", err)
	}
	if cur == nil {
		x.Out.Line("site: no hay ninguna generación publicada; nada que generar")
		return nil
	}
	// prepare dejó en el checkout el estado de referencia (que puede venir de
	// un PR de estado sin integrar) y publish/retire lo actualizaron.
	st, err := repo.ReadState(x.Root)
	if err != nil {
		return err
	}
	m, err := site.FromData(x.Cfg, x.Root, st, x.dataDir(), func(s string) { x.Out.Line("%s", s) })
	if err != nil {
		return err
	}
	if TestHookSite != nil {
		if err := TestHookSite(); err != nil {
			return err
		}
	}
	dir, err := site.BuildDigest(m, filepath.Join(x.dataDir(), "site"))
	if err != nil {
		return err
	}
	dest := x.Cfg.CI.Destination
	res, err := reposync.SyncSite(ctx, x.Repo, dest, dir, m.Serial, false)
	if err != nil {
		return err
	}
	if res.Unchanged {
		x.Out.Event("site", map[string]any{"serial": m.Serial, "unchanged": true},
			fmt.Sprintf("site: serial %d sin cambios; nada que subir", m.Serial))
		return nil
	}
	pages, largest := pageStats(dir)
	x.Out.Event("site", map[string]any{"serial": m.Serial, "pages": pages, "uploaded": res.Uploaded, "deleted": res.Deleted},
		fmt.Sprintf("site: serial %d, %d páginas, %d KB la mayor; %s: subidas %d, borradas %d",
			m.Serial, pages, (largest+1023)/1024, dest, res.Uploaded, res.Deleted))
	return nil
}

// TestHookSite, si no es nil, se llama antes de generar el sitio; solo lo
// usan los tests para simular un fallo del paso site.
var TestHookSite func() error

func pageStats(dir string) (pages, largest int) {
	names, _ := site.Files(dir)
	for _, n := range names {
		if !strings.HasSuffix(n, ".html") {
			continue
		}
		pages++
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(n))); err == nil && int(info.Size()) > largest {
			largest = int(info.Size())
		}
	}
	return pages, largest
}
