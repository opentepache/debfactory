package ci

import (
	"context"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/container"
	"debfactory/internal/repo"
	"debfactory/internal/report"
)

// Options son las opciones de las órdenes ci (contracts/cli.md).
type Options struct {
	Mode, Base, Package, Version, Reason string
	Stage, StageFile, Against            string
	Last, Fix                            bool
	Jobs                                 int
	// Force y DryRun son de ci watch (specs/005-upstream-release-watch).
	Force, DryRun bool
	// Artifacts: ci main acepta los artefactos de los jobs hospedados de
	// ese directorio en lugar de construir y probar en la PC (specs/009).
	Artifacts string
	// Local: ci main construye y prueba todo en la PC (FR-011b).
	Local bool
}

// Runner reúne lo que necesitan las órdenes ci.
type Runner struct {
	Root    string // raíz del repositorio git (el checkout)
	Cfg     *config.Config
	Env     *Env
	Out     *report.Printer
	Repo    *repo.Env
	Build   func() *build.Env
	Cache   string
	Workdir string

	// Notifier recibe fallos y resoluciones; nil = issues de GitHub en las
	// órdenes pub (notify.go).
	Notifier Notifier

	// Gates sustituye los gates 1–6 en las pruebas de internal/ci (que no
	// usan Docker): deja en root/dist los .deb y sus informes y devuelve el
	// sha256 por arquitectura. nil = build e installtest reales.
	Gates func(ctx context.Context, root, name string) (map[string]string, error)

	// accept, si no es nil, sustituye los gates por la verificación de los
	// artefactos de los jobs hospedados (ci main --artifacts).
	accept *Accept
	// AcceptHooks permite a las pruebas sustituir la atestación y la prueba
	// de humo de accept.
	AcceptHooks *Accept
}

// preflight comprueba Docker y binfmt, salvo con gates de prueba.
func (r *Runner) preflight(ctx context.Context) error {
	if r.Gates != nil {
		return nil
	}
	if r.accept != nil {
		// accept solo usa contenedores amd64 (lintian, nivel 1 y humo).
		return container.Preflight(ctx, []string{"amd64"})
	}
	return container.Preflight(ctx, r.Cfg.Arches)
}
