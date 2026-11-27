package repo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/report"
	"debfactory/internal/sign"
)

// Env reúne lo que necesitan publish, retire y repo.
type Env struct {
	Root    string // raíz del repositorio git
	DataDir string // <data>
	Cfg     *config.Config
	Out     *report.Printer
	Now     func() time.Time

	// AfterGenerate se llama con la generación ya creada y antes de
	// verificarla; solo lo usan los tests para inyectar fallos (T033).
	AfterGenerate func(genDir string) error

	// Restore reconstruye un .deb que no está ni en el almacén ni en dist/
	// (repo rebuild desde el historial de git; FR-036).
	Restore func(ctx context.Context, store Store, name, version, arch, sha string) error
}

// NowTime devuelve la hora de Env.Now, o la real si no está fijada.
func (e *Env) NowTime() time.Time { return e.now() }

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Preflight comprueba que estén aptly y gpg (código 3 si faltan).
func Preflight(tools ...string) error {
	for _, t := range tools {
		if _, err := exec.LookPath(t); err != nil {
			return &report.EnvError{Cause: fmt.Sprintf("falta %s en el PATH (sudo apt install %s)", t, t)}
		}
	}
	return nil
}

// Publish publica una versión de cada paquete de names en una sola
// generación (FR-006–FR-011). versions puede fijar la versión de un paquete;
// si no, se usa la del manifiesto.
func (e *Env) Publish(ctx context.Context, names []string, versions map[string]string, dryRun bool) error {
	if err := Preflight("aptly", "gpg", "gpgv"); err != nil {
		return err
	}
	return e.mutate(ctx, dryRun, func(st *State) ([]Op, error) {
		var ops []Op
		var errs report.Errors
		for _, name := range names {
			v := versions[name]
			if v == "" {
				var err error
				if v, err = ManifestVersion(e.Root, e.Cfg, name); err != nil {
					errs = append(errs, report.Wrap(err, report.Validate, name, "", ""))
					continue
				}
			}
			in, err := Inputs(e.Root, e.Cfg, name, v)
			if err != nil {
				errs = appendErr(errs, err)
				continue
			}
			ops = append(ops, Add{Package: name, Published: in.Published})
			if !dryRun {
				for _, a := range in.Published.Artifacts(name, e.Cfg.Arches) {
					if err := StoreFor(e.DataDir).PutFor(in.Source(a), a.Name, a.SHA256); err != nil {
						errs = append(errs, &report.Error{Package: name, Version: v, Arch: a.Arch,
							Stage: report.Inputs, Cause: err.Error()})
					}
				}
			}
		}
		return ops, errs.Err()
	})
}

// RetireVersion retira una versión de todas las suites (FR-014–FR-016).
func (e *Env) RetireVersion(ctx context.Context, name, version, reason string, last, dryRun bool) error {
	if err := Preflight("aptly", "gpg", "gpgv"); err != nil {
		return err
	}
	return e.mutate(ctx, dryRun, func(st *State) ([]Op, error) {
		return []Op{Retire{Package: name, Version: version, Reason: reason, Last: last}}, nil
	})
}

