package repo

import (
	"context"
	"fmt"
	"path/filepath"

	"debfactory/internal/fetch"
	"debfactory/internal/report"
	"debfactory/internal/sign"
)

// Rebuild regenera la generación vigente desde repo/state.yaml (FR-036):
// cada .deb sale del almacén, de dist/ (si su sha coincide) o, si no está en
// ninguno, de Restore (reconstrucción desde git; T057). No sube el serial.
func (e *Env) Rebuild(ctx context.Context) error {
	if err := Preflight("aptly", "gpg", "gpgv"); err != nil {
		return err
	}
	release, err := Acquire(e.DataDir)
	if err != nil {
		return err
	}
	defer release()
	if err := sign.Check(ctx, e.Cfg, sign.Home(e.DataDir)); err != nil {
		return err
	}
	if err := e.checkExpiry(ctx); err != nil {
		return err
	}
	st, err := ReadState(e.Root)
	if err != nil {
		return err
	}
	if err := st.Validate(e.Cfg); err != nil {
		return err
	}
	store := StoreFor(e.DataDir)
	var errs report.Errors
	for _, name := range st.PackageNames() {
		for _, p := range st.Packages[name] {
			if p.Level2() {
				errs = append(errs, e.restoreLevel2(store, name, p)...)
				continue
			}
			for _, arch := range p.ArchList(e.Cfg.Arches) {
				sha := p.Debs[arch]
				if store.Has(sha) {
					continue
				}
				if err := e.restoreDeb(ctx, store, name, p.Version, arch, sha); err != nil {
					errs = append(errs, &report.Error{Package: name, Version: p.Version, Arch: arch,
						Stage: report.State, Cause: err.Error()})
				}
			}
		}
	}
	if err := errs.Err(); err != nil {
		return err
	}
	info, err := e.commit(ctx, st)
	if err != nil {
		return err
	}
	e.Out.Event("rebuild", map[string]any{"serial": st.Serial, "generation": info.Name},
		fmt.Sprintf("repo: serial %d regenerado (%s)", st.Serial, info.Name))
	return nil
}

// restoreDeb repone en el almacén un .deb que falta: de dist/ si su sha
// coincide o, si no, reconstruyéndolo desde git (Restore).
func (e *Env) restoreDeb(ctx context.Context, store Store, name, version, arch, sha string) error {
	dist := filepath.Join(e.Root, "dist", DebFile(name, version, arch))
	if got, err := fetch.SHA256File(dist); err == nil && got == sha {
		return store.Put(dist, sha)
	}
	if e.Restore != nil {
		return e.Restore(ctx, store, name, version, arch, sha)
	}
	return fmt.Errorf("falta %s (sha %s…); reconstrúyelo con debfactory build", DebFile(name, version, arch), sha[:12])
}

// restoreLevel2 repone en el almacén los archivos que faltan de una versión
// de nivel 2, desde dist/ si su sha coincide. Una compilación desde el
// código fuente no se rehace aquí: se recompila con debfactory build y la
// doble compilación garantiza el mismo resultado (FR-008).
func (e *Env) restoreLevel2(store Store, name string, p Published) report.Errors {
	var errs report.Errors
	for _, a := range p.Artifacts(name, e.Cfg.Arches) {
		if store.HasFor(a.Name, a.SHA256) {
			continue
		}
		dist := filepath.Join(e.Root, "dist", a.Name)
		if got, err := fetch.SHA256File(dist); err == nil && got == a.SHA256 {
			if err := store.PutFor(dist, a.Name, a.SHA256); err == nil {
				continue
			}
		}
		errs = append(errs, &report.Error{Package: name, Version: p.Version, Arch: a.Suite, Stage: report.State,
			Cause: fmt.Sprintf("falta %s (sha %s…); recompílalo con debfactory build %s", a.Name, a.SHA256[:12], name)})
	}
	return errs
}
