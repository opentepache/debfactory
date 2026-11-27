package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"debfactory/internal/assemble"
	"debfactory/internal/fetch"
	"debfactory/internal/license"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
)

// Options de build.
type Options struct {
	Arch         string // vacío = todas las de la config
	Suite        string // nivel 2: vacío = todas las no excluidas
	Reproducible bool   // construir dos veces y comparar sha256
	Jobs         int    // build --all: paquetes simultáneos (mínimo 1)
}

// Build construye un paquete: validate → lock → fetch → verify → license →
// assemble → package → lintian, y copia el .deb y su build.json a dist/.
// Nada llega a dist/ sin pasar el gate de licencias y lintian.
func (e *Env) Build(ctx context.Context, name string, o Options) error {
	m, err := manifest.Validate(e.Root, e.Cfg, name)
	if err != nil {
		return err
	}
	lk, err := lock.Read(e.Root, m.Name)
	if err != nil {
		return report.Wrap(err, report.Lock, m.Name, m.Version, "")
	}
	if m.Level2() {
		return e.buildLevel2(ctx, m, lk, o)
	}
	arches := e.Cfg.Arches
	if o.Arch != "" {
		arches = []string{o.Arch}
	}
	for _, arch := range arches {
		if err := e.buildArch(ctx, m, lk, arch, o.Reproducible); err != nil {
			return err
		}
	}
	return nil
}

// BuildAll construye todos los paquetes de packages/ en paralelo (hasta
// o.Jobs a la vez); sigue si uno falla y al final imprime, en orden
// alfabético, la salida de cada paquete y un resumen.
func (e *Env) BuildAll(ctx context.Context, names []string, o Options) error {
	type result struct {
		name, status string
		flush        func()
	}
	results := make([]result, len(names))
	sem := make(chan struct{}, max(o.Jobs, 1))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, flush := e.Out.Buffered()
			results[i] = result{name: name, status: "ok", flush: flush}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i].status = "fail (cancelado)"
				return
			}
			pe := *e
			pe.Out = out
			err := pe.Build(ctx, name, o)
			if err == nil {
				return
			}
			out.Fail(err)
			stage := "?"
			var re *report.Error
			var list report.Errors
			switch {
			case errors.As(err, &list) && len(list) > 0:
				stage = string(list[0].Stage)
			case errors.As(err, &re):
				stage = string(re.Stage)
			}
			results[i].status = "fail (" + stage + ")"
		}()
	}
	wg.Wait()
	failed := 0
	for _, r := range results {
		r.flush()
		if r.status != "ok" {
			failed++
		}
	}
	e.Out.Line("\nresumen de build --all:")
	for _, r := range results {
		e.Out.Line("  %-12s %s", r.name, r.status)
		if e.Out.JSON {
			e.Out.Result(r.name, "", "", "build-all", r.status)
		}
	}
	if failed > 0 {
		return fmt.Errorf("debfactory: build --all: %d de %d paquetes fallaron", failed, len(names))
	}
	return nil
}

// built es el resultado de una construcción en un directorio de trabajo.
type built struct {
	deb, sha, testCommand string
}

