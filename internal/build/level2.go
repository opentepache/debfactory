package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"

	"debfactory/internal/assemble"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/license"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
	"debfactory/internal/srcbuild"
	"debfactory/internal/srcpkg"
)

// Level2Suites son las suites en las que se compila un paquete de nivel 2:
// las de la configuración que el manifiesto no excluye, en su orden.
func Level2Suites(cfg *config.Config, m *manifest.Manifest) []config.Suite {
	var out []config.Suite
	for _, s := range cfg.Suites {
		if _, excluded := m.Exclude.Suites[s.Name]; !excluded {
			out = append(out, s)
		}
	}
	return out
}

// Level2Base es el nombre de los archivos de una suite y arquitectura:
// <name>_<versión con sufijo>_<arch>.
func Level2Base(m *manifest.Manifest, s config.Suite, arch string) string {
	return fmt.Sprintf("%s_%s_%s", m.Name, m.SuiteVersion(s.Level2Suffix), arch)
}

// buildLevel2 compila un paquete de nivel 2 (specs/008-source-builds,
// contracts/build.md): verifica el tarball, pasa el gate de licencias,
// genera el paquete fuente una vez por suite y lo compila dos veces por
// arquitectura en contenedores limpios; los dos .deb deben ser idénticos
// (FR-008). Nada llega a dist/ sin pasar todo, y si una combinación falla no
// se escribe ninguna (constitución VII).
func (e *Env) buildLevel2(ctx context.Context, m *manifest.Manifest, lk *lock.File, o Options) error {
	fail := func(stage report.Stage, arch string, err error) error {
		return report.Wrap(err, stage, m.Name, m.Version, arch)
	}
	entry, ok := lk.Versions[m.Version]
	if !ok {
		return fail(report.Lock, "", fmt.Errorf("no hay entrada para %s en %s; ejecuta `debfactory lock %s`",
			m.Version, lock.Path(m.Name), m.Name))
	}
	if err := entry.CompleteSource(); err != nil {
		return fail(report.Lock, "", fmt.Errorf("entrada incompleta para %s en %s (%v); ejecuta `debfactory lock %s`",
			m.Version, lock.Path(m.Name), err, m.Name))
	}
	suites := Level2Suites(e.Cfg, m)
	if o.Suite != "" {
		i := slices.IndexFunc(suites, func(s config.Suite) bool { return s.Name == o.Suite })
		if i < 0 {
			return fail(report.Validate, "", fmt.Errorf("suite %q no está en la configuración o el manifiesto la excluye", o.Suite))
		}
		suites = suites[i : i+1]
	}
	arches := e.Cfg.Arches
	if o.Arch != "" {
		arches = []string{o.Arch}
	}
	if e.SourceGen == nil {
		e.SourceGen = srcbuild.GenerateSource
	}
	if e.BinaryBuild == nil {
		e.BinaryBuild = srcbuild.Build
	}
	tarball, err := e.level2Inputs(ctx, m, entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.Workdir, 0o700); err != nil {
		return fail(report.Source, "", err)
	}
	work, err := os.MkdirTemp(e.Workdir, m.Name+"-l2-")
	if err != nil {
		return fail(report.Source, "", err)
	}
	defer os.RemoveAll(work)

	// Un paquete fuente por suite (research D6), con lintian sobre el .dsc.
	sources := make([]*srcbuild.Source, len(suites))
	for i, s := range suites {
		in := srcpkg.Input{Name: m.Name, Upstream: m.Version, Version: m.SuiteVersion(s.Level2Suffix), Suite: s.Name,
			Maintainer: e.Cfg.Maintainer, Epoch: entry.SourceDateEpoch, Orig: tarball, OrigName: m.OrigName(),
			DebianDir: filepath.Join(e.Root, m.DebianDir())}
		src, err := e.SourceGen(ctx, s, in, filepath.Join(work, s.Name))
		if err != nil {
			return report.Wrap(err, report.Source, m.Name, m.Version, s.Name)
		}
		if _, err := e.Lint(ctx, e.Cfg, filepath.Join(src.Dir, src.DSC)); err != nil {
			return report.Wrap(err, report.Lintian, m.Name, m.Version, s.Name)
		}
		sources[i] = src
	}

	// Compilaciones: amd64 primero (research D3), hasta level2.jobs a la vez.
	type job struct {
		suite config.Suite
		src   *srcbuild.Source
		arch  string
		res   *level2Result
		err   error
	}
	var jobs []*job
	for _, arch := range arches {
		for i, s := range suites {
			jobs = append(jobs, &job{suite: s, src: sources[i], arch: arch})
		}
	}
	tmpfs := e.Cfg.Level2.TmpfsOrDefault()
	if m.BuildOpts.Tmpfs != "" {
		tmpfs = m.BuildOpts.Tmpfs
	}
	sem := make(chan struct{}, e.Cfg.Level2.JobsOrDefault())
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				j.err = ctx.Err()
				return
			}
			spec := srcbuild.Spec{Suite: j.suite, Arch: j.arch, Source: j.src, Tree: m.Name + "-" + m.Version,
				Package: m.Name, Version: m.SuiteVersion(j.suite.Level2Suffix), Epoch: entry.SourceDateEpoch, Tmpfs: tmpfs}
			j.res, j.err = e.compileTwice(ctx, m, spec, filepath.Join(work, j.suite.Name+"-"+j.arch))
		}()
	}
	wg.Wait()
	var errs report.Errors
	for _, j := range jobs {
		if j.err != nil {
			var re *report.Error
			if errors.As(j.err, &re) {
				errs = append(errs, re)
			} else {
				errs = append(errs, report.Wrap(j.err, report.Build, m.Name, m.Version, j.suite.Name+"/"+j.arch))
			}
		}
	}
	if err := errs.Err(); err != nil {
		return err
	}

	// Todo pasó: a dist/ (el .orig una vez, el fuente por suite, el binario
	// por suite y arquitectura).
	if err := e.copyDist(tarball, m.OrigName()); err != nil {
		return fail(report.Package, "", err)
	}
	for _, src := range sources {
		for _, f := range []string{src.DSC, src.DebianTar} {
			if err := e.copyDist(filepath.Join(src.Dir, f), f); err != nil {
				return fail(report.Package, "", err)
			}
		}
	}
	manifestSHA, err := fetch.SHA256File(filepath.Join(e.Root, m.File))
	if err != nil {
		return fail(report.Package, "", err)
	}
	lockSHA, err := fetch.SHA256File(filepath.Join(e.Root, lock.Path(m.Name)))
	if err != nil {
		return fail(report.Package, "", err)
	}
	for _, j := range jobs {
		base := Level2Base(m, j.suite, j.arch)
		dsc, err := fetch.SHA256File(filepath.Join(j.src.Dir, j.src.DSC))
		if err != nil {
			return fail(report.Package, j.arch, err)
		}
		info := &pkg.BuildInfo{SHA256: j.res.sha, Lintian: "pass", Overrides: []string{}, TestCommand: m.Test.Command,
			ManifestSHA256: manifestSHA, LockSHA256: lockSHA, Suite: j.suite.Name, DSCSHA256: dsc,
			BuildinfoSHA256: j.res.buildinfoSHA, Reproducible: true}
		if err := e.copyDist(j.res.buildinfo, base+".buildinfo"); err != nil {
			return fail(report.Package, j.arch, err)
		}
		if err := e.publishDist(j.res.deb, info, base); err != nil {
			return fail(report.Package, j.arch, err)
		}
		e.Out.Result(m.Name, m.Version, j.arch, "deb", path.Join("dist", base+".deb"))
		e.Out.Result(m.Name, m.Version, j.arch, "reproducible", j.res.sha)
	}
	return nil
}

