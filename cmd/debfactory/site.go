package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"debfactory/internal/repo"
	"debfactory/internal/site"
	reposync "debfactory/internal/sync"
)

// siteNoConfig atiende las órdenes de site que no leen la configuración:
// build (desde un site.json) y serve (un árbol ya generado).
func (a *app) siteNoConfig(ctx context.Context, sub string, args []string) error {
	if len(args) != 0 {
		return usageError{"site " + sub + " no admite argumentos"}
	}
	switch sub {
	case "build":
		if a.opts.in == "" || a.opts.out == "" {
			return usageError{"site build necesita --in FILE y --out DIR"}
		}
		data, err := os.ReadFile(a.opts.in)
		if err != nil {
			return usageError{err.Error()}
		}
		var m site.Model
		if err := json.Unmarshal(data, &m); err != nil {
			return usageError{fmt.Sprintf("%s: %v", a.opts.in, err)}
		}
		if err := site.Build(&m, a.opts.out); err != nil {
			return err
		}
		fmt.Fprintln(a.p.Out, a.opts.out)
		return nil
	case "serve":
		if a.opts.dir == "" {
			return usageError{"site serve necesita --dir DIR"}
		}
		host, _, err := net.SplitHostPort(a.opts.addr)
		if ip := net.ParseIP(host); err != nil || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return usageError{fmt.Sprintf("site serve solo escucha en loopback (--addr %q)", a.opts.addr)}
		}
		srv := &http.Server{Addr: a.opts.addr, Handler: http.FileServer(http.Dir(a.opts.dir))}
		go func() { <-ctx.Done(); srv.Close() }()
		fmt.Fprintf(a.p.Out, "sirviendo %s en http://%s/\n", a.opts.dir, a.opts.addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	return usageError{"orden de site desconocida: " + sub}
}

// site atiende data y sync, que leen la configuración, el estado y <data>.
func (a *app) site(ctx context.Context, pos []string) error {
	if len(pos) == 0 {
		return usageError{"site necesita una suborden: data, build, serve o sync"}
	}
	sub := pos[0]
	if len(pos) != 1 {
		return usageError{"site " + sub + " no admite argumentos"}
	}
	e, err := a.repoEnv()
	if err != nil {
		return err
	}
	statePath := a.opts.state
	if statePath == "" {
		statePath = filepath.Join(a.opts.root, repo.StateFile)
	}
	st, err := repo.ReadStateFile(statePath)
	if err != nil {
		return err
	}
	warn := func(s string) { fmt.Fprintln(a.p.Err, s) }
	switch sub {
	case "data":
		m, err := site.FromData(a.cfg, a.opts.root, st, e.DataDir, warn)
		if err != nil {
			return err
		}
		js, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		js = append(js, '\n')
		if a.opts.out == "" {
			_, err = a.p.Out.Write(js)
			return err
		}
		return os.WriteFile(a.opts.out, js, 0o644)
	case "sync":
		if a.opts.dest == "" {
			return usageError{"site sync necesita --dest NAME"}
		}
		if _, ok := a.cfg.Repo.Destinations[a.opts.dest]; !ok {
			return usageError{fmt.Sprintf("destino desconocido %q (repo.destinations)", a.opts.dest)}
		}
		m, err := site.FromData(a.cfg, a.opts.root, st, e.DataDir, warn)
		if err != nil {
			return err
		}
		dir, err := site.BuildDigest(m, filepath.Join(e.DataDir, "site"))
		if err != nil {
			return err
		}
		res, err := reposync.SyncSite(ctx, e, a.opts.dest, dir, m.Serial, a.opts.dryRun)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.p.Out, siteSummary(m.Serial, a.opts.dest, res, a.opts.dryRun))
		return nil
	case "build", "serve":
		return usageError{"site " + sub + " no lee la configuración"} // lo atiende siteNoConfig
	}
	return usageError{"orden de site desconocida: " + sub}
}

func siteSummary(serial int, dest string, res *reposync.SiteResult, dryRun bool) string {
	switch {
	case res.Unchanged:
		return fmt.Sprintf("site: serial %d sin cambios en %s; nada que subir", serial, dest)
	case dryRun:
		return fmt.Sprintf("site: serial %d; %s: subiría %d, borraría %d (--dry-run)", serial, dest, res.Uploaded, res.Deleted)
	}
	return fmt.Sprintf("site: serial %d; %s: subidos %d, borrados %d", serial, dest, res.Uploaded, res.Deleted)
}
