package ci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
	"debfactory/internal/srcpkg"
	"debfactory/internal/supply"
)

// Accept es la verificación independiente en la PC de lo que compilaron y
// probaron los runners de GitHub (specs/009, research D17). Nada de lo que
// llega se publica sin: atestación del run esperado en cada archivo; nivel 2,
// las dos variantes idénticas; nivel 1, la misma salida reconstruida en la
// PC; lintian local de cada .deb; prueba de humo amd64 en la PC; y los
// informes de prueba de cada suite y arquitectura no excluida.
type Accept struct {
	Dir string     // artefactos descargados (un subdirectorio por artefacto)
	Run supply.Run // run cuyos artefactos se aceptan
	// Attest verifica la atestación de un archivo (por defecto VerifyRun con
	// la API de GitHub y cosign).
	Attest func(ctx context.Context, run supply.Run, file, sha256 string) error
	// Smoke prueba un .deb en la PC (por defecto installtest.RunSuite).
	Smoke func(ctx context.Context, s config.Suite, d installtest.Deb) installtest.Result
}

// Nombres de los artefactos del run (contracts/pipeline.md, data-model §6).
func l1Artifact(p, arch string) string          { return "l1-" + p + "-" + arch }
func sourceArtifact(p string) string            { return "source-" + p }
func testArtifact(p, suite, arch string) string { return "test-" + p + "-" + suite + "-" + arch }
func buildArtifact(p, suite, arch string, v int) string {
	return fmt.Sprintf("build-%s-%s-%s-%d", p, suite, arch, v)
}

// accepter acepta un paquete en root/dist.
type accepter struct {
	r    *Runner
	a    *Accept
	root string
	m    *manifest.Manifest
	errs report.Errors
}

func (x *accepter) fail(stage report.Stage, where, format string, args ...any) {
	x.errs = append(x.errs, &report.Error{Package: x.m.Name, Version: x.m.DebVersion(), Arch: where, Stage: stage,
		Cause: fmt.Sprintf(format, args...)})
}

// file devuelve la ruta y el sha256 de un archivo de un artefacto ya
// verificado por su atestación ("" si falta o no verifica).
func (x *accepter) file(ctx context.Context, where, artifact, name string) (string, string) {
	p := filepath.Join(x.a.Dir, artifact, name)
	sha, err := fetch.SHA256File(p)
	if err != nil {
		x.fail(report.Inputs, where, "falta %s/%s", artifact, name)
		return "", ""
	}
	if err := x.a.Attest(ctx, x.a.Run, p, sha); err != nil {
		x.fail(report.Attest, where, "%s/%s: %v", artifact, name, err)
		return "", ""
	}
	return p, sha
}

func (x *accepter) readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, v)
	}
	if err != nil {
		x.fail(report.Inputs, "", "%s: %v", filepath.Base(path), err)
		return false
	}
	return true
}

func (x *accepter) toDist(src, name string) {
	if err := copyTo(src, filepath.Join(x.root, "dist", name)); err != nil {
		x.fail(report.Package, "", "dist/%s: %v", name, err)
	}
}

// AcceptPackage verifica los artefactos de name y deja en root/dist lo mismo
// que dejarían build y test en local; devuelve los sha256 como gates.
func (r *Runner) AcceptPackage(ctx context.Context, root, name string, a *Accept) (map[string]string, error) {
	m, err := manifest.Validate(root, r.Cfg, name)
	if err != nil {
		return nil, err
	}
	if a.Attest == nil {
		gh := &fetch.GitHub{Token: os.Getenv("GITHUB_TOKEN")}
		at := &supply.Attestor{List: gh.Attestations}
		a.Attest = at.VerifyRun
	}
	if a.Smoke == nil {
		a.Smoke = installtest.RunSuite
	}
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		return nil, err
	}
	x := &accepter{r: r, a: a, root: root, m: m}
	if m.Level2() {
		x.level2(ctx)
	} else {
		x.level1(ctx)
	}
	if err := x.errs.Err(); err != nil {
		return nil, err
	}
	return distShas(root, r.Cfg, m)
}

// suites son las de la configuración que el manifiesto no excluye.
func (x *accepter) suites() []config.Suite {
	var out []config.Suite
	for _, s := range x.r.Cfg.Suites {
		if _, ex := x.m.Exclude.Suites[s.Name]; !ex {
			out = append(out, s)
		}
	}
	return out
}