// level2Result es una combinación compilada dos veces con el mismo .deb.
type level2Result struct {
	deb, sha, buildinfo, buildinfoSHA string
}

// compileTwice compila spec con dos entornos (research D8), compara y pasa
// lintian al .deb.
func (e *Env) compileTwice(ctx context.Context, m *manifest.Manifest, spec srcbuild.Spec, dir string) (*level2Result, error) {
	where := spec.Suite.Name + "/" + spec.Arch
	var shas [2]string
	var first *srcbuild.Binary
	for v := range 2 {
		spec.Variant = v
		bin, err := e.BinaryBuild(ctx, spec, filepath.Join(dir, fmt.Sprint(v)))
		if err != nil {
			var f *srcbuild.Failure
			if errors.As(err, &f) {
				return nil, &report.Error{Package: m.Name, Version: m.Version, Arch: where, Stage: report.Build,
					Cause: f.Error(), Detail: f.Tail}
			}
			return nil, report.Wrap(err, report.Build, m.Name, m.Version, where)
		}
		if shas[v], err = fetch.SHA256File(bin.Deb); err != nil {
			return nil, report.Wrap(err, report.Build, m.Name, m.Version, where)
		}
		if v == 0 {
			first = bin
		}
	}
	if shas[0] != shas[1] {
		return nil, &report.Error{Package: m.Name, Version: m.Version, Arch: where, Stage: report.Compare,
			Cause:  "no reproducible: las dos compilaciones del mismo .dsc dan .deb distintos",
			Detail: []string{"primera: " + shas[0], "segunda: " + shas[1]}}
	}
	if _, err := e.Lint(ctx, e.Cfg, first.Deb); err != nil {
		return nil, report.Wrap(err, report.Lintian, m.Name, m.Version, where)
	}
	bi, err := fetch.SHA256File(first.Buildinfo)
	if err != nil {
		return nil, report.Wrap(err, report.Build, m.Name, m.Version, where)
	}
	return &level2Result{deb: first.Deb, sha: shas[0], buildinfo: first.Buildinfo, buildinfoSHA: bi}, nil
}

