package ci

import (
	"context"

	"debfactory/internal/report"
)

// Refresh es `ci refresh` (FR-031): concilia el estado, vuelve a generar la
// generación con la misma serial (Valid-Until nuevo), sincroniza y verifica.
// No toca state.yaml ni la auditoría.
func (r *Runner) Refresh(ctx context.Context, o Options) error {
	if err := r.Env.RequireWriter(r.Cfg); err != nil {
		return err
	}
	x, err := r.newRun(ctx)
	if err != nil {
		return err
	}
	release, err := r.ciLock()
	if err != nil {
		return err
	}
	defer release()
	r.Out.Line("ci: refresh (ejecución %s)", r.Env.RunID)
	ref, _, err := x.prepare(ctx)
	if err != nil {
		x.failErr(ctx, err, report.State, "", "")
		return x.errs
	}
	x.checkStatePRAge(ctx, ref)
	x.setStage(report.Sign)
	if err := r.Repo.Rebuild(ctx); err != nil {
		x.failErr(ctx, err, report.Sign, "", "")
		return x.errs
	}
	if err := x.syncVerify(ctx); err != nil {
		x.failErr(ctx, err, report.Sync, "", "")
		return x.errs
	}
	r.summary("### debfactory ci refresh\n\nÍndices firmados de nuevo, sincronizados y verificados.")
	return x.errs.Err()
}