// testReport verifica el informe de una suite y arquitectura y que probó
// exactamente ese .deb.
func (x *accepter) testReport(ctx context.Context, suite, arch, debSHA string) *installtest.Report {
	where := suite + "/" + arch
	p, _ := x.file(ctx, where, testArtifact(x.m.Name, suite, arch), "test.json")
	if p == "" {
		return nil
	}
	var tr installtest.Report
	if !x.readJSON(p, &tr) {
		return nil
	}
	switch {
	case tr.Overall != "pass" || len(tr.Results) != 1 || tr.Results[0].Suite != suite || tr.Results[0].Status != "pass":
		x.fail(report.Test, where, "la prueba de GitHub no pasó en %s", suite)
	case tr.DebSHA256 != debSHA:
		x.fail(report.Inputs, where, "test.json probó otro .deb (%s, se aceptó %s)", tr.DebSHA256, debSHA)
	default:
		return &tr
	}
	return nil
}

// smoke instala y ejecuta el .deb amd64 en la primera suite no excluida.
func (x *accepter) smoke(ctx context.Context, deb, command string, lifecycle bool) {
	ss := x.suites()
	if len(ss) == 0 {
		return
	}
	vars, _ := x.m.Vars("amd64")
	expect, err := manifest.Render(x.m.Test.Expect, vars)
	if err != nil {
		x.fail(report.Validate, "", "test.expect: %v", err)
		return
	}
	if command == "" {
		command = x.m.Test.Command
	}
	if command == "" {
		command = x.m.Name + " --version"
	}
	res := x.a.Smoke(ctx, ss[0], installtest.Deb{Path: deb, Package: x.m.Name, Arch: "amd64", TestCommand: command,
		Expect: expect, Lifecycle: lifecycle})
	if res.Status != "pass" {
		x.fail(report.Test, ss[0].Name+"/amd64", "prueba de humo en la PC: %s: %s", res.Stage, res.Reason)
	}
}

// level1: reconstrucción en la PC con el mismo sha256 (lintian incluido),
// informes de prueba de cada suite y prueba de humo amd64.
func (x *accepter) level1(ctx context.Context) {
	m := x.m
	for _, arch := range m.Arches(x.r.Cfg.Arches) {
		base := m.DebBase(arch)
		art := l1Artifact(m.Name, arch)
		deb, sha := x.file(ctx, arch, art, base+".deb")
		if deb == "" {
			continue
		}
		local, err := os.MkdirTemp(x.r.Workdir, m.Name+"-accept-")
		if err != nil {
			x.fail(report.Build, arch, "%v", err)
			continue
		}
		defer os.RemoveAll(local)
		b := x.r.Build()
		b.Root, b.Dist = x.root, local
		if err := b.Build(ctx, m.Name, build.Options{Arch: arch}); err != nil {
			x.errs = append(x.errs, asReport(err, m, arch))
			continue
		}
		mine, err := fetch.SHA256File(filepath.Join(local, base+".deb"))
		if err != nil || mine != sha {
			x.fail(report.Compare, arch, "el .deb de GitHub (%s) no es el que reconstruye la PC (%s)", sha, mine)
			continue
		}
		info, err := pkg.ReadBuildInfo(filepath.Join(local, base+".build.json"))
		if err != nil {
			x.fail(report.Build, arch, "%v", err)
			continue
		}
		merged := &installtest.Report{Package: m.Name, Version: m.DebVersion(), Arch: arch, DebSHA256: sha, Versions: "pass", Overall: "pass"}
		complete := true
		for _, s := range x.r.Cfg.Suites {
			if reason, ex := m.Exclude.Suites[s.Name]; ex {
				merged.Results = append(merged.Results, installtest.Result{Suite: s.Name, Status: "excluded", Reason: reason})
				continue
			}
			tr := x.testReport(ctx, s.Name, arch, sha)
			if tr == nil {
				complete = false
				continue
			}
			merged.Results = append(merged.Results, tr.Results[0])
		}
		if arch == "amd64" {
			x.smoke(ctx, deb, info.TestCommand, false)
		}
		if !complete {
			continue
		}
		x.toDist(deb, base+".deb")
		x.toDist(filepath.Join(local, base+".build.json"), base+".build.json")
		x.toDist(filepath.Join(local, base+".supply.json"), base+".supply.json")
		if err := merged.Write(filepath.Join(x.root, "dist", base+".test.json")); err != nil {
			x.fail(report.Package, arch, "%v", err)
		}
	}
}

