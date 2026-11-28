package main

import (
	"context"
	"fmt"

	"debfactory/internal/ci"
)

const ciUsage = `uso: debfactory ci <orden> [opciones]

Órdenes (specs/003-ci-pipeline/contracts/cli.md):
  plan [--mode main|pr|rebuild] [--base REF] [--package P --version V]
  pr --base REF [--jobs N]                     check de un pull request (nivel ci)
  main [--jobs N]                              ejecución de main (nivel pub)
  rebuild --package P --version V [--jobs N]   reconstruye una versión (nivel pub)
  retire --package P --version V --reason TEXTO [--last]
  refresh                                      vuelve a firmar (Valid-Until) y sincroniza
  restore-state-pr                             vuelve a crear el PR de estado
  notify --package P --version V (--stage S | --stage-file F) --reason TEXTO
  audit check [--against REF]                  invariantes de repo/audit.jsonl
  doctor [--fix]                               configuración de GitHub y de la PC
  watch [--package P] [--force] [--dry-run]    detector de versiones upstream (nivel ci; specs/005)

Opciones: --env-file F (variables GITHUB_* para pruebas locales) y las globales.
Sin --package ni la acción en la línea de órdenes, main lee INPUT_ACTION, etc.
`

// ci despacha `debfactory ci <orden>`.
func (a *app) ci(ctx context.Context, pos []string) error {
	if len(pos) == 0 || pos[0] == "help" {
		fmt.Fprint(a.p.Out, ciUsage)
		if len(pos) == 0 {
			return usageError{"ci necesita una orden"}
		}
		return nil
	}
	sub, args := pos[0], pos[1:]
	if sub == "audit" {
		if len(args) != 1 || args[0] != "check" {
			return usageError{"uso: debfactory ci audit check [--against REF]"}
		}
		sub, args = "audit check", nil
	}
	if len(args) != 0 {
		return usageError{fmt.Sprintf("ci %s no admite argumentos posicionales", sub)}
	}
	env, err := ci.LoadEnv(a.cfg, a.opts.envFile)
	if err != nil {
		return usageError{err.Error()}
	}
	r, err := a.ciRunner(env)
	if err != nil {
		return err
	}
	switch sub {
	case "plan", "matrix", "review", "compare", "pr", "main", "rebuild", "retire", "refresh", "restore-state-pr", "notify", "audit check", "doctor", "watch":
		return r.Dispatch(ctx, sub, a.ciOptions())
	}
	return usageError{fmt.Sprintf("orden de ci desconocida %q", sub)}
}

func (a *app) ciOptions() ci.Options {
	return ci.Options{
		Mode: a.opts.mode, Base: a.opts.base, Package: a.opts.pkg, Version: a.opts.version,
		Reason: a.opts.reason, Last: a.opts.last, Stage: a.opts.stage, StageFile: a.opts.stageFile,
		Against: a.opts.against, Fix: a.opts.fix, Jobs: a.opts.jobs,
		Force: a.opts.force, DryRun: a.opts.dryRun,
		Artifacts: a.opts.artifacts, Local: a.opts.local,
	}
}

// ciRunner reúne lo que necesitan las órdenes ci: configuración, contexto
// del job y los entornos de build y de repositorio de las fases 001 y 002.
func (a *app) ciRunner(env *ci.Env) (*ci.Runner, error) {
	re, err := a.repoEnv()
	if err != nil {
		return nil, err
	}
	return &ci.Runner{
		Root: a.opts.root, Cfg: a.cfg, Env: env, Out: a.p,
		Repo: re, Build: a.env, Cache: a.opts.cache, Workdir: a.opts.workdir,
	}, nil
}
