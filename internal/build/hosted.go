package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/srcbuild"
	"debfactory/internal/srcpkg"
	"debfactory/internal/supply"
)

// Modo job hospedado (specs/009, contracts/pipeline.md): cada orden deja sus
// archivos en un directorio para upload-artifact; la PC los acepta después
// (debfactory accept) sin confiar en ellos más que en su atestación y en sus
// propias comprobaciones.

// SourceResult es source.json: lo que generó `level2 source`.
type SourceResult struct {
	Package    string                 `json:"package"`
	Version    string                 `json:"version"`
	OrigSHA256 string                 `json:"orig_sha256"`
	Components map[string]string      `json:"components,omitempty"` // nombre de archivo -> sha256
	Suites     map[string]SourceSuite `json:"suites"`
}

// SourceSuite es el paquete fuente de una suite.
type SourceSuite struct {
	DSC       string `json:"dsc"`
	DSCSHA256 string `json:"dsc_sha256"`
}

// HostedBuild es build.json de `level2 build`: una variante compilada.
type HostedBuild struct {
	Package         string `json:"package"`
	Version         string `json:"version"` // versión Debian del binario
	Suite           string `json:"suite"`
	Arch            string `json:"arch"`
	Variant         int    `json:"variant"`
	Deb             string `json:"deb"`
	SHA256          string `json:"sha256"`
	Buildinfo       string `json:"buildinfo"`
	BuildinfoSHA256 string `json:"buildinfo_sha256"`
	DSCSHA256       string `json:"dsc_sha256"`
}

// level2Entry valida el manifiesto de nivel 2 y devuelve su entrada del
// lockfile completa.
func (e *Env) level2Entry(name string) (*manifest.Manifest, *lock.Entry, error) {
	m, err := manifest.Validate(e.Root, e.Cfg, name)
	if err != nil {
		return nil, nil, err
	}
	if !m.Level2() {
		return nil, nil, report.Wrap(fmt.Errorf("%s es de nivel 1", name), report.Validate, m.Name, m.Version, "")
	}
	lk, err := lock.Read(e.Root, m.Name)
	if err != nil {
		return nil, nil, report.Wrap(err, report.Lock, m.Name, m.Version, "")
	}
	entry, ok := lk.Versions[m.Version]
	if !ok {
		return nil, nil, report.Wrap(fmt.Errorf("no hay entrada para %s; ejecuta `debfactory lock %s`", m.Version, m.Name),
			report.Lock, m.Name, m.Version, "")
	}
	if err := entry.CompleteSource(); err != nil {
		return nil, nil, report.Wrap(err, report.Lock, m.Name, m.Version, "")
	}
	return m, entry, nil
}