// level2: fuente atestado y fiel al lockfile, dos variantes idénticas por
// suite y arquitectura, lintian local, informes de prueba y prueba de humo.
func (x *accepter) level2(ctx context.Context) {
	m := x.m
	lk, err := lock.Read(x.root, m.Name)
	if err != nil {
		x.fail(report.Lock, "", "%v", err)
		return
	}
	entry := lk.Versions[m.Version]
	if entry == nil {
		x.fail(report.Lock, "", "el lockfile no tiene %s", m.Version)
		return
	}
	src := sourceArtifact(m.Name)
	var sr build.SourceResult
	if p, _ := x.file(ctx, "", src, "source.json"); p == "" || !x.readJSON(p, &sr) {
		return
	}
	if p, _ := x.file(ctx, "", src, "supply.json"); p != "" {
		x.toDist(p, m.Name+"_"+m.Version+".supply.json")
	}
	want := map[string]string{m.OrigName(): entry.SourceSHA256}
	if entry.ZigdepsSHA256 != "" {
		want[srcpkg.ComponentName(m.Name, m.Version, "zigdeps")] = entry.ZigdepsSHA256
	}
	manifestSHA, _ := fetch.SHA256File(filepath.Join(x.root, m.File))
	lockSHA, _ := fetch.SHA256File(filepath.Join(x.root, lock.PathIn(x.root, m.Name)))
	smoked := false
	for _, s := range x.suites() {
		v := m.SuiteVersion(s.Level2Suffix)
		dir := filepath.Join(src, s.Name)
		ss, ok := sr.Suites[s.Name]
		if !ok {
			x.fail(report.Inputs, s.Name, "source.json no tiene la suite %s", s.Name)
			continue
		}
		dsc, dscSHA := x.file(ctx, s.Name, dir, ss.DSC)
		tarPath, _ := x.file(ctx, s.Name, dir, m.Name+"_"+v+".debian.tar.xz")
		if dsc == "" || tarPath == "" || dscSHA != ss.DSCSHA256 {
			if dsc != "" && dscSHA != ss.DSCSHA256 {
				x.fail(report.Inputs, s.Name, "el .dsc no es el de source.json")
			}
			continue
		}
		for f, sha := range want {
			p, got := x.file(ctx, s.Name, dir, f)
			if p != "" && got != sha {
				x.fail(report.Verify, s.Name, "%s no es el del lockfile (%s, se esperaba %s)", f, got, sha)
				continue
			}
			if p != "" {
				x.toDist(p, f)
			}
		}
		x.toDist(dsc, ss.DSC)
		x.toDist(tarPath, m.Name+"_"+v+".debian.tar.xz")
		for _, arch := range m.Arches(x.r.Cfg.Arches) {
			where := s.Name + "/" + arch
			var hb [2]build.HostedBuild
			var debs [2]string
			ok := true
			for vi := range 2 {
				art := buildArtifact(m.Name, s.Name, arch, vi)
				p, _ := x.file(ctx, where, art, "build.json")
				if p == "" || !x.readJSON(p, &hb[vi]) {
					ok = false
					continue
				}
				deb, debSHA := x.file(ctx, where, art, hb[vi].Deb)
				bi, biSHA := x.file(ctx, where, art, hb[vi].Buildinfo)
				switch {
				case deb == "" || bi == "":
					ok = false
				case debSHA != hb[vi].SHA256 || biSHA != hb[vi].BuildinfoSHA256 || hb[vi].Variant != vi ||
					hb[vi].Suite != s.Name || hb[vi].Arch != arch || hb[vi].DSCSHA256 != dscSHA:
					x.fail(report.Inputs, where, "%s/build.json no corresponde a sus archivos o a esta combinación", art)
					ok = false
				default:
					debs[vi] = deb
				}
			}
			if !ok {
				continue
			}
			if hb[0].SHA256 != hb[1].SHA256 {
				x.fail(report.Compare, where, "no reproducible: las dos variantes dan %s y %s", hb[0].SHA256, hb[1].SHA256)
				continue
			}
			b := x.r.Build()
			if _, err := b.Lint(ctx, x.r.Cfg, debs[0]); err != nil {
				x.errs = append(x.errs, asReport(err, m, where))
				continue
			}
			tr := x.testReport(ctx, s.Name, arch, hb[0].SHA256)
			if arch == "amd64" && !smoked {
				x.smoke(ctx, debs[0], m.Test.Command, true)
				smoked = true
			}
			if tr == nil {
				continue
			}
			base := build.Level2Base(m, s, arch)
			art := buildArtifact(m.Name, s.Name, arch, 0)
			x.toDist(debs[0], base+".deb")
			x.toDist(filepath.Join(x.a.Dir, art, hb[0].Buildinfo), base+".buildinfo")
			info := &pkg.BuildInfo{SHA256: hb[0].SHA256, Lintian: "pass", Overrides: []string{}, TestCommand: m.Test.Command,
				ManifestSHA256: manifestSHA, LockSHA256: lockSHA, Suite: s.Name, DSCSHA256: dscSHA,
				BuildinfoSHA256: hb[0].BuildinfoSHA256, Reproducible: true}
			if b, err := info.Marshal(); err != nil {
				x.fail(report.Package, where, "%v", err)
			} else if err := os.WriteFile(filepath.Join(x.root, "dist", base+".build.json"), b, 0o644); err != nil {
				x.fail(report.Package, where, "%v", err)
			}
			if err := tr.Write(filepath.Join(x.root, "dist", base+".test.json")); err != nil {
				x.fail(report.Package, where, "%v", err)
			}
		}
	}
}

