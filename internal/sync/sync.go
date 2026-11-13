package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"debfactory/internal/repo"
	"debfactory/internal/report"
)

// RecordPath es el registro de la última sincronización a un destino.
func RecordPath(dataDir, dest string) string {
	return filepath.Join(dataDir, "sync", dest+".json")
}

// ReadRecord lee el registro; nil si nunca se sincronizó.
func ReadRecord(dataDir, dest string) (*Record, error) {
	data, err := os.ReadFile(RecordPath(dataDir, dest))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", RecordPath(dataDir, dest), err)
	}
	return &r, nil
}

func writeRecord(dataDir, dest string, r *Record) error {
	p := RecordPath(dataDir, dest)
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

// Sync sube la generación vigente al destino destName (FR-025–FR-029). El
// registro solo se actualiza al terminar la fase inrelease: si se
// interrumpe, la siguiente sincronización vuelve a subir lo que falte
// (FR-027).
func Sync(ctx context.Context, e *repo.Env, destName string, dryRun bool) error {
	dest, ok := e.Cfg.Repo.Destinations[destName]
	if !ok {
		return report.Newf(report.Sync, "%s: destino desconocido (repo.destinations)", destName)
	}
	if dest.CacheControl && e.Cfg.Repo.URL == "" {
		return report.Newf(report.Sync, "%s: repo.url está vacío; no se sincroniza un destino público sin URL", destName)
	}
	if err := checkRclone(); err != nil {
		return err
	}
	release, err := repo.Acquire(e.DataDir)
	if err != nil {
		return err
	}
	defer release()
	cur, err := repo.Current(e.DataDir)
	if err != nil {
		return report.Newf(report.Sync, "%v", err)
	}
	if cur == nil {
		return report.Newf(report.Sync, "%s: no hay nada publicado; ejecuta `debfactory publish` antes", destName)
	}
	last, err := ReadRecord(e.DataDir, destName)
	if err != nil {
		return report.Newf(report.Sync, "%v", err)
	}
	phases := Plan(e.Cfg, dest, cur, last)
	if Unchanged(phases) && last != nil && last.Generation == cur.Name {
		e.Out.Event("unchanged", map[string]any{"dest": destName}, fmt.Sprintf("%s: sin cambios (serial %d)", destName, cur.Serial))
		return nil
	}
	srcDir := cur.Dir(e.DataDir)
	var rec *Record
	for _, ph := range phases {
		files := len(ph.Files())
		ev := map[string]any{"dest": destName, "phase": ph.Name, "files": files, "changed": ph.Changed, "dry_run": dryRun}
		e.Out.Event("upload", ev, fmt.Sprintf("%s: %-9s %4d archivos (%d nuevos o cambiados)", destName, ph.Name, files, ph.Changed))
		if dryRun || (files == 0 && ph.Name != PhaseInRelease) {
			continue
		}
		for _, b := range ph.Batches {
			if err := runBatch(ctx, destName, dest, srcDir, ph.Name, b); err != nil {
				return err
			}
		}
		switch ph.Name {
		case PhaseInRelease:
			// Los índices nuevos ya están: se registra la generación y los
			// borrados pendientes antes de hacerlos.
			rec = &Record{Serial: cur.Serial, Generation: cur.Name, Files: cur.Files,
				Pending: Deletions(cur, last)}
			if last != nil {
				rec.PreviousFiles = last.Files
				if last.Generation == cur.Name {
					rec.PreviousFiles = last.PreviousFiles
				}
			}
			if err := writeRecord(e.DataDir, destName, rec); err != nil {
				return report.Newf(report.Sync, "%v", err)
			}
		case PhaseDelete:
			rec.Pending = nil
			if err := writeRecord(e.DataDir, destName, rec); err != nil {
				return report.Newf(report.Sync, "%v", err)
			}
		}
		if Hook != nil {
			if err := Hook(ph.Name); err != nil {
				return err
			}
		}
	}
	if !dryRun {
		e.Out.Event("synced", map[string]any{"dest": destName, "serial": cur.Serial},
			fmt.Sprintf("%s: sincronizado serial %d (%s)", destName, cur.Serial, cur.Name))
	}
	return nil
}
