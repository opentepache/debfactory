package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"debfactory/internal/build"
	"debfactory/internal/container"
	"debfactory/internal/repo"
	"debfactory/internal/repotest"
	"debfactory/internal/restore"
	reposync "debfactory/internal/sync"
)

func (a *app) repoEnv() (*repo.Env, error) {
	data, err := filepath.Abs(a.opts.data)
	if err != nil {
		return nil, usageError{err.Error()}
	}
	e := &repo.Env{Root: a.opts.root, DataDir: data, Cfg: a.cfg, Out: a.p}
	e.Restore = restore.Func(a.opts.root, func(root string) *build.Env {
		b := a.env()
		b.Root = root
		return b
	})
	return e, nil
}

// publish: `publish <pkg> [<version>]` o `publish --all`.
func (a *app) publish(ctx context.Context, pos []string) error {
	switch {
	case a.opts.all && len(pos) > 0:
		return usageError{"publish --all no admite paquetes"}
	case !a.opts.all && (len(pos) < 1 || len(pos) > 2):
		return usageError{"publish necesita un paquete (y opcionalmente su versión) o --all"}
	}
	e, err := a.repoEnv()
	if err != nil {
		return err
	}
	names, versions := pos[:min(1, len(pos))], map[string]string{}
	if len(pos) == 2 {
		versions[pos[0]] = pos[1]
	}
	if a.opts.all {
		if names, err = build.ListPackages(a.opts.root); err != nil {
			return err
		}
	}
	return e.Publish(ctx, names, versions, a.opts.dryRun)
}

// retire: `retire <pkg> <version> --reason TEXTO [--last]`.
func (a *app) retire(ctx context.Context, pos []string) error {
	if len(pos) != 2 {
		return usageError{"retire necesita un paquete y una versión"}
	}
	if strings.TrimSpace(a.opts.reason) == "" || strings.ContainsAny(a.opts.reason, "\n\r") {
		return usageError{"retire necesita --reason con una línea de texto"}
	}
	e, err := a.repoEnv()
	if err != nil {
		return err
	}
	return e.RetireVersion(ctx, pos[0], pos[1], a.opts.reason, a.opts.last, a.opts.dryRun)
}

// repo: verify, sync, rebuild, status y serve.
func (a *app) repo(ctx context.Context, pos []string) error {
	if len(pos) == 0 {
		return usageError{"repo necesita una suborden: verify, sync, rebuild, status o serve"}
	}
	e, err := a.repoEnv()
	if err != nil {
		return err
	}
	sub, args := pos[0], pos[1:]
	switch sub {
	case "verify":
		if len(args) != 0 {
			return usageError{"repo verify no admite argumentos (usa --dest)"}
		}
		if a.opts.dest != "" {
			return a.verifyDest(ctx, e, a.opts.dest)
		}
		return e.Verify(ctx)
	case "sync":
		if len(args) != 1 {
			return usageError{"repo sync necesita un destino"}
		}
		return reposync.Sync(ctx, e, args[0], a.opts.dryRun)
	case "status":
		return e.Status(ctx)
	case "serve":
		return a.serve(ctx, e)
	case "rebuild":
		if len(args) != 0 {
			return usageError{"repo rebuild no admite argumentos"}
		}
		return e.Rebuild(ctx)
	}
	return usageError{fmt.Sprintf("suborden de repo desconocida o no implementada: %q", sub)}
}

// verifyDest comprueba un destino ya sincronizado (US5).
func (a *app) verifyDest(ctx context.Context, e *repo.Env, dest string) error {
	return reposync.VerifyDest(ctx, e, dest)
}

// serve sirve el destino local con Caddy en 127.0.0.1:<port> hasta Ctrl-C.
func (a *app) serve(ctx context.Context, e *repo.Env) error {
	dest, ok := a.cfg.Repo.Destinations["local"]
	if !ok || !filepath.IsAbs(dest.Remote) {
		return usageError{"repo serve necesita repo.destinations.local con una ruta absoluta"}
	}
	if err := container.Preflight(ctx, nil); err != nil {
		return err
	}
	srv, err := repotest.StartServer(ctx, a.cfg, dest.Remote, a.opts.port)
	if err != nil {
		return err
	}
	defer srv.Stop()
	a.p.Line("sirviendo %s en http://localhost:%d/ (Ctrl-C para terminar)", dest.Remote, a.opts.port)
	<-ctx.Done()
	return nil
}