// Level2Source verifica el fuente (sha256, firma, tag, procedencia), el
// toolchain del host y las dependencias, y genera en out/<suite>/ el paquete
// fuente de cada suite no excluida, con lintian sobre cada .dsc; deja también
// out/source.json y out/supply.json.
func (e *Env) Level2Source(ctx context.Context, name, out string) error {
	m, entry, err := e.level2Entry(name)
	if err != nil {
		return err
	}
	e = e.forManifest(m)
	fail := func(stage report.Stage, err error) error { return report.Wrap(err, stage, m.Name, m.Version, "") }
	if e.SourceGen == nil {
		e.SourceGen = srcbuild.GenerateSource
	}
	tarball, rep, err := e.level2Inputs(ctx, m, entry)
	if err != nil {
		return err
	}
	work, err := e.workDir(m.Name + "-src-")
	if err != nil {
		return fail(report.Source, err)
	}
	defer os.RemoveAll(work)
	extra, err := e.level2Extras(ctx, m, entry, tarball, []string{srcbuild.HostArch()}, work)
	if err != nil {
		return err
	}
	rep.TarballVsTag, rep.Toolchain, rep.Deps = extra.tag, extra.toolchain, extra.deps
	if e.Scorecard != nil {
		rep.Scorecard = e.Scorecard(ctx, m.Upstream)
	}
	res := &SourceResult{Package: m.Name, Version: m.Version, OrigSHA256: entry.SourceSHA256, Suites: map[string]SourceSuite{}}
	for c, p := range extra.components {
		sha, err := fetch.SHA256File(p)
		if err != nil {
			return fail(report.Source, err)
		}
		if res.Components == nil {
			res.Components = map[string]string{}
		}
		res.Components[srcpkg.ComponentName(m.Name, m.Version, c)] = sha
	}
	for _, s := range Level2Suites(e.Cfg, m) {
		in := srcpkg.Input{Name: m.Name, Upstream: m.Version, Version: m.SuiteVersion(s.Level2Suffix), Suite: s.Name,
			Maintainer: e.Cfg.Maintainer, Epoch: entry.SourceDateEpoch, Orig: tarball, OrigName: m.OrigName(),
			DebianDir: filepath.Join(e.Root, m.DebianDir()), Components: extra.components, Toolchain: extra.toolchainFile}
		src, err := e.SourceGen(ctx, s, in, filepath.Join(work, "gen-"+s.Name))
		if err != nil {
			return report.Wrap(err, report.Source, m.Name, m.Version, s.Name)
		}
		if _, err := e.Lint(ctx, e.Cfg, filepath.Join(src.Dir, src.DSC)); err != nil {
			return report.Wrap(err, report.Lintian, m.Name, m.Version, s.Name)
		}
		dst := filepath.Join(out, s.Name)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fail(report.Source, err)
		}
		for _, f := range append([]string{src.DSC, src.DebianTar, src.Orig}, src.Components...) {
			if err := copyFile(filepath.Join(src.Dir, f), filepath.Join(dst, f)); err != nil {
				return fail(report.Source, err)
			}
		}
		sha, err := fetch.SHA256File(filepath.Join(dst, src.DSC))
		if err != nil {
			return fail(report.Source, err)
		}
		res.Suites[s.Name] = SourceSuite{DSC: src.DSC, DSCSHA256: sha}
	}
	if err := rep.Write(filepath.Join(out, "supply.json")); err != nil {
		return fail(report.Package, err)
	}
	if err := writeJSON(filepath.Join(out, "source.json"), res); err != nil {
		return fail(report.Package, err)
	}
	e.Out.Result(m.Name, m.Version, "", "source", out)
	return nil
}

