package ci

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"debfactory/internal/build"
	"debfactory/internal/fetch"
	"debfactory/internal/forge"
	"debfactory/internal/installtest"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	reposync "debfactory/internal/sync"
)

// Notifier recibe los fallos y las resoluciones de una ejecución pub
// (contracts/notificaciones.md). La implementación de GitHub está en
// notify.go; logNotifier solo escribe en el log.
type Notifier interface {
	Fail(ctx context.Context, e *report.Error)
	Published(ctx context.Context, pkg, version string, serial int)
	Resolve(ctx context.Context, pkg, version, note string)
	StagePassed(ctx context.Context, stage report.Stage)
}

type logNotifier struct{ out *report.Printer }

func (n logNotifier) Fail(_ context.Context, e *report.Error) {
	n.out.Line("notificación: %s %s en %s: %s", e.Package, e.Version, e.Stage, e.Cause)
}
func (logNotifier) Published(context.Context, string, string, int)  {}
func (logNotifier) Resolve(context.Context, string, string, string) {}
func (logNotifier) StagePassed(context.Context, report.Stage)       {}

// TestHookAfterSwap lo usan solo las pruebas de recuperación (SC-007): se
// llama justo después de publicar y escribir la copia, y si devuelve error la
// ejecución termina ahí, como si se hubiera matado.
var TestHookAfterSwap func() error

// TestHookAfterSync es lo mismo tras sync y verify, antes del push.
var TestHookAfterSync func() error

func (r *Runner) dataDir() string { return r.Repo.DataDir }

func (r *Runner) forge() (*forge.Client, error) {
	return forge.New(r.Env.APIURL, r.Cfg.CI.Repository, r.Env.Token)
}

func (r *Runner) statePR(gh *forge.Client) *StatePR {
	return &StatePR{Root: r.Root, Cfg: r.Cfg, Dir: filepath.Join(r.dataDir(), "ci"), GH: gh, Out: r.Out, Now: r.Repo.Now, RunURL: r.Env.RunURL()}
}

// setStage escribe la etapa en curso en $RUNNER_TEMP/debfactory-stage para
// que el paso if: failure() la notifique si la ejecución muere (A2).
func (r *Runner) setStage(stage report.Stage) {
	if r.Env.RunnerTemp == "" {
		return
	}
	os.WriteFile(filepath.Join(r.Env.RunnerTemp, "debfactory-stage"), []byte(string(stage)+"\n"), 0o644)
}