func (e *Env) buildArch(ctx context.Context, m *manifest.Manifest, lk *lock.File, arch string, reproducible bool) error {
	fail := func(stage report.Stage, err error) error {
		return report.Wrap(err, stage, m.Name, m.Version, arch)
	}
	entry, ok := lk.Versions[m.Version]
	if !ok {
		return fail(report.Lock, fmt.Errorf("no hay entrada para %s en %s; ejecuta `debfactory lock %s`",
			m.Version, lock.Path(m.Name), m.Name))
	}
	if err := entry.Complete(e.Cfg.Arches, m.Checksums != nil); err != nil {
		return fail(report.Lock, fmt.Errorf("entrada incompleta para %s en %s (%v); ejecuta `debfactory lock %s`",
			m.Version, lock.Path(m.Name), err, m.Name))
	}
	if err := os.MkdirAll(e.Workdir, 0o700); err != nil {
		return fail(report.Assemble, err)
	}
	work, err := os.MkdirTemp(e.Workdir, m.Name+"-"+arch+"-")
	if err != nil {
		return fail(report.Assemble, err)
	}
	defer os.RemoveAll(work)

	artifact, text, err := e.inputs(ctx, m, entry, arch)
	if err != nil {
		return err
	}
	first, err := e.assembleAndPackage(ctx, m, entry, arch, artifact, text, filepath.Join(work, "1"))
	if err != nil {
		return err
	}
	if reproducible {
		second, err := e.assembleAndPackage(ctx, m, entry, arch, artifact, text, filepath.Join(work, "2"))
		if err != nil {
			return err
		}
		if first.sha != second.sha {
			return fail(report.Package, fmt.Errorf("no reproducible: dos construcciones dan %s y %s", first.sha, second.sha))
		}
	}
	lres, err := e.Lint(ctx, e.Cfg, first.deb)
	if err != nil {
		return fail(report.Lintian, err)
	}
	info := &pkg.BuildInfo{SHA256: first.sha, Lintian: "pass", Overrides: pkg.OverrideLines(m), TestCommand: first.testCommand}
	if info.ManifestSHA256, err = fetch.SHA256File(filepath.Join(e.Root, m.File)); err != nil {
		return fail(report.Package, err)
	}
	if info.LockSHA256, err = fetch.SHA256File(filepath.Join(e.Root, lock.Path(m.Name))); err != nil {
		return fail(report.Package, err)
	}
	if err := e.publishDist(first.deb, info, m.DebBase(arch)); err != nil {
		return fail(report.Package, err)
	}
	e.Out.Result(m.Name, m.Version, arch, "deb", path.Join("dist", m.DebBase(arch)+".deb"))
	for _, w := range lres.Warnings {
		e.Out.Result(m.Name, m.Version, arch, "lintian-warning", w)
	}
	e.Out.Result(m.Name, m.Version, arch, "lintian", "pass")
	if reproducible {
		e.Out.Result(m.Name, m.Version, arch, "reproducible", first.sha)
	}
	return nil
}

// inputs descarga y verifica el artefacto (contra el lock y, si existen,
// contra los checksums upstream), obtiene el texto de licencia verificado y
// pasa el gate de licencias, en ese orden (constitución, Flujo 2 y 3).
func (e *Env) inputs(ctx context.Context, m *manifest.Manifest, entry *lock.Entry, arch string) (string, []byte, error) {
	fail := func(stage report.Stage, err error) error {
		return report.Wrap(err, stage, m.Name, m.Version, arch)
	}
	url, err := m.RenderURL(m.Artifact.URL, arch)
	if err != nil {
		return "", nil, fail(report.Validate, err)
	}
	artifact, _, err := e.Fetcher.Get(ctx, url, entry.Artifacts[arch])
	if err != nil {
		return "", nil, downloadErr(err, m, arch, "lock")
	}
	if m.Checksums != nil {
		curl, err := m.RenderURL(m.Checksums.URL, arch)
		if err != nil {
			return "", nil, fail(report.Validate, err)
		}
		cpath, _, err := e.Fetcher.Get(ctx, curl, entry.ChecksumsSHA256)
		if err != nil {
			return "", nil, downloadErr(err, m, arch, "lock")
		}
		data, err := os.ReadFile(cpath)
		if err != nil {
			return "", nil, fail(report.Verify, err)
		}
		sums, err := fetch.ParseChecksums(data)
		if err != nil {
			return "", nil, fail(report.Verify, fmt.Errorf("%s: %v", path.Base(curl), err))
		}
		if err := fetch.VerifyChecksum(sums, curl, url, entry.Artifacts[arch]); err != nil {
			return "", nil, downloadErr(err, m, arch, "lock")
		}
	}

	vars, err := m.Vars(arch)
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
			return "", nil, downloadErr(err, m, arch, "lock")
		}
		if text, err = os.ReadFile(lp); err != nil {
			return "", nil, fail(report.License, err)
		}
	} else {
		p, err := manifest.Render(m.License.File.Archive, vars)
		if err != nil {
			return "", nil, fail(report.Validate, err)
		}
		if text, err = assemble.ExtractFile(artifact, m.Artifact.Format, p); err != nil {
			return "", nil, fail(report.License, fmt.Errorf("license.file.archive: %v", err))
		}
		if got := fetch.SHA256(text); got != entry.LicenseSHA256 {
			return "", nil, downloadErr(&fetch.HashMismatch{URL: p, Expected: entry.LicenseSHA256, Got: got}, m, arch, "lock")
		}
	}
	if err := license.Gate(text, m.License.SPDX, e.Cfg); err != nil {
		return "", nil, fail(report.License, err)
	}
	return artifact, text, nil
}