// level2Inputs descarga y verifica el tarball contra el lock, obtiene el
// texto de licencia verificado y pasa el gate de licencias, antes de
// compilar nada (FR-006). Devuelve la ruta del tarball verificado.
func (e *Env) level2Inputs(ctx context.Context, m *manifest.Manifest, entry *lock.Entry) (string, error) {
	fail := func(stage report.Stage, err error) error { return report.Wrap(err, stage, m.Name, m.Version, "") }
	url, err := m.RenderURL(m.Source.URL, "")
	if err != nil {
		return "", fail(report.Validate, err)
	}
	tarball, _, err := e.Fetcher.Get(ctx, url, entry.SourceSHA256)
	if err != nil {
		return "", downloadErr(err, m, "", "lock")
	}
	vars, err := m.Vars("")
	if err != nil {
		return "", fail(report.Validate, err)
	}
	var text []byte
	if m.License.File.Repo != "" {
		p, err := manifest.Render(m.License.File.Repo, vars)
		if err != nil {
			return "", fail(report.Validate, err)
		}
		tag, _ := m.RenderedTag()
		lp, _, err := e.Fetcher.Get(ctx, fetch.LicenseURL(m.Upstream, tag, p), entry.LicenseSHA256)
		if err != nil {
			return "", downloadErr(err, m, "", "lock")
		}
		if text, err = os.ReadFile(lp); err != nil {
			return "", fail(report.License, err)
		}
	} else {
		p, err := manifest.Render(m.License.File.Archive, vars)
		if err != nil {
			return "", fail(report.Validate, err)
		}
		if text, err = assemble.ExtractFile(tarball, m.Source.Format, p); err != nil {
			return "", fail(report.License, fmt.Errorf("license.file.archive: %v", err))
		}
		if got := fetch.SHA256(text); got != entry.LicenseSHA256 {
			return "", downloadErr(&fetch.HashMismatch{URL: p, Expected: entry.LicenseSHA256, Got: got}, m, "", "lock")
		}
	}
	if err := license.Gate(text, m.License.SPDX, e.Cfg); err != nil {
		return "", fail(report.License, err)
	}
	return tarball, nil
}

// copyDist copia un archivo a dist/<name> de forma atómica.
func (e *Env) copyDist(src, name string) error {
	dist := filepath.Join(e.Root, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dist, name), func(w io.Writer) error {
		fh, err := os.Open(src)
		if err != nil {
			return err
		}
		defer fh.Close()
		_, err = io.Copy(w, fh)
		return err
	})
}
