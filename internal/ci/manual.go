package ci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/restore"
)

// planRebuild es el plan de `ci rebuild P V` (FR-026): el commit que declaró
// la versión y si ya está publicada (se compara) o no (se aplica la tabla de
// conciliación: una versión anterior a la publicada no se publica).
func (r *Runner) planRebuild(ctx context.Context, st *repo.State, pkg, version string) (*Plan, error) {
	p := &Plan{Mode: "rebuild"}
	commit, err := restore.FindCommit(ctx, r.Root, pkg, version)
	if err != nil {
		p.Errors = append(p.Errors, PlanError{Package: pkg, Version: version, Stage: report.State, Reason: err.Error()})
		return p, nil
	}
	if _, ok := st.Find(pkg, version); ok {
		p.Compare = append(p.Compare, PkgVersion{pkg, version})
		p.Publish = nil
		p.Test = append(p.Test, AffectedPackage{Package: pkg, Version: version, Commit: commit, Reason: Rebuild})
		return p, nil
	}
	_, _, perr := Classify(st, pkg, version)
	if perr != nil {
		p.Errors = append(p.Errors, *perr)
		return p, nil
	}
	p.Publish = append(p.Publish, AffectedPackage{Package: pkg, Version: version, Commit: commit, Reason: Rebuild})
	return p, nil
}

// worktree crea un worktree temporal en commit y devuelve su ruta y cómo
// quitarlo.
func (r *Runner) worktree(ctx context.Context, commit string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "debfactory-rebuild-*")
	if err != nil {
		return "", nil, err
	}
	wt := filepath.Join(tmp, "wt")
	if _, err := gitOut(ctx, r.Root, "worktree", "add", "-q", "--detach", wt, commit); err != nil {
		os.RemoveAll(tmp)
		return "", nil, err
	}
	return wt, func() {
		gitOut(context.Background(), r.Root, "worktree", "remove", "--force", wt)
		os.RemoveAll(tmp)
	}, nil
}