// assembleAndPackage extrae el artefacto en dir, resuelve los archivos y
// escribe el .deb en dir/out.
func (e *Env) assembleAndPackage(ctx context.Context, m *manifest.Manifest, entry *lock.Entry, arch, artifact string,
	licenseText []byte, dir string) (*built, error) {
	fail := func(stage report.Stage, err error) error {
		return report.Wrap(err, stage, m.Name, m.Version, arch)
	}
	src := filepath.Join(dir, "src")
	if err := assemble.Extract(artifact, m.Artifact.Format, src, m.Name); err != nil {
		return nil, fail(report.Assemble, fmt.Errorf("extracción de %s: %v", m.Artifact.Format, err))
	}
	files, err := assemble.Install(m, arch, src)
	if err != nil {
		return nil, fail(report.Assemble, err)
	}
	man, err := assemble.Man(m, arch, src, filepath.Join(dir, "man"))
	if err != nil {
		return nil, fail(report.Assemble, err)
	}
	comp, err := assemble.Completions(ctx, m, arch, src, filepath.Join(dir, "completions"), files, e.Generate)
	if err != nil {
		return nil, fail(report.Assemble, err)
	}
	files, err = assemble.Sorted(append(append(files, man...), comp...))
	if err != nil {
		return nil, fail(report.Assemble, err)
	}
	depends, err := assemble.ELFDepends(files, e.Cfg.ELFDependencies)
	if err != nil {
		return nil, fail(report.Assemble, err)
	}
	testCommand := m.Test.Command
	if testCommand == "" {
		if testCommand, err = defaultTestCommand(files); err != nil {
			return nil, fail(report.Assemble, err)
		}
	}
	meta, err := pkg.MetaFiles(m, e.Cfg.Maintainer, licenseText, entry.SourceDateEpoch, filepath.Join(dir, "meta"))
	if err != nil {
		return nil, fail(report.Package, err)
	}
	all, err := assemble.Sorted(append(files, meta...))
	if err != nil {
		return nil, fail(report.Assemble, err)
	}
	plan := &pkg.Plan{
		Name: m.Name, Version: m.Version, Revision: m.Revision, Arch: arch,
		Maintainer: e.Cfg.Maintainer, Summary: m.Summary, Description: m.Description,
		Homepage: m.Homepage, Section: m.Section,
		Depends:    assemble.MergeDepends(depends, m.Depends),
		Recommends: m.Recommends, Conflicts: m.Conflicts, Replaces: m.Replaces, Provides: m.Provides,
		Files: all, MTime: time.Unix(entry.SourceDateEpoch, 0).UTC(),
	}
	deb, err := pkg.Build(plan, filepath.Join(dir, "out"))
	if err != nil {
		return nil, fail(report.Package, err)
	}
	sha, err := fetch.SHA256File(deb)
	if err != nil {
		return nil, fail(report.Package, err)
	}
	return &built{deb: deb, sha: sha, testCommand: testCommand}, nil
}

// defaultTestCommand es "<primer archivo instalado bajo /usr/bin> --version"
// (data-model.md); se resuelve después de ensamblar por los globs.
func defaultTestCommand(files []assemble.File) (string, error) {
	var bins []string
	for _, f := range files {
		if path.Dir(f.Dst) == "/usr/bin" {
			bins = append(bins, path.Base(f.Dst))
		}
	}
	if len(bins) == 0 {
		return "", errors.New("no hay ejecutables en /usr/bin; declara test.command")
	}
	sort.Strings(bins)
	return bins[0] + " --version", nil
}

// publishDist copia el .deb y su build.json a dist/ de forma atómica: primero
// a dist/.tmp-<archivo> y luego rename dentro de dist/ (el workdir está en
// tmpfs y rename entre sistemas de archivos falla con EXDEV).
func (e *Env) publishDist(deb string, info *pkg.BuildInfo, base string) error {
	dist := filepath.Join(e.Root, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return err
	}
	data, err := info.Marshal()
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dist, base+".deb"), func(w io.Writer) error {
		fh, err := os.Open(deb)
		if err != nil {
			return err
		}
		defer fh.Close()
		_, err = io.Copy(w, fh)
		return err
	}); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dist, base+".build.json"), func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

func atomicWrite(dst string, fill func(io.Writer) error) error {
	tmp := filepath.Join(filepath.Dir(dst), ".tmp-"+filepath.Base(dst))
	fh, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := fill(fh); err != nil {
		fh.Close()
		os.Remove(tmp)
		return err
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		os.Remove(tmp)
		return err
	}
	if err := fh.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ListPackages devuelve los nombres de packages/*.yaml (sin lockfiles) en
// orden alfabético.
func ListPackages(root string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(root, "packages", "*.yaml"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".lock.yaml") {
			names = append(names, manifest.FileName(f))
		}
	}
	sort.Strings(names)
	return names, nil
}