// ciLock serializa las ejecuciones que escriben (FR-018), además de la cola
// de GitHub y del único runner pub. Es distinto de <data>/lock, que toman
// publish, retire y sync por su cuenta.
func (r *Runner) ciLock() (func(), error) {
	dir := filepath.Join(r.dataDir(), "ci")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, report.Newf(report.LockRepo, "%v", err)
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// summary añade Markdown a $GITHUB_STEP_SUMMARY.
func (r *Runner) summary(md string) {
	if r.Env.StepSummary == "" {
		return
	}
	f, err := os.OpenFile(r.Env.StepSummary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(md + "\n")
}

// run es el estado de una ejecución pub.
type run struct {
	*Runner
	gh     *forge.Client
	sp     *StatePR
	notify Notifier
	errs   report.Errors
}

func (x *run) fail(ctx context.Context, e *report.Error) {
	if e.Package == "" {
		e.Package = "pipeline"
	}
	if e.Version == "" {
		e.Version = "-"
	}
	x.Out.Fail(e)
	x.errs = append(x.errs, e)
	x.notify.Fail(ctx, e)
}

func (x *run) failErr(ctx context.Context, err error, stage report.Stage, pkg, version string) {
	var errs report.Errors
	if errors.As(err, &errs) {
		for _, e := range errs {
			x.fail(ctx, report.Wrap(e, stage, pkg, version, ""))
		}
		return
	}
	x.fail(ctx, report.Wrap(err, stage, pkg, version, ""))
}

func (r *Runner) newRun(ctx context.Context) (*run, error) {
	gh, err := r.forge()
	if err != nil {
		return nil, &report.EnvError{Cause: err.Error()}
	}
	x := &run{Runner: r, gh: gh, sp: r.statePR(gh)}
	x.notify = r.Notifier
	if x.notify == nil {
		x.notify = x.githubNotifier()
	}
	return x, nil
}

// prepare concilia el estado (paso 2): devuelve la referencia ya escrita en
// el checkout, la primera importación de la auditoría si falta y la
// sincronización pendiente de una ejecución interrumpida.
func (x *run) prepare(ctx context.Context) (*Ref, *repo.State, error) {
	x.setStage(report.State)
	ref, err := x.sp.Reconcile(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := x.writeRef(ref); err != nil {
		return nil, nil, err
	}
	st, err := repo.ReadState(x.Root)
	if err != nil {
		return nil, nil, err
	}
	x.Out.Event("state", map[string]any{"source": ref.Source, "serial": st.Serial, "sha256": ref.SHA256()},
		fmt.Sprintf("estado: referencia %s (serial %d, sha256 %s…)%s", ref.Source, st.Serial, ref.SHA256()[:4], prText(ref)))
	x.notify.StagePassed(ctx, report.State)
	// Auditoría: primera importación (contracts/audit.md) e invariantes (FR-024, FR-025).
	x.setStage(report.Audit)
	if len(ref.Audit) == 0 && st.Serial >= 1 {
		commit, err := gitOut(ctx, x.Root, "log", "-1", "--format=%H", x.sp.remote()+"/main", "--", StateFile)
		if err != nil || commit == "" {
			return nil, nil, report.Newf(report.Audit, "no se encuentra el commit de %s para importar: %v", StateFile, err)
		}
		added, err := AppendAudit(x.Root, ImportEntries(st, commit)...)
		if err != nil {
			return nil, nil, err
		}
		x.Out.Event("audit", map[string]any{"import": len(added)}, fmt.Sprintf("auditoría: %d entradas import (versiones publicadas antes del pipeline)", len(added)))
		if err := x.sp.SaveCopy(); err != nil {
			return nil, nil, err
		}
		if _, err := x.sp.Push(ctx); err != nil {
			return nil, nil, report.Newf(report.State, "%v", err)
		}
		ref.Audit, _ = os.ReadFile(filepath.Join(x.Root, AuditFile))
	}
	mainAudit, _ := x.sp.show(ctx, x.sp.remote()+"/main", AuditFile)
	if err := CheckAudit(ref.Audit, mainAudit, st); err != nil {
		return nil, nil, err
	}
	x.notify.StagePassed(ctx, report.Audit)
	// Sincronización pendiente (FR-029): el destino no tiene la generación
	// current. Se completa sin reconstruir.
	if err := x.syncIfPending(ctx); err != nil {
		return nil, nil, err
	}
	return ref, st, nil
}

func prText(ref *Ref) string {
	if ref.PR != nil {
		return fmt.Sprintf("; PR de estado #%d abierto", ref.PR.Number)
	}
	return "; sin PR de estado"
}

// writeRef escribe el estado y la auditoría de referencia en el checkout:
// publish y retire parten de ahí (FR-007a).
func (x *run) writeRef(ref *Ref) error {
	if err := writeFileAtomic(filepath.Join(x.Root, StateFile), ref.State); err != nil {
		return err
	}
	path := filepath.Join(x.Root, AuditFile)
	if len(ref.Audit) == 0 {
		os.Remove(path)
		return nil
	}
	return writeFileAtomic(path, ref.Audit)
}

func (x *run) syncIfPending(ctx context.Context) error {
	cur, err := repo.Current(x.dataDir())
	if err != nil || cur == nil {
		return err
	}
	dest := x.Cfg.CI.Destination
	rec, err := reposync.ReadRecord(x.dataDir(), dest)
	if err != nil {
		return err
	}
	if rec != nil && rec.Generation == cur.Name {
		return nil
	}
	x.Out.Line("sync: %s no tiene la generación %s; se completa la sincronización pendiente", dest, cur.Name)
	return x.syncVerify(ctx)
}

func (x *run) syncVerify(ctx context.Context) error {
	x.setStage(report.Sync)
	dest := x.Cfg.CI.Destination
	if err := reposync.Sync(ctx, x.Repo, dest, false); err != nil {
		return report.Wrap(err, report.Sync, "", "", "")
	}
	x.setStage(report.VerifyRepo)
	if err := reposync.VerifyDest(ctx, x.Repo, dest); err != nil {
		return err
	}
	x.notify.StagePassed(ctx, report.Sync)
	x.checkExpiry(ctx)
	return nil
}

// gates aplica los gates 1–6 a un paquete en root: build (validate, lock,
// fetch, verify, license, assemble, package, lintian) y la prueba de
// instalación en cada suite y arquitectura. No reimplementa ninguna etapa.
func (r *Runner) gates(ctx context.Context, root, name string, lim installtest.Limiter, out *report.Printer) (map[string]string, error) {
	if r.Gates != nil {
		return r.Gates(ctx, root, name)
	}
	b := r.Build()
	b.Root, b.Out = root, out
	if err := b.Build(ctx, name, build.Options{}); err != nil {
		return nil, err
	}
	m, err := manifest.Validate(root, r.Cfg, name)
	if err != nil {
		return nil, err
	}
	var errs report.Errors
	shas := map[string]string{}
	for _, arch := range r.Cfg.Arches {
		if _, err := installtest.TestPackage(ctx, root, r.Cfg, m, arch, installtest.Options{Limiter: lim}, out); err != nil {
			var e *report.Error
			if errors.As(err, &e) {
				errs = append(errs, e)
			} else {
				errs = append(errs, report.Wrap(err, report.Test, name, m.DebVersion(), arch))
			}
			continue
		}
		sha, err := fetch.SHA256File(filepath.Join(root, "dist", m.DebBase(arch)+".deb"))
		if err != nil {
			return nil, err
		}
		shas[arch] = sha
	}
	return shas, errs.Err()
}

// gateResult es el resultado de los gates de un paquete.
type gateResult struct {
	ap   AffectedPackage
	shas map[string]string
	err  error
}

// runGates ejecuta los gates de varios paquetes en paralelo; cada salida se
// imprime entera al terminar.
func (r *Runner) runGates(ctx context.Context, root string, aps []AffectedPackage, jobs int) []gateResult {
	res := make([]gateResult, len(aps))
	lim := installtest.NewLimiter(jobs)
	var wg sync.WaitGroup
	for i, ap := range aps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, flush := r.Out.Buffered()
			defer flush()
			shas, err := r.gates(ctx, root, ap.Package, lim, out)
			res[i] = gateResult{ap: ap, shas: shas, err: err}
			if err == nil {
				out.Line("%s %s: build ok · lint ok · test ok", ap.Package, ap.Version)
			}
		}()
	}
	wg.Wait()
	return res
}

// compare reconstruye versiones ya publicadas y compara sus .deb con el
// sha256 del estado (FR-010, FR-026). Usa el código de root.
func (r *Runner) compare(ctx context.Context, root string, st *repo.State, list []PkgVersion) report.Errors {
	var errs report.Errors
	for _, pv := range list {
		pub, ok := st.Find(pv.Package, pv.Version)
		if !ok {
			continue
		}
		var err error
		if r.Gates != nil {
			_, err = r.Gates(ctx, root, pv.Package)
		} else {
			b := r.Build()
			b.Root = root
			out, flush := r.Out.Buffered()
			b.Out = out
			err = b.Build(ctx, pv.Package, build.Options{})
			flush()
		}
		if err != nil {
			errs = append(errs, report.Wrap(err, report.Compare, pv.Package, pv.Version, ""))
			continue
		}
		for _, arch := range r.Cfg.Arches {
			got, err := fetch.SHA256File(filepath.Join(root, "dist", repo.DebFile(pv.Package, pv.Version, arch)))
			if err != nil {
				errs = append(errs, report.Wrap(err, report.Compare, pv.Package, pv.Version, arch))
				continue
			}
			if got != pub.Debs[arch] {
				errs = append(errs, &report.Error{Package: pv.Package, Version: pv.Version, Arch: arch, Stage: report.Compare,
					Cause: fmt.Sprintf("el .deb reconstruido (sha256 %s…) no es el publicado (%s…); una versión publicada no se reescribe: sube revision",
						sha12(got), sha12(pub.Debs[arch]))})
			}
		}
	}
	return errs
}

// validateAll valida todos los manifiestos cuando cambia la configuración o
// la herramienta fijada (FR-005). Las comprobaciones de código Go (gofmt,
// vet, test) ya no corren aquí: en packages no hay código, y las hace
// `just tool-install` antes de instalar una versión (004 research D5, D6).
func (r *Runner) validateAll(root string) error {
	var errs report.Errors
	names, err := build.ListPackages(root)
	if err != nil {
		return err
	}
	for _, n := range names {
		if _, err := manifest.Validate(root, r.Cfg, n); err != nil {
			errs = append(errs, report.Wrap(err, report.Validate, n, "", ""))
		}
	}
	return errs.Err()
}

// publish publica los paquetes que pasaron en una sola generación y, justo
// después del swap, añade la auditoría y guarda la copia del pipeline
// (contracts/cli.md, paso 5).
func (x *run) publish(ctx context.Context, passed []gateResult, commit string) (*repo.State, error) {
	x.setStage(report.Inputs)
	// Las entradas de cada paquete se comprueban por separado: uno roto no
	// arrastra a los demás (FR-004); publish valida todo junto.
	var ok []gateResult
	for _, g := range passed {
		if _, err := repo.Inputs(x.Root, x.Cfg, g.ap.Package, g.ap.Version); err != nil {
			x.failErr(ctx, err, report.Inputs, g.ap.Package, g.ap.Version)
			continue
		}
		ok = append(ok, g)
	}
	passed = ok
	if len(passed) == 0 {
		return nil, nil
	}
	x.setStage(report.Generate)
	var names []string
	versions := map[string]string{}
	for _, g := range passed {
		names = append(names, g.ap.Package)
		versions[g.ap.Package] = g.ap.Version
	}
	before, _ := repo.ReadState(x.Root)
	if err := x.Repo.Publish(ctx, names, versions, false); err != nil {
		return nil, err
	}
	st, err := repo.ReadState(x.Root)
	if err != nil {
		return nil, err
	}
	run := x.Env.RunURL()
	var entries []Entry
	for _, g := range passed {
		p, ok := st.Find(g.ap.Package, g.ap.Version)
		if !ok {
			continue
		}
		c := g.ap.Commit
		if c == "" || c == "HEAD" {
			c = commit
		}
		entries = append(entries, Entry{Time: p.Published, Op: OpPublish, Package: g.ap.Package, Version: g.ap.Version,
			Commit: c, Debs: p.Debs, Serial: st.Serial, Run: &run})
	}
	if _, err := AppendAudit(x.Root, entries...); err != nil {
		return nil, err
	}
	if err := x.sp.SaveCopy(); err != nil {
		return nil, err
	}
	x.Out.Event("audit", map[string]any{"added": len(entries)}, fmt.Sprintf("auditoría: +%d entradas", len(entries)))
	if before != nil {
		x.Out.Line("repo: serial %d → %d", before.Serial, st.Serial)
	}
	if TestHookAfterSwap != nil {
		if err := TestHookAfterSwap(); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// pushState empuja el PR de estado (paso 7).
func (x *run) pushState(ctx context.Context) {
	x.setStage(report.State)
	pr, err := x.sp.Push(ctx)
	if err != nil {
		x.failErr(ctx, err, report.State, "", "")
		return
	}
	if pr != nil {
		x.Out.Event("pr", map[string]any{"number": pr.Number}, fmt.Sprintf("estado: PR #%d abierto (%s)", pr.Number, x.Cfg.CI.StateBranch))
	}
}

// Main es `ci main` (contracts/cli.md), en este orden:
//  1. superada; 2. conciliar el estado y completar lo pendiente; 3. plan;
//     3b. comprobaciones de código si cambió; 4. gates 1–6 por paquete;
//  5. publish + auditoría + copia; 6. sync + verify; 7. PR de estado;
//  8. notificaciones.
func (r *Runner) Main(ctx context.Context, o Options) error {
	if err := r.Env.RequireWriter(r.Cfg); err != nil {
		return err
	}
	x, err := r.newRun(ctx)
	if err != nil {
		return err
	}
	r.Out.Line("ci: ejecución %s (%s %s) en %s", r.Env.RunID, r.Env.EventName, short(r.Env.SHA), r.Cfg.CI.Labels.Pub)
	release, err := r.ciLock()
	if err != nil {
		return err
	}
	defer release()
	// 1. Superada.
	x.setStage(report.Plan)
	head, err := x.sp.MainHead(ctx)
	if err != nil {
		return report.Newf(report.Plan, "%v", err)
	}
	if r.Env.SHA != "" && head != r.Env.SHA {
		r.Out.Event("superseded", map[string]any{"head": head}, fmt.Sprintf("ci: superada: main ya está en %s; la ejecución de ese commit concilia todo", short(head)))
		r.summary("Ejecución superada: `main` ya está en `" + short(head) + "`.")
		return nil
	}
	// 2. Estado.
	ref, st, err := x.prepare(ctx)
	if err != nil {
		x.failErr(ctx, err, report.State, "", "")
		return x.errs
	}
	x.checkStatePRAge(ctx, ref)
	x.checkExpiry(ctx)
	// 3. Plan.
	x.setStage(report.Plan)
	p, err := PlanMain(r.Root, r.Cfg, st, Reference{Source: ref.Source, SHA256: ref.SHA256()}, head)
	if err != nil {
		x.failErr(ctx, err, report.Plan, "", "")
		return x.errs
	}
	r.Out.Event("plan", map[string]any{"plan": p}, "plan: "+p.Summary())
	for _, e := range p.Err() {
		x.fail(ctx, e)
	}
	// 3b. Comprobaciones de código (FR-005).
	codeOK := true
	if last := x.sp.ReadPushed().LastMainSHA; last != "" && last != head {
		changed, err := CodeChanges(ctx, r.Root, r.Cfg, last+".."+head)
		if err != nil {
			r.Out.Line("plan: no se puede comparar con %s (%v); se comprueba el código", short(last), err)
			changed = []string{"?"}
		}
		if len(changed) > 0 {
			codeOK = x.checkCode(ctx, st)
		}
	}
	// 4. Gates.
	var passed []gateResult
	if codeOK && len(p.Publish) > 0 {
		if err := r.preflight(ctx); err != nil {
			return err
		}
		x.setStage(report.Test)
		for _, g := range r.runGates(ctx, r.Root, p.Publish, o.Jobs) {
			if g.err != nil {
				x.failErr(ctx, g.err, report.Test, g.ap.Package, g.ap.Version)
				continue
			}
			passed = append(passed, g)
		}
	}
	// 5–7. Publicar, sincronizar y empujar el estado.
	if len(passed) > 0 {
		pubSt, err := x.publish(ctx, passed, head)
		if err != nil {
			x.failErr(ctx, err, report.Generate, "", "")
			return x.errs
		}
		if pubSt == nil {
			r.writeSummary(p, nil, x.errs)
			return x.errs
		}
		if err := x.syncVerify(ctx); err != nil {
			x.failErr(ctx, err, report.Sync, "", "")
			return x.errs
		}
		if TestHookAfterSync != nil {
			if err := TestHookAfterSync(); err != nil {
				return err
			}
		}
		x.pushState(ctx)
		for _, g := range passed {
			x.notify.Published(ctx, g.ap.Package, g.ap.Version, pubSt.Serial)
		}
	} else if len(p.Publish) == 0 && len(p.Errors) == 0 && codeOK {
		r.Out.Event("unchanged", nil, "ci: nada que publicar")
	}
	if len(x.errs) == 0 {
		x.sp.SetLastMain(head)
		x.notify.StagePassed(ctx, report.Compare)
		x.notify.StagePassed(ctx, report.Plan)
	}
	r.writeSummary(p, passed, x.errs)
	return x.errs.Err()
}

// checkCode es el paso 3b: si cambió la configuración o la herramienta
// fijada (ci.code_paths), valida los manifiestos y compara las versiones
// publicadas con lo que produce ahora (FR-005, FR-010; 004 FR-008).
func (x *run) checkCode(ctx context.Context, st *repo.State) bool {
	x.setStage(report.Validate)
	ok := true
	if err := x.validateAll(x.Root); err != nil {
		x.failErr(ctx, err, report.Validate, "", "")
		ok = false
	}
	x.setStage(report.Compare)
	var list []PkgVersion
	names, _ := build.ListPackages(x.Root)
	for _, n := range names {
		if v, perr := manifestEntry(x.Root, x.Cfg, n); perr == nil {
			if _, pub := st.Find(n, v); pub {
				list = append(list, PkgVersion{n, v})
			}
		}
	}
	if len(list) > 0 {
		if err := x.preflight(ctx); err != nil {
			x.failErr(ctx, err, report.Compare, "", "")
			return false
		}
	}
	for _, e := range x.compare(ctx, x.Root, st, list) {
		x.fail(ctx, e)
		ok = false
	}
	return ok
}

// writeSummary escribe el resumen del job.
func (r *Runner) writeSummary(p *Plan, passed []gateResult, errs report.Errors) {
	var b strings.Builder
	fmt.Fprintf(&b, "### debfactory ci %s\n\nPlan: %s\n\n", p.Mode, p.Summary())
	if len(passed) > 0 {
		b.WriteString("| paquete | versión | resultado |\n|---|---|---|\n")
		for _, g := range passed {
			fmt.Fprintf(&b, "| %s | %s | ✅ publicado |\n", g.ap.Package, g.ap.Version)
		}
	}
	if len(errs) > 0 {
		b.WriteString("\n| paquete | versión | etapa | motivo |\n|---|---|---|---|\n")
		for _, e := range errs {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", e.Package, e.Version, e.Stage, mdEscape(e.Cause))
		}
	}
	r.summary(b.String())
}

func mdEscape(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}

// Dispatch ejecuta una orden ci.
func (r *Runner) Dispatch(ctx context.Context, cmd string, o Options) error {
	if cmd == "main" && r.Env.EventName == "workflow_dispatch" {
		in, err := r.Env.Inputs()
		if err != nil {
			return err
		}
		if in.Action != "main" {
			cmd = in.Action
			o.Package, o.Version, o.Reason, o.Last = in.Package, in.Version, in.Reason, in.Last
		}
	}
	switch cmd {
	case "main", "pr", "refresh", "rebuild", "retire", "restore-state-pr", "watch":
		// notify, plan, audit check y doctor no: notify tiene que poder avisar
		// justo de este fallo, y los demás no construyen ni publican.
		if err := CheckTool(r.Root, r.Env); err != nil {
			r.setStage(report.Tool)
			return err
		}
	}
	switch cmd {
	case "plan":
		return r.Plan(ctx, o)
	case "main":
		return r.Main(ctx, o)
	case "pr":
		return r.PR(ctx, o)
	case "refresh":
		return r.Refresh(ctx, o)
	case "rebuild":
		return r.Rebuild(ctx, o)
	case "retire":
		return r.Retire(ctx, o)
	case "restore-state-pr":
		return r.RestoreStatePR(ctx, o)
	case "notify":
		return r.NotifyCmd(ctx, o)
	case "audit check":
		return r.AuditCheck(ctx, o)
	case "doctor":
		return r.Doctor(ctx, o)
	case "watch":
		return r.Watch(ctx, o)
	}
	return &InputError{fmt.Sprintf("ci %s: orden desconocida", cmd)}
}

// Plan es `ci plan`: calcula y muestra el plan sin construir nada.
func (r *Runner) Plan(ctx context.Context, o Options) error {
	switch o.Mode {
	case "", "main":
		st, err := repo.ReadState(r.Root)
		if err != nil {
			return err
		}
		head, _ := gitOut(ctx, r.Root, "rev-parse", "HEAD")
		p, err := PlanMain(r.Root, r.Cfg, st, Reference{Source: "checkout"}, head)
		if err != nil {
			return err
		}
		r.Out.Event("plan", map[string]any{"plan": p}, "plan: "+p.Summary())
		return p.Err().Err()
	case "pr":
		if o.Base == "" {
			return &InputError{"ci plan --mode pr necesita --base REF"}
		}
		st, err := stateAt(ctx, r.Root, o.Base)
		if err != nil {
			return err
		}
		p, err := PlanPR(ctx, r.Root, r.Cfg, o.Base, st)
		if err != nil {
			return err
		}
		r.Out.Event("plan", map[string]any{"plan": p}, "plan: "+p.Summary())
		return p.Err().Err()
	case "rebuild":
		if err := ValidatePackageVersion("rebuild", o.Package, o.Version); err != nil {
			return err
		}
		st, err := repo.ReadState(r.Root)
		if err != nil {
			return err
		}
		p, err := r.planRebuild(ctx, st, o.Package, o.Version)
		if err != nil {
			return err
		}
		r.Out.Event("plan", map[string]any{"plan": p}, "plan: "+p.Summary())
		return p.Err().Err()
	}
	return &InputError{fmt.Sprintf("--mode %q: main, pr o rebuild", o.Mode)}
}

// stateAt lee repo/state.yaml de un ref de git.
func stateAt(ctx context.Context, root, ref string) (*repo.State, error) {
	data, err := (&StatePR{Root: root}).show(ctx, ref, StateFile)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return repo.Empty(), nil
	}
	return (&Ref{State: data}).ParseState()
}