// mutate es el flujo común de las operaciones que cambian el estado:
// bloqueo → llave → estado y consistencia → operaciones → generar →
// verificar → swap (research D2, D3).
func (e *Env) mutate(ctx context.Context, dryRun bool, build func(*State) ([]Op, error)) error {
	release, err := Acquire(e.DataDir)
	if err != nil {
		return err
	}
	defer release()
	if !dryRun {
		if err := sign.Check(ctx, e.Cfg, sign.Home(e.DataDir)); err != nil {
			return err
		}
		if err := e.checkExpiry(ctx); err != nil {
			return err
		}
	}
	st, err := ReadState(e.Root)
	if err != nil {
		return err
	}
	if err := st.Validate(e.Cfg); err != nil {
		return err
	}
	if err := CheckConsistent(e.Root, e.DataDir, st); err != nil {
		return err
	}
	ops, err := build(st)
	if err != nil {
		return err
	}
	next, changes, err := st.Apply(e.Cfg, e.now(), ops...)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		e.Out.Event("unchanged", nil, "sin cambios")
		return nil
	}
	e.printChanges(changes)
	if dryRun {
		e.Out.Event("dry-run", map[string]any{"serial": next.Serial}, fmt.Sprintf("repo: serial %d → %d (simulación; no se aplicó)", st.Serial, next.Serial))
		return nil
	}
	if err := next.Validate(e.Cfg); err != nil {
		return err
	}
	if _, err := e.commit(ctx, next); err != nil {
		return err
	}
	e.Out.Event("serial", map[string]any{"from": st.Serial, "to": next.Serial, "subkey": e.Cfg.Repo.Signing.Subkey},
		fmt.Sprintf("repo: serial %d → %d; firmado con %s…", st.Serial, next.Serial, e.Cfg.Repo.Signing.Subkey[:16]))
	return nil
}

// commit genera, verifica y expone la generación de st.
func (e *Env) commit(ctx context.Context, st *State) (*GenInfo, error) {
	genDir, err := Generate(ctx, e.Cfg, st, StoreFor(e.DataDir), e.DataDir, e.now())
	if err != nil {
		return nil, err
	}
	if e.AfterGenerate != nil {
		if err := e.AfterGenerate(genDir); err != nil {
			removeGen(genDir)
			return nil, err
		}
	}
	if err := VerifyTree(ctx, e.Cfg, st, genDir, e.now()); err != nil {
		removeGen(genDir)
		return nil, err
	}
	info, err := Commit(e.Root, e.DataDir, st, genDir, e.Cfg.Repo.Signing.Subkey)
	if err != nil {
		removeGen(genDir)
		return nil, err
	}
	return info, nil
}

// checkExpiry falla si la subllave ya caducó y avisa si caduca en menos de
// repo.signing.warn_days días (FR-023). El aviso no hace fallar.
func (e *Env) checkExpiry(ctx context.Context) error {
	sub := e.Cfg.Repo.Signing.Subkey
	exp, err := sign.Expiry(ctx, sign.Home(e.DataDir), sub)
	if err != nil || exp.IsZero() {
		return nil // sin caducidad, o sign.Check ya informó del problema
	}
	now := e.now()
	date := exp.UTC().Format(time.DateOnly)
	if !exp.After(now) {
		return report.Newf(report.Sign, "la subllave %s caducó el %s", sub[len(sub)-16:], date)
	}
	days := int(exp.Sub(now).Hours() / 24)
	if days < e.Cfg.Repo.Signing.WarnDays {
		e.Out.Event("warning", map[string]any{"subkey": sub, "expires": date, "days": days},
			fmt.Sprintf("aviso: la subllave de firma caduca el %s (en %d días)", date, days))
	}
	return nil
}

func (e *Env) printChanges(changes []Change) {
	arches := strings.Join(e.Cfg.Arches, " ")
	for _, c := range changes {
		ev := map[string]any{"package": c.Package, "version": c.Version, "suites": c.Suites, "reason": c.Reason}
		suites := strings.Join(c.Suites, " ")
		switch c.Kind {
		case KindAdd:
			e.Out.Event("add", ev, fmt.Sprintf("%s %s: agregar en %s (%s)", c.Package, c.Version, suites, arches))
		case KindRemove:
			e.Out.Event("remove", ev, fmt.Sprintf("%s %s: quitar por %s de %s", c.Package, c.Version, c.Reason, suites))
		case KindKeep:
			e.Out.Event("warning", ev, fmt.Sprintf("aviso: %s %s: %s", c.Package, c.Version, c.Reason))
		}
	}
}

func appendErr(errs report.Errors, err error) report.Errors {
	if list, ok := err.(report.Errors); ok {
		return append(errs, list...)
	}
	return append(errs, report.Wrap(err, report.Inputs, "", "", ""))
}
