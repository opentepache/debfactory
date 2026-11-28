package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"time"

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
	"debfactory/internal/supply"
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
	e = e.forManifest(m)
	fail := func(stage report.Stage, arch string, err error) error {
		return report.Wrap(err, stage, m.Name, m.Version, arch)
	}
	entry, ok := lk.Versions[m.Version]
	if !ok {
		return fail(report.Lock, "", fmt.Errorf("no hay entrada para %s en %s; ejecuta `debfactory lock %s`",
			m.Version, lock.PathIn(e.Root, m.Name), m.Name))
	}
	if err := entry.CompleteSource(); err != nil {
		return fail(report.Lock, "", fmt.Errorf("entrada incompleta para %s en %s (%v); ejecuta `debfactory lock %s`",
			m.Version, lock.PathIn(e.Root, m.Name), err, m.Name))
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
	tarball, supplyRep, err := e.level2Inputs(ctx, m, entry)
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
	extra, err := e.level2Extras(ctx, m, entry, tarball, arches, work)
	if err != nil {
		return err
	}
	supplyRep.TarballVsTag, supplyRep.Toolchain, supplyRep.Deps = extra.tag, extra.toolchain, extra.deps

	// Un paquete fuente por suite (research D6). lintian sobre el .dsc corre
	// a la vez que las compilaciones (en un fuente grande tarda minutos); si
	// falla, las cancela, y nada llega a dist/ sin haberlo pasado.
	bctx, cancelBuilds := context.WithCancel(ctx)
	defer cancelBuilds()
	sources := make([]*srcbuild.Source, len(suites))
	lintErrs := make([]error, len(suites))
	var lintWG sync.WaitGroup
	for i, s := range suites {
		in := srcpkg.Input{Name: m.Name, Upstream: m.Version, Version: m.SuiteVersion(s.Level2Suffix), Suite: s.Name,
			Maintainer: e.Cfg.Maintainer, Epoch: entry.SourceDateEpoch, Orig: tarball, OrigName: m.OrigName(),
			DebianDir: filepath.Join(e.Root, m.DebianDir()), Components: extra.components, Toolchain: extra.toolchainFile}
		src, err := e.SourceGen(ctx, s, in, filepath.Join(work, s.Name))
		if err != nil {
			return report.Wrap(err, report.Source, m.Name, m.Version, s.Name)
		}
		lintWG.Add(1)
		go func() {
			defer lintWG.Done()
			if _, err := e.Lint(ctx, e.Cfg, filepath.Join(src.Dir, src.DSC)); err != nil {
				lintErrs[i] = report.Wrap(err, report.Lintian, m.Name, m.Version, s.Name)
				cancelBuilds()
			}
		}()
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
	if o.Disk {
		tmpfs = ""
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
			case <-bctx.Done():
				j.err = bctx.Err()
				return
			}
			spec := srcbuild.Spec{Suite: j.suite, Arch: j.arch, Source: j.src, Tree: m.Name + "-" + m.Version,
				Package: m.Name, Version: m.SuiteVersion(j.suite.Level2Suffix), Epoch: entry.SourceDateEpoch, Tmpfs: tmpfs,
				Toolchain: extra.toolchains[j.arch], Timeout: e.Cfg.Level2.TimeoutOrDefault()}
			j.res, j.err = e.compileTwice(bctx, m, spec, filepath.Join(work, j.suite.Name+"-"+j.arch),
				o.Parallel || e.Cfg.Level2.ParallelVariants)
		}()
	}
	wg.Wait()
	lintWG.Wait()
	var errs report.Errors
	for _, err := range lintErrs {
		if err != nil {
			var re *report.Error
			if errors.As(err, &re) {
				errs = append(errs, re)
			}
		}
	}
	if len(errs) > 0 {
		return errs.Err() // las compilaciones se cancelaron por lintian
	}
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
	if err := e.writeSupply(supplyRep, m.Name+"_"+m.Version+".supply.json", work); err != nil {
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
	lockSHA, err := fetch.SHA256File(filepath.Join(e.Root, lock.PathIn(e.Root, m.Name)))
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
func (e *Env) compileTwice(ctx context.Context, m *manifest.Manifest, spec srcbuild.Spec, dir string, parallel bool) (*level2Result, error) {
	where := spec.Suite.Name + "/" + spec.Arch
	var shas [2]string
	var bins [2]*srcbuild.Binary
	var errs [2]error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	one := func(v int) {
		s := spec
		s.Variant = v
		bin, err := e.BinaryBuild(ctx, s, filepath.Join(dir, fmt.Sprint(v)))
		if err == nil {
			shas[v], err = fetch.SHA256File(bin.Deb)
		}
		if err != nil {
			cancel() // la otra variante ya no sirve
		}
		bins[v], errs[v] = bin, err
	}
	if parallel {
		var wg sync.WaitGroup
		for v := range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); one(v) }()
		}
		wg.Wait()
	} else {
		for v := range 2 {
			if one(v); errs[v] != nil {
				break
			}
		}
	}
	if err := firstRealError(errs[:]); err != nil {
		var f *srcbuild.Failure
		if errors.As(err, &f) {
			return nil, &report.Error{Package: m.Name, Version: m.Version, Arch: where, Stage: report.Build,
				Cause: f.Error(), Detail: f.Tail}
		}
		return nil, report.Wrap(err, report.Build, m.Name, m.Version, where)
	}
	first := bins[0]
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
func (e *Env) level2Inputs(ctx context.Context, m *manifest.Manifest, entry *lock.Entry) (string, *supply.Report, error) {
	fail := func(stage report.Stage, err error) error { return report.Wrap(err, stage, m.Name, m.Version, "") }
	url, err := m.RenderURL(m.Source.URL, "")
	if err != nil {
		return "", nil, fail(report.Validate, err)
	}
	tarball, _, err := e.Fetcher.Get(ctx, url, entry.SourceSHA256)
	if err != nil {
		return "", nil, downloadErr(err, m, "", "lock")
	}
	if _, err := e.verifySignatures(ctx, m, nil, map[string]string{url: tarball}, entry.Signatures); err != nil {
		return "", nil, err
	}
	_, prov, err := e.checkProvenance(ctx, m, map[string]provFile{url: {tarball, entry.SourceSHA256}}, entry.Provenance)
	if err != nil {
		return "", nil, err
	}
	vars, err := m.Vars("")
	if err != nil {
		return "", nil, fail(report.Validate, err)
	}
	var text []byte
	if m.License.File.Repo != "" {
		p, err := manifest.Render(m.License.File.Repo, vars)
		if err != nil {
			return "", nil, fail(report.Validate, err)
		}
		tag, _ := m.RenderedTag()
		lp, _, err := e.Fetcher.Get(ctx, fetch.LicenseURL(m.Upstream, tag, p), entry.LicenseSHA256)
		if err != nil {
			return "", nil, downloadErr(err, m, "", "lock")
		}
		if text, err = os.ReadFile(lp); err != nil {
			return "", nil, fail(report.License, err)
		}
	} else {
		p, err := manifest.Render(m.License.File.Archive, vars)
		if err != nil {
			return "", nil, fail(report.Validate, err)
		}
		if text, err = assemble.ExtractFile(tarball, m.Source.Format, p); err != nil {
			return "", nil, fail(report.License, fmt.Errorf("license.file.archive: %v", err))
		}
		if got := fetch.SHA256(text); got != entry.LicenseSHA256 {
			return "", nil, downloadErr(&fetch.HashMismatch{URL: p, Expected: entry.LicenseSHA256, Got: got}, m, "", "lock")
		}
	}
	if err := license.Gate(text, m.License.SPDX, e.Cfg.WithLicenses(m.PackageExceptionLicenses()...)); err != nil {
		return "", nil, fail(report.License, err)
	}
	return tarball, newSupplyReport(m, "", prov), nil
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

// level2Extra es lo que la fase 009 añade a la compilación de un paquete.
type level2Extra struct {
	components    map[string]string // componente -> tarball (zigdeps)
	toolchainFile string            // debian/upstream/toolchain
	toolchains    map[string]string // arch -> directorio extraído
	// Para supply.json.
	tag       *supply.TagReport
	toolchain *supply.ToolchainReport
	deps      *supply.DepsReport
}

// level2Extras vuelve a comprobar, con los valores del lockfile, el tarball
// contra el tag (FR-014), el toolchain de cada arquitectura (FR-001) y las
// dependencias del lenguaje (FR-003), y prepara el componente zigdeps.
func (e *Env) level2Extras(ctx context.Context, m *manifest.Manifest, entry *lock.Entry, tarball string,
	arches []string, work string) (*level2Extra, error) {
	fail := func(stage report.Stage, err error) error { return report.Wrap(err, stage, m.Name, m.Version, "") }
	x := &level2Extra{toolchains: map[string]string{}}
	if entry.TagArchiveSHA256 != "" {
		tagPath, _, err := e.Fetcher.Get(ctx, TagArchiveURL(m), entry.TagArchiveSHA256)
		if err != nil {
			return nil, downloadErr(err, m, "", "lock")
		}
		diff, err := supply.CompareTag(tarball, m.Source.Format, tagPath)
		if err != nil {
			return nil, fail(report.TagDiff, err)
		}
		var declared []string
		for _, f := range m.Source.ExtraFiles {
			declared = append(declared, f.Path)
		}
		locked := entry.ExtraFiles
		if locked == nil {
			locked = map[string]string{}
		}
		extras, err := diff.CheckExtras(declared, locked)
		if err != nil {
			return nil, fail(report.TagDiff, err)
		}
		x.tag = &supply.TagReport{Extra: slices.Sorted(maps.Keys(extras))}
	}
	if m.Toolchain == nil {
		return x, nil
	}
	v := &supply.Verifier{Root: e.Root, Pkg: m.Name, Sigstore: e.Sigstore}
	need := slices.Clone(arches)
	if !slices.Contains(need, srcbuild.HostArch()) && m.Deps != nil {
		need = append(need, srcbuild.HostArch()) // zig fetch corre en el host
	}
	hostTarball := ""
	for _, arch := range need {
		want := entry.Toolchain[arch]
		if want == "" {
			return nil, fail(report.Lock, fmt.Errorf("falta toolchain.%s en el lockfile; ejecuta `debfactory lock %s`", arch, m.Name))
		}
		wantSig := ""
		if m.Toolchain.Signing != nil {
			sigURL, _ := m.Toolchain.RenderURL(m.Toolchain.Signing.URL(), arch)
			wantSig = entry.Signatures[sigURL]
		}
		r, err := supply.FetchToolchain(ctx, e.Fetcher, v, m.Toolchain, arch, want, wantSig)
		if err != nil {
			return nil, fail(report.Toolchain, err)
		}
		dir := filepath.Join(work, "toolchain-"+arch)
		if err := supply.ExtractToolchain(ctx, r.Tarball, dir); err != nil {
			return nil, fail(report.Toolchain, err)
		}
		x.toolchains[arch] = dir
		if arch == srcbuild.HostArch() {
			hostTarball = r.Tarball
		}
	}
	x.toolchainFile = toolchainFile(m, entry, e.Cfg.Arches)
	x.toolchain = &supply.ToolchainReport{Name: m.Toolchain.Name, Version: m.Toolchain.Version, OK: true}
	if m.Deps == nil {
		return x, nil
	}
	depsWork := filepath.Join(work, "deps")
	if err := os.MkdirAll(depsWork, 0o755); err != nil {
		return nil, fail(report.Source, err)
	}
	list, sha, err := e.resolveDeps(ctx, m, tarball, hostTarball, depsWork, time.Unix(entry.SourceDateEpoch, 0).UTC(), entry.Deps)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(list, entry.Deps) {
		return nil, fail(report.Deps, fmt.Errorf("las dependencias resueltas no son las del lockfile (%d frente a %d o hashes distintos); ejecuta `debfactory lock %s`",
			len(list), len(entry.Deps), m.Name))
	}
	if sha != entry.ZigdepsSHA256 {
		return nil, fail(report.Deps, fmt.Errorf("el componente zigdeps da %s y el lockfile dice %s", sha, entry.ZigdepsSHA256))
	}
	x.components = map[string]string{"zigdeps": filepath.Join(depsWork, "zigdeps.tar.xz")}
	x.deps = &supply.DepsReport{Count: len(list), Skipped: slices.Sorted(maps.Keys(m.Deps.Skip)), OK: true}
	return x, nil
}

// firstRealError devuelve el primer error que no sea la cancelación
// provocada por el fallo de la otra variante.
func firstRealError(errs []error) error {
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