// Level2BuildOne compila una variante de una suite y arquitectura desde el
// paquete fuente de sourceDir (salida de Level2Source para esa suite) y deja
// en out el .deb, el .buildinfo y build.json. Comprueba que el .orig y sus
// componentes son los del lockfile y verifica el toolchain de la arquitectura.
func (e *Env) Level2BuildOne(ctx context.Context, name, suite, arch string, variant int, sourceDir, out string, o Options) error {
	m, entry, err := e.level2Entry(name)
	if err != nil {
		return err
	}
	e = e.forManifest(m)
	where := suite + "/" + arch
	fail := func(stage report.Stage, err error) error { return report.Wrap(err, stage, m.Name, m.Version, where) }
	if variant != 0 && variant != 1 {
		return fail(report.Validate, fmt.Errorf("--variant debe ser 0 o 1"))
	}
	i := slices.IndexFunc(Level2Suites(e.Cfg, m), func(s config.Suite) bool { return s.Name == suite })
	if i < 0 {
		return fail(report.Validate, fmt.Errorf("suite %q no está en la configuración o el manifiesto la excluye", suite))
	}
	s := Level2Suites(e.Cfg, m)[i]
	if !slices.Contains(m.Arches(e.Cfg.Arches), arch) {
		return fail(report.Validate, fmt.Errorf("arquitectura %q no está en la configuración o el manifiesto la excluye", arch))
	}
	if e.BinaryBuild == nil {
		e.BinaryBuild = srcbuild.Build
	}
	version := m.SuiteVersion(s.Level2Suffix)
	src := &srcbuild.Source{Dir: sourceDir, DSC: m.Name + "_" + version + ".dsc",
		DebianTar: m.Name + "_" + version + ".debian.tar.xz", Orig: m.OrigName()}
	want := map[string]string{src.Orig: entry.SourceSHA256}
	if entry.ZigdepsSHA256 != "" {
		c := srcpkg.ComponentName(m.Name, m.Version, "zigdeps")
		src.Components = append(src.Components, c)
		want[c] = entry.ZigdepsSHA256
	}
	for f, sha := range want {
		got, err := fetch.SHA256File(filepath.Join(sourceDir, f))
		if err != nil {
			return fail(report.Inputs, err)
		}
		if got != sha {
			return fail(report.Verify, &fetch.HashMismatch{URL: f, Expected: sha, Got: got})
		}
	}
	work, err := e.workDir(m.Name + "-" + suite + "-" + arch + "-")
	if err != nil {
		return fail(report.Build, err)
	}
	defer os.RemoveAll(work)
	spec := srcbuild.Spec{Suite: s, Arch: arch, Source: src, Tree: m.Name + "-" + m.Version, Package: m.Name,
		Version: version, Epoch: entry.SourceDateEpoch, Variant: variant, Timeout: e.Cfg.Level2.TimeoutOrDefault()}
	if !o.Disk {
		spec.Tmpfs = e.Cfg.Level2.TmpfsOrDefault()
		if m.BuildOpts.Tmpfs != "" {
			spec.Tmpfs = m.BuildOpts.Tmpfs
		}
	}
	if m.Toolchain != nil {
		dir, err := e.verifiedToolchain(ctx, m, entry, arch, work)
		if err != nil {
			return err
		}
		spec.Toolchain = dir
	}
	bin, err := e.BinaryBuild(ctx, spec, filepath.Join(work, "out"))
	if err != nil {
		var f *srcbuild.Failure
		if errorsAs(err, &f) {
			return &report.Error{Package: m.Name, Version: m.Version, Arch: where, Stage: report.Build, Cause: f.Error(), Detail: f.Tail}
		}
		return fail(report.Build, err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fail(report.Package, err)
	}
	res := HostedBuild{Package: m.Name, Version: version, Suite: suite, Arch: arch, Variant: variant,
		Deb: filepath.Base(bin.Deb), Buildinfo: filepath.Base(bin.Buildinfo)}
	for _, f := range []struct {
		src string
		sha *string
	}{{bin.Deb, &res.SHA256}, {bin.Buildinfo, &res.BuildinfoSHA256}, {filepath.Join(sourceDir, src.DSC), &res.DSCSHA256}} {
		if *f.sha, err = fetch.SHA256File(f.src); err != nil {
			return fail(report.Package, err)
		}
	}
	for _, f := range []string{bin.Deb, bin.Buildinfo} {
		if err := copyFile(f, filepath.Join(out, filepath.Base(f))); err != nil {
			return fail(report.Package, err)
		}
	}
	if err := writeJSON(filepath.Join(out, "build.json"), res); err != nil {
		return fail(report.Package, err)
	}
	e.Out.Result(m.Name, m.Version, arch, "deb", filepath.Join(out, res.Deb))
	return nil
}

// verifiedToolchain baja, verifica (sha256 del lockfile y firma) y extrae el
// toolchain de arch en work.
func (e *Env) verifiedToolchain(ctx context.Context, m *manifest.Manifest, entry *lock.Entry, arch, work string) (string, error) {
	fail := func(err error) error { return report.Wrap(err, report.Toolchain, m.Name, m.Version, arch) }
	want := entry.Toolchain[arch]
	if want == "" {
		return "", fail(fmt.Errorf("falta toolchain.%s en el lockfile; ejecuta `debfactory lock %s`", arch, m.Name))
	}
	wantSig := ""
	if m.Toolchain.Signing != nil {
		u, _ := m.Toolchain.RenderURL(m.Toolchain.Signing.URL(), arch)
		wantSig = entry.Signatures[u]
	}
	v := &supply.Verifier{Root: e.Root, Pkg: m.Name, Sigstore: e.Sigstore}
	r, err := supply.FetchToolchain(ctx, e.Fetcher, v, m.Toolchain, arch, want, wantSig)
	if err != nil {
		return "", fail(err)
	}
	dir := filepath.Join(work, "toolchain-"+arch)
	if err := supply.ExtractToolchain(ctx, r.Tarball, dir); err != nil {
		return "", fail(err)
	}
	return dir, nil
}

func (e *Env) workDir(prefix string) (string, error) {
	if err := os.MkdirAll(e.Workdir, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(e.Workdir, prefix)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func errorsAs(err error, target any) bool { return errors.As(err, target) }

// copyFile copia src a dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// distDir es la carpeta de salida de los builds.
func (e *Env) distDir() string {
	if e.Dist != "" {
		return e.Dist
	}
	return filepath.Join(e.Root, "dist")
}
