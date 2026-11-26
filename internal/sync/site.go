package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"debfactory/internal/fetch"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/site"
)

// Fases de la subida del sitio (specs/006-repo-website, research D5): los
// estilos (nombre con hash, inmutables) antes que las páginas que los usan, y
// el borrado diferido al final.
const (
	SitePhaseAssets = "assets"
	SitePhasePages  = "pages"
	SitePhaseDelete = "delete"
)

// SiteRecord es lo último subido del sitio a un destino
// (<data>/sync/<dest>.site.json, data-model §3).
type SiteRecord struct {
	Schema        int               `json:"schema"`
	Serial        int               `json:"serial"`
	Digest        string            `json:"digest"`
	Files         map[string]string `json:"files"`             // ruta -> sha256 del sitio subido
	PreviousFiles map[string]string `json:"previous_files"`    // el anterior
	Pending       []string          `json:"pending,omitempty"` // borrados calculados aún sin hacer
	Time          time.Time         `json:"time"`
}

// SiteResult resume una subida del sitio.
type SiteResult struct {
	Unchanged bool
	Uploaded  int // archivos nuevos o cambiados
	Deleted   int
}

// SiteRecordPath es el registro del sitio de un destino.
func SiteRecordPath(dataDir, dest string) string {
	return filepath.Join(dataDir, "sync", dest+".site.json")
}

// ReadSiteRecord lee el registro del sitio; nil si nunca se subió.
func ReadSiteRecord(dataDir, dest string) (*SiteRecord, error) {
	data, err := os.ReadFile(SiteRecordPath(dataDir, dest))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r SiteRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", SiteRecordPath(dataDir, dest), err)
	}
	return &r, nil
}

func writeSiteRecord(dataDir, dest string, r *SiteRecord) error {
	p := SiteRecordPath(dataDir, dest)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(r, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// SiteDeletions es lo que se borra al subir cur: lo del sitio anterior al
// último que ya no está ni en el último ni en el actual, más lo que quedó
// pendiente (borrado diferido, como el repositorio).
func SiteDeletions(cur map[string]string, last *SiteRecord) []string {
	if last == nil {
		return nil
	}
	set := map[string]bool{}
	for p := range last.PreviousFiles {
		set[p] = true
	}
	for _, p := range last.Pending {
		set[p] = true
	}
	var out []string
	for p := range set {
		if _, ok := cur[p]; ok {
			continue
		}
		if _, ok := last.Files[p]; ok {
			continue
		}
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// SyncSite sube el árbol del sitio siteDir (generado por site.Build) al
// destino destName (research D5, D6): primero assets/, después las páginas y
// site.json, y al final el borrado diferido. El registro se escribe al
// terminar las páginas; si algo falla antes, la siguiente subida ve un digest
// distinto y vuelve a subir (FR-013). Si el digest coincide con el registro y
// no hay borrados pendientes, no sube nada.
func SyncSite(ctx context.Context, e *repo.Env, destName, siteDir string, serial int, dryRun bool) (*SiteResult, error) {
	fail := func(format string, args ...any) error {
		return report.Newf(report.Site, "%s: "+format, append([]any{destName}, args...)...)
	}
	dest, ok := e.Cfg.Repo.Destinations[destName]
	if !ok {
		return nil, fail("destino desconocido (repo.destinations)")
	}
	if err := checkRclone(); err != nil {
		return nil, err
	}
	digest, err := site.Digest(siteDir)
	if err != nil {
		return nil, fail("%v", err)
	}
	names, err := site.Files(siteDir)
	if err != nil {
		return nil, fail("%v", err)
	}
	cur := map[string]string{}
	for _, n := range names {
		sha, err := fetch.SHA256File(filepath.Join(siteDir, filepath.FromSlash(n)))
		if err != nil {
			return nil, fail("%v", err)
		}
		cur[n] = sha
	}
	release, err := repo.Acquire(e.DataDir)
	if err != nil {
		return nil, err
	}
	defer release()
	last, err := ReadSiteRecord(e.DataDir, destName)
	if err != nil {
		return nil, fail("%v", err)
	}
	if last != nil && last.Digest == digest && len(last.Pending) == 0 {
		return &SiteResult{Unchanged: true}, nil
	}

	var assets, pages []string
	res := &SiteResult{}
	for _, n := range names {
		if last == nil || last.Files[n] != cur[n] {
			res.Uploaded++
		}
		if strings.HasPrefix(n, "assets/") {
			assets = append(assets, n)
		} else {
			pages = append(pages, n)
		}
	}
	gone := SiteDeletions(cur, last)
	res.Deleted = len(gone)
	immutable, mutable := "", ""
	if dest.CacheControl {
		immutable, mutable = e.Cfg.Repo.CacheControl.Immutable, e.Cfg.Repo.CacheControl.Mutable
	}
	phases := []struct {
		name  string
		batch Batch
	}{
		{SitePhaseAssets, Batch{CacheControl: immutable, Files: assets}},
		{SitePhasePages, Batch{CacheControl: mutable, Files: pages}},
		{SitePhaseDelete, Batch{Files: gone}},
	}
	for _, ph := range phases {
		e.Out.Event("site-upload", map[string]any{"dest": destName, "phase": ph.name, "files": len(ph.batch.Files), "dry_run": dryRun},
			fmt.Sprintf("%s: sitio %-6s %4d archivos", destName, ph.name, len(ph.batch.Files)))
		if dryRun || len(ph.batch.Files) == 0 && ph.name != SitePhasePages {
			continue
		}
		if len(ph.batch.Files) > 0 {
			rphase := ph.name
			if ph.name == SitePhaseDelete {
				rphase = PhaseDelete
			}
			if err := runBatch(ctx, destName, dest, siteDir, rphase, ph.batch); err != nil {
				cause := err.Error()
				var re *report.Error
				if errors.As(err, &re) {
					cause = re.Cause // ya empieza por el destino
				}
				return nil, report.New(report.Site, cause)
			}
		}
		switch ph.name {
		case SitePhasePages:
			rec := &SiteRecord{Schema: 1, Serial: serial, Digest: digest, Files: cur, Pending: gone,
				Time: time.Now().UTC().Truncate(time.Second)}
			if last != nil {
				rec.PreviousFiles = last.Files
				if last.Digest == digest {
					rec.PreviousFiles = last.PreviousFiles
				}
			}
			if err := writeSiteRecord(e.DataDir, destName, rec); err != nil {
				return nil, fail("%v", err)
			}
			last = rec
		case SitePhaseDelete:
			last.Pending = nil
			if err := writeSiteRecord(e.DataDir, destName, last); err != nil {
				return nil, fail("%v", err)
			}
		}
		if SiteHook != nil {
			if err := SiteHook(ph.name); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// SiteHook, si no es nil, se llama tras cada fase de la subida del sitio; solo
// lo usan los tests para simular un fallo.
var SiteHook func(phase string) error