func asReport(err error, m *manifest.Manifest, where string) *report.Error {
	var re *report.Error
	if errors.As(err, &re) {
		return re
	}
	return &report.Error{Package: m.Name, Version: m.DebVersion(), Arch: where, Stage: report.Build, Cause: err.Error()}
}

// copyTo copia src a dst.
func copyTo(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// hostedAccept prepara la aceptación de los artefactos del run de este job
// (ci main --artifacts). FR-012: con un repositorio de paquetes privado no se
// usan runners hospedados.
func (r *Runner) hostedAccept(ctx context.Context, dir string) (*Accept, error) {
	gh, err := r.forge()
	if err != nil {
		return nil, err
	}
	info, err := gh.GetRepo(ctx)
	if err != nil {
		return nil, report.Newf(report.Config, "no se pudo leer %s: %v", r.Cfg.CI.Repository, err)
	}
	if info.Private {
		return nil, report.Newf(report.Config, "%s no es público: el modo hospedado no se usa (FR-012); ejecuta `ci main --local`",
			r.Cfg.CI.Repository)
	}
	run, err := RunFromEnv(r.Env)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	a := &Accept{Dir: abs, Run: run}
	if r.AcceptHooks != nil {
		a.Attest, a.Smoke = r.AcceptHooks.Attest, r.AcceptHooks.Smoke
	}
	return a, nil
}

// RunFromEnv identifica el run desde las variables del job: repositorio,
// ruta del workflow y ref (GITHUB_WORKFLOW_REF) y commit (GITHUB_SHA).
func RunFromEnv(env *Env) (supply.Run, error) {
	ref := env.WorkflowRef
	prefix := env.Repository + "/"
	at := strings.LastIndex(ref, "@")
	if env.Repository == "" || env.SHA == "" || !strings.HasPrefix(ref, prefix) || at < len(prefix) {
		return supply.Run{}, &InputError{fmt.Sprintf("ci main --artifacts necesita GITHUB_REPOSITORY, GITHUB_SHA y GITHUB_WORKFLOW_REF (%q)", ref)}
	}
	return supply.Run{Repo: env.Repository, Workflow: ref[len(prefix):at], Ref: ref[at+1:], Commit: env.SHA}, nil
}

// gatesFor elige cómo se validan los artefactos de un paquete: con accept
// (modo hospedado) o, para las ampliaciones (suite o arquitectura nueva,
// poco frecuentes), con el flujo local en la PC: la matriz hospedada solo
// compila versiones nuevas y accept exige todos sus artefactos (specs/009,
// T042).
func (r *Runner) gatesFor(ctx context.Context, root string, ap AffectedPackage, lim installtest.Limiter, out *report.Printer) (map[string]string, error) {
	return r.gatesMode(ctx, root, ap.Package, lim, out, r.accept != nil && ap.Reason != Extend)
}
