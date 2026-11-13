package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"debfactory/internal/sign"
)

// Status muestra lo publicado y lo sincronizado (repo status).
func (e *Env) Status(ctx context.Context) error {
	st, err := ReadState(e.Root)
	if err != nil {
		return err
	}
	cur, err := Current(e.DataDir)
	if err != nil {
		return err
	}
	gen := "ninguna"
	if cur != nil {
		gen = fmt.Sprintf("%s (%s)", cur.Name, cur.Created.Format(time.DateTime))
	}
	e.Out.Event("status", map[string]any{"serial": st.Serial, "generation": gen},
		fmt.Sprintf("repo: serial %d; generación servida: %s", st.Serial, gen))
	for _, name := range st.PackageNames() {
		for _, p := range st.Packages[name] {
			e.Out.Event("published", map[string]any{"package": name, "version": p.Version, "suites": p.Suites},
				fmt.Sprintf("  %-10s %-22s %s", name, p.Version, strings.Join(p.Suites, " ")))
		}
	}
	for _, r := range st.Retired {
		e.Out.Event("retired", map[string]any{"package": r.Package, "version": r.Version, "reason": r.Reason},
			fmt.Sprintf("  %-10s %-22s retirada el %s: %s", r.Package, r.Version, r.Date.Format(time.DateOnly), r.Reason))
	}
	var dests []string
	for d := range e.Cfg.Repo.Destinations {
		dests = append(dests, d)
	}
	sort.Strings(dests)
	for _, d := range dests {
		msg := "nunca sincronizado"
		var rec struct {
			Serial     int    `json:"serial"`
			Generation string `json:"generation"`
		}
		if data, err := os.ReadFile(filepath.Join(e.DataDir, "sync", d+".json")); err == nil && json.Unmarshal(data, &rec) == nil {
			msg = fmt.Sprintf("serial %d (%s)", rec.Serial, rec.Generation)
			if cur != nil && rec.Generation != cur.Name {
				msg += "; pendiente de sincronizar"
			}
		}
		e.Out.Event("destination", map[string]any{"dest": d, "state": msg}, fmt.Sprintf("destino %s: %s", d, msg))
	}
	if sub := e.Cfg.Repo.Signing.Subkey; sub != "" {
		if exp, err := sign.Expiry(ctx, sign.Home(e.DataDir), sub); err == nil && !exp.IsZero() {
			days := int(time.Until(exp).Hours() / 24)
			e.Out.Event("subkey", map[string]any{"expires": exp.Format(time.DateOnly), "days": days},
				fmt.Sprintf("subllave %s…: caduca el %s (en %d días)", sub[:16], exp.Format(time.DateOnly), days))
		} else {
			e.Out.Event("subkey", map[string]any{"error": "no disponible"}, "subllave: no se pudo leer el llavero de firma")
		}
	}
	return nil
}