// Rebuild es `ci rebuild --package P --version V` (FR-026): construye y
// prueba con el manifiesto y el lockfile (y el código) del commit que declaró
// la versión. Si ya está publicada, compara los sha256 y no publica; si no,
// la publica como una ejecución de main.
func (r *Runner) Rebuild(ctx context.Context, o Options) error {
	if err := r.Env.RequireWriter(r.Cfg); err != nil {
		return err
	}
	if err := ValidatePackageVersion("rebuild", o.Package, o.Version); err != nil {
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
	r.Out.Line("ci: rebuild %s %s (ejecución %s)", o.Package, o.Version, r.Env.RunID)
	_, st, err := x.prepare(ctx)
	if err != nil {
		x.failErr(ctx, err, report.State, "", "")
		return x.errs
	}
	x.setStage(report.Plan)
	p, err := r.planRebuild(ctx, st, o.Package, o.Version)
	if err != nil {
		return err
	}
	r.Out.Event("plan", map[string]any{"plan": p}, "plan: "+p.Summary())
	for _, e := range p.Err() {
		x.fail(ctx, e)
	}
	if len(p.Errors) > 0 {
		return x.errs
	}
	ap := append(p.Test, p.Publish...)[0]
	if err := r.preflight(ctx); err != nil {
		return err
	}
	wt, cleanup, err := r.worktree(ctx, ap.Commit)
	if err != nil {
		x.failErr(ctx, err, report.Plan, ap.Package, ap.Version)
		return x.errs
	}
	defer cleanup()
	x.setStage(report.Test)
	g := r.runGates(ctx, wt, []AffectedPackage{ap}, o.Jobs)[0]
	if g.err != nil {
		x.failErr(ctx, g.err, report.Test, ap.Package, ap.Version)
		return x.errs
	}
	if len(p.Compare) > 0 {
		pub, _ := st.Find(ap.Package, ap.Version)
		for _, e := range compareShas(ap.Package, ap.Version, g.shas, pub, r.Cfg.Arches, " (reconstruido en "+short(ap.Commit)+")") {
			x.fail(ctx, e)
		}
		if len(x.errs) > 0 {
			return x.errs.Err()
		}
		// Mismos .deb: si a la versión actual le faltan suites configuradas,
		// se amplía (specs/007-configurable-suites, research D3).
		missing := MissingSuites(wt, r.Cfg, st, ap.Package, ap.Version)
		if len(missing) == 0 {
			if pubs := st.Packages[ap.Package]; len(pubs) > 0 && pubs[0].Version != ap.Version {
				r.Out.Line("rebuild: %s %s no es la versión más alta; no se amplía", ap.Package, ap.Version)
			}
			r.Out.Event("unchanged", nil, "rebuild: sha256 iguales; nada que publicar")
			x.notify.Published(ctx, ap.Package, ap.Version, st.Serial)
			r.summary(fmt.Sprintf("### debfactory ci rebuild\n\n%s %s: sha256 iguales a lo publicado; nada que publicar.", ap.Package, ap.Version))
			return x.errs.Err()
		}
		ap.Reason, ap.Suites = Extend, missing
		g.ap = ap
		r.Out.Line("rebuild: %s %s: sha256 iguales; se amplía a %s", ap.Package, ap.Version, strings.Join(missing, " "))
	}
	// No publicada: se copian sus .deb e informes a dist/ del checkout y se
	// publica como en main.
	for _, arch := range r.Cfg.Arches {
		base := fmt.Sprintf("%s_%s_%s", ap.Package, ap.Version, arch)
		for _, ext := range []string{".deb", ".build.json", ".test.json"} {
			data, err := os.ReadFile(filepath.Join(wt, "dist", base+ext))
			if err != nil {
				x.failErr(ctx, err, report.Inputs, ap.Package, ap.Version)
				return x.errs
			}
			os.MkdirAll(filepath.Join(r.Root, "dist"), 0o755)
			if err := os.WriteFile(filepath.Join(r.Root, "dist", base+ext), data, 0o644); err != nil {
				return err
			}
		}
	}
	pubSt, err := x.publish(ctx, []gateResult{g}, ap.Commit)
	if err != nil || pubSt == nil {
		if err != nil {
			x.failErr(ctx, err, report.Generate, ap.Package, ap.Version)
		}
		return x.errs
	}
	if err := x.syncVerify(ctx); err != nil {
		x.failErr(ctx, err, report.Sync, "", "")
		return x.errs
	}
	x.pushState(ctx)
	x.notify.Published(ctx, ap.Package, ap.Version, pubSt.Serial)
	x.site(ctx)
	return x.errs.Err()
}

// Retire es `ci retire --package P --version V --reason TEXTO [--last]`
// (FR-027): retira con la orden de la fase 002, sincroniza, verifica, añade
// la entrada retire a la auditoría y actualiza el PR de estado.
func (r *Runner) Retire(ctx context.Context, o Options) error {
	if err := r.Env.RequireWriter(r.Cfg); err != nil {
		return err
	}
	if err := ValidatePackageVersion("retire", o.Package, o.Version); err != nil {
		return err
	}
	if err := ValidateReason(o.Reason); err != nil {
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
	r.Out.Line("ci: retire %s %s (ejecución %s)", o.Package, o.Version, r.Env.RunID)
	if _, _, err := x.prepare(ctx); err != nil {
		x.failErr(ctx, err, report.State, "", "")
		return x.errs
	}
	x.setStage(report.State)
	if err := r.Repo.RetireVersion(ctx, o.Package, o.Version, o.Reason, o.Last, false); err != nil {
		x.failErr(ctx, err, report.State, o.Package, o.Version)
		return x.errs
	}
	st, err := repo.ReadState(r.Root)
	if err != nil {
		return err
	}
	runURL := r.Env.RunURL()
	if _, err := AppendAudit(r.Root, Entry{Time: x.now().UTC(), Op: OpRetire, Package: o.Package, Version: o.Version,
		Commit: r.Env.SHA, Serial: st.Serial, Run: &runURL, Reason: o.Reason}); err != nil {
		x.failErr(ctx, err, report.Audit, o.Package, o.Version)
		return x.errs
	}
	if err := x.sp.SaveCopy(); err != nil {
		return err
	}
	if err := x.syncVerify(ctx); err != nil {
		x.failErr(ctx, err, report.Sync, "", "")
		return x.errs
	}
	x.pushState(ctx)
	x.site(ctx)
	x.notify.Resolve(ctx, o.Package, o.Version, fmt.Sprintf("retirada en serial %d", st.Serial))
	r.summary(fmt.Sprintf("### debfactory ci retire\n\n%s %s retirada (serial %d): %s", o.Package, o.Version, st.Serial, mdEscape(o.Reason)))
	return x.errs.Err()
}

// RestoreStatePR es `ci restore-state-pr` (D8).
func (r *Runner) RestoreStatePR(ctx context.Context, o Options) error {
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
	pr, err := x.sp.RestoreStatePR(ctx, r.dataDir())
	if err != nil {
		x.failErr(ctx, err, report.State, "", "")
		return x.errs
	}
	if pr == nil {
		r.Out.Line("estado: main ya tiene lo publicado; no hace falta PR de estado")
	} else {
		r.Out.Event("pr", map[string]any{"number": pr.Number}, fmt.Sprintf("estado: PR #%d vuelto a crear", pr.Number))
	}
	x.notify.StagePassed(ctx, report.State)
	return nil
}

// AuditCheck es `ci audit check [--against REF]` (FR-024, FR-025).
func (r *Runner) AuditCheck(ctx context.Context, o Options) error {
	data, err := os.ReadFile(filepath.Join(r.Root, AuditFile))
	if err != nil {
		return report.Newf(report.Audit, "%s: %v", AuditFile, err)
	}
	var against []byte
	if o.Against != "" {
		if against, err = (&StatePR{Root: r.Root}).show(ctx, o.Against, AuditFile); err != nil {
			return err
		}
	}
	st, err := repo.ReadState(r.Root)
	if err != nil {
		return err
	}
	if err := CheckAudit(data, against, st); err != nil {
		return err
	}
	entries, _ := ParseAudit(data)
	r.Out.Event("audit", map[string]any{"entries": len(entries)},
		fmt.Sprintf("auditoría: %d entradas; solo añadir y cobertura de %s en verde", len(entries), StateFile))
	return nil
}
