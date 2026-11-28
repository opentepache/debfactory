// Orden debfactory: construye .deb de nivel 1 a partir de manifiestos
// declarativos (specs/001-manifest-deb-builder/contracts/cli.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"

	"debfactory/internal/build"
	"debfactory/internal/ci"
	"debfactory/internal/config"
	"debfactory/internal/container"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/lint"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/supply"
	"debfactory/internal/verify"
	"debfactory/internal/version"
)

const usage = `uso: debfactory <orden> [opciones]

Órdenes:
  validate [pkg…]                              valida manifiestos (sin pkg, todos)
  lock <pkg> [--force]                         genera o actualiza el lockfile del paquete
  migrate layout                               pasa cada paquete a packages/<pkg>/ (manifiesto, lockfile, debian/)
  lock <pkg> --prune                           borra del lockfile las versiones viejas
  build <pkg> [--arch A] [--suite S] [--check-reproducible]   (nivel 2: --suite y doble compilación siempre)
  build --all [--check-reproducible] [--jobs N] construye en dist/
  build <pkg> --arch A --out DIR               (job hospedado, nivel 1) deja el .deb en DIR
  level2 source <pkg> --out DIR                (job hospedado) verifica y genera el paquete fuente
  level2 build <pkg> --suite S --arch A --variant V --source DIR --out DIR [--disk]
                                               (job hospedado) compila una variante
  lint <deb…>                                  pasa lintian a .deb existentes
  test <pkg> [--arch A] [--suite S] [--force] [--jobs N]
  test --all [--force] [--jobs N]              prueba la instalación en cada suite
  test <pkg> --suite S --arch A --deb FILE --out DIR   (job hospedado) prueba un .deb
  publish <pkg> [<version>] [--dry-run]
  publish --all [--dry-run]                    publica en el repositorio APT firmado
  retire <pkg> <version> --reason TEXTO [--last] [--dry-run]
  repo verify [--dest D]                       comprueba el árbol local o un destino
  repo sync <destino> [--dry-run]              sube el repositorio a un destino
  repo rebuild                                 regenera el árbol desde repo/state.yaml
  repo status                                  muestra lo publicado y lo sincronizado
  repo serve [--port 8080]                     sirve el destino local por HTTP
  site data [--state FILE] [--out FILE]        modelo del sitio web (site.json) de lo publicado
  site build --in FILE --out DIR               genera el sitio desde un site.json
  site serve --dir DIR [--addr 127.0.0.1:8080] sirve un sitio generado en local
  site sync --dest D [--state FILE] [--dry-run] genera y sube el sitio a un destino
  ci <orden> [opciones]                        pipeline de GitHub Actions (debfactory ci help)
  supply review <pkg> --from V1 --to V2      resumen de aviso del cambio entre versiones (markdown)
  verify <pkg> <version> [--suite S] [--arch A] [--site URL] [--keyring F] [--no-rebuild]
                                               comprueba un paquete publicado (sin credenciales)
  version                                      imprime el commit de esta compilación

Opciones globales: --root DIR, --workdir DIR, --cache DIR, --data DIR, --json
Códigos de salida: 0 éxito, 1 fallo de una etapa, 2 uso incorrecto, 3 entorno incompleto.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// usageError es un uso incorrecto (código 2).
type usageError struct{ msg string }

func (e usageError) Error() string { return "debfactory: " + e.msg }

// errReported indica un fallo ya impreso (código 1).
var errReported = errors.New("fallo ya informado")

// options son las opciones de todas las órdenes; cada orden registra las suyas.
type options struct {
	root, workdir, cache, data string
	json                       bool
	arch, suite                string
	all, force, prune          bool
	reproducible               bool
	dryRun, last               bool
	reason, dest               string
	port, jobs                 int

	// modo job hospedado (specs/009)
	deb, source string
	variant     int
	disk        bool
	artifacts   string
	local       bool
	site        string
	keyring     string
	noRebuild   bool
	from, to    string

	// ci (specs/003-ci-pipeline/contracts/cli.md)
	envFile, mode, base       string
	pkg, version              string
	stage, stageFile, against string
	fix                       bool

	// site (specs/006-repo-website/contracts/cli.md)
	state, in, out, dir, addr string
}

type app struct {
	opts options
	p    *report.Printer
	cfg  *config.Config
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if cmd == "version" {
		if len(rest) != 0 {
			fmt.Fprintf(stderr, "debfactory: version no admite argumentos\n")
			return 2
		}
		fmt.Fprintln(stdout, version.Commit)
		return 0
	}
	var o options
	fs := flag.NewFlagSet("debfactory "+cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.root, "root", ".", "")
	fs.StringVar(&o.workdir, "workdir", defaultWorkdir(), "")
	fs.StringVar(&o.cache, "cache", defaultCache(), "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.StringVar(&o.data, "data", defaultData(), "")
	switch cmd {
	case "validate", "lint":
	case "lock":
		fs.BoolVar(&o.force, "force", false, "")
		fs.BoolVar(&o.prune, "prune", false, "")
	case "build":
		fs.StringVar(&o.arch, "arch", "", "")
		fs.StringVar(&o.suite, "suite", "", "")
		fs.BoolVar(&o.all, "all", false, "")
		fs.BoolVar(&o.reproducible, "check-reproducible", false, "")
		fs.IntVar(&o.jobs, "jobs", defaultJobs(), "")
		fs.StringVar(&o.out, "out", "", "")
	case "level2":
		fs.StringVar(&o.arch, "arch", "", "")
		fs.StringVar(&o.suite, "suite", "", "")
		fs.IntVar(&o.variant, "variant", -1, "")
		fs.StringVar(&o.source, "source", "", "")
		fs.StringVar(&o.out, "out", "", "")
		fs.BoolVar(&o.disk, "disk", false, "")
	case "test":
		fs.StringVar(&o.arch, "arch", "", "")
		fs.StringVar(&o.suite, "suite", "", "")
		fs.BoolVar(&o.all, "all", false, "")
		fs.BoolVar(&o.force, "force", false, "")
		fs.IntVar(&o.jobs, "jobs", defaultJobs(), "")
		fs.StringVar(&o.deb, "deb", "", "")
		fs.StringVar(&o.out, "out", "", "")
	case "publish":
		fs.BoolVar(&o.all, "all", false, "")
		fs.BoolVar(&o.dryRun, "dry-run", false, "")
	case "retire":
		fs.StringVar(&o.reason, "reason", "", "")
		fs.BoolVar(&o.last, "last", false, "")
		fs.BoolVar(&o.dryRun, "dry-run", false, "")
	case "repo":
		fs.StringVar(&o.dest, "dest", "", "")
		fs.BoolVar(&o.dryRun, "dry-run", false, "")
		fs.IntVar(&o.port, "port", 8080, "")
	case "site":
		fs.StringVar(&o.state, "state", "", "")
		fs.StringVar(&o.in, "in", "", "")
		fs.StringVar(&o.out, "out", "", "")
		fs.StringVar(&o.dir, "dir", "", "")
		fs.StringVar(&o.addr, "addr", "127.0.0.1:8080", "")
		fs.StringVar(&o.dest, "dest", "", "")
		fs.BoolVar(&o.dryRun, "dry-run", false, "")
	case "migrate":
	case "supply":
		fs.StringVar(&o.from, "from", "", "")
		fs.StringVar(&o.to, "to", "", "")
	case "verify":
		fs.StringVar(&o.arch, "arch", "", "")
		fs.StringVar(&o.suite, "suite", "", "")
		fs.StringVar(&o.site, "site", "https://apt.opentepache.org", "")
		fs.StringVar(&o.keyring, "keyring", "", "")
		fs.BoolVar(&o.noRebuild, "no-rebuild", false, "")
	case "ci":
		fs.StringVar(&o.envFile, "env-file", "", "")
		fs.StringVar(&o.artifacts, "artifacts", "", "")
		fs.BoolVar(&o.local, "local", false, "")
		fs.StringVar(&o.mode, "mode", "", "")
		fs.StringVar(&o.base, "base", "", "")
		fs.StringVar(&o.pkg, "package", "", "")
		fs.StringVar(&o.version, "version", "", "")
		fs.StringVar(&o.reason, "reason", "", "")
		fs.StringVar(&o.stage, "stage", "", "")
		fs.StringVar(&o.stageFile, "stage-file", "", "")
		fs.StringVar(&o.against, "against", "", "")
		fs.BoolVar(&o.last, "last", false, "")
		fs.BoolVar(&o.fix, "fix", false, "")
		fs.IntVar(&o.jobs, "jobs", defaultJobs(), "")
		fs.BoolVar(&o.force, "force", false, "")
		fs.BoolVar(&o.dryRun, "dry-run", false, "")
	default:
		fmt.Fprintf(stderr, "debfactory: orden desconocida %q\n\n%s", cmd, usage)
		return 2
	}
	pos, err := parseInterspersed(fs, rest)
	if err != nil {
		fmt.Fprintf(stderr, "debfactory: %v\n\n%s", err, usage)
		return 2
	}
	a := &app{opts: o, p: &report.Printer{JSON: o.json, Out: stdout, Err: stderr}}
	err = a.dispatch(ctx, cmd, pos)
	if err != nil && !errors.Is(err, errReported) {
		a.p.Fail(err)
	}
	return exitCode(err)
}

func exitCode(err error) int {
	var env *report.EnvError
	var u usageError
	var in *ci.InputError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &env):
		return 3
	case errors.As(err, &u), errors.As(err, &in):
		return 2
	}
	return 1
}

func (a *app) dispatch(ctx context.Context, cmd string, pos []string) error {
	root, err := filepath.Abs(a.opts.root)
	if err != nil {
		return usageError{err.Error()}
	}
	a.opts.root = root
	if a.opts.arch != "" && a.opts.arch != "amd64" && a.opts.arch != "arm64" {
		return usageError{fmt.Sprintf("--arch %q: se admite amd64 o arm64", a.opts.arch)}
	}
	if cmd == "site" && len(pos) > 0 && (pos[0] == "build" || pos[0] == "serve") {
		return a.siteNoConfig(ctx, pos[0], pos[1:])
	}
	if cmd == "verify" {
		return a.verify(ctx, pos)
	}
	names := pos
	switch cmd {
	case "lint", "repo", "ci", "site":
		names = nil
	case "level2", "supply":
		if len(pos) > 0 {
			names = pos[1:]
		}
	case "publish", "retire":
		if len(pos) > 0 {
			names = pos[:1] // el segundo argumento es una versión
		}
	}
	{
		for _, p := range names {
			if !pkgName.MatchString(p) {
				return usageError{fmt.Sprintf("nombre de paquete inválido %q", p)}
			}
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	a.cfg = cfg

	switch cmd {
	case "validate":
		return a.validate(pos)
	case "lock":
		if len(pos) != 1 {
			return usageError{"lock necesita exactamente un paquete"}
		}
		if a.opts.force && a.opts.prune {
			return usageError{"--force y --prune no se combinan"}
		}
		if a.opts.prune {
			return a.env().Prune(pos[0])
		}
		return a.env().Lock(ctx, pos[0], a.opts.force)
	case "build":
		return a.build(ctx, pos)
	case "level2":
		return a.level2(ctx, pos)
	case "supply":
		if len(pos) != 2 || pos[0] != "review" || a.opts.from == "" || a.opts.to == "" {
			return usageError{"supply necesita: review <pkg> --from V1 --to V2"}
		}
		md, err := a.env().SupplyReview(ctx, pos[1], a.opts.from, a.opts.to, os.Getenv("GITHUB_TOKEN"))
		if err != nil {
			return err
		}
		fmt.Fprint(a.p.Out, md)
		return nil
	case "lint":
		return a.lint(ctx, pos)
	case "test":
		return a.test(ctx, pos)
	case "publish":
		return a.publish(ctx, pos)
	case "retire":
		return a.retire(ctx, pos)
	case "repo":
		return a.repo(ctx, pos)
	case "ci":
		return a.ci(ctx, pos)
	case "site":
		return a.site(ctx, pos)
	case "migrate":
		if len(pos) != 1 || pos[0] != "layout" {
			return usageError{"migrate necesita: layout"}
		}
		return build.MigrateLayout(ctx, a.opts.root, a.p)
	}
	return usageError{"orden no implementada: " + cmd}
}

func (a *app) env() *build.Env {
	e := &build.Env{
		Root:    a.opts.root,
		Workdir: a.opts.workdir,
		Cfg:     a.cfg,
		Fetcher: fetch.New(a.opts.cache, fetch.DefaultTimeout, nil),
		GitHub:  &fetch.GitHub{Token: os.Getenv("GITHUB_TOKEN")},
		Out:     a.p,
		Scorecard: func(ctx context.Context, upstream string) *supply.Scorecard {
			return supply.FetchScorecard(ctx, nil, "", upstream)
		},
	}
	e.Defaults()
	return e
}

// build: `build <pkg> [--arch A] [--suite S] [--check-reproducible]` o `build --all`.
func (a *app) build(ctx context.Context, pos []string) error {
	o := build.Options{Arch: a.opts.arch, Suite: a.opts.suite, Reproducible: a.opts.reproducible, Jobs: a.opts.jobs}
	if a.opts.out != "" {
		if a.opts.all || len(pos) != 1 || a.opts.arch == "" {
			return usageError{"build --out necesita un paquete y --arch"}
		}
		if err := container.Preflight(ctx, []string{a.opts.arch}); err != nil {
			return err
		}
		e := a.env()
		out, err := filepath.Abs(a.opts.out)
		if err != nil {
			return usageError{err.Error()}
		}
		e.Dist = out
		return e.Build(ctx, pos[0], o)
	}
	switch {
	case a.opts.all && (len(pos) > 0 || a.opts.arch != "" || a.opts.suite != ""):
		return usageError{"build --all no admite paquetes, --arch ni --suite"}
	case !a.opts.all && len(pos) != 1:
		return usageError{"build necesita un paquete o --all"}
	}
	if err := container.Preflight(ctx, a.cfg.Arches); err != nil {
		return err
	}
	if a.opts.all {
		names, err := build.ListPackages(a.opts.root)
		if err != nil {
			return err
		}
		return a.env().BuildAll(ctx, names, o)
	}
	return a.env().Build(ctx, pos[0], o)
}

// lint: lintian sobre .deb ya construidos.
func (a *app) lint(ctx context.Context, debs []string) error {
	if len(debs) == 0 {
		return usageError{"lint necesita al menos un .deb"}
	}
	if err := container.Preflight(ctx, nil); err != nil {
		return err
	}
	failed := false
	for _, deb := range debs {
		pkgName, version, arch := debParts(deb)
		if _, err := os.Stat(deb); err != nil {
			a.p.Fail(&report.Error{Package: pkgName, Version: version, Arch: arch, Stage: report.Lintian, Cause: err.Error()})
			failed = true
			continue
		}
		res, err := lint.Run(ctx, a.cfg, deb)
		if err != nil {
			a.p.Fail(report.Wrap(err, report.Lintian, pkgName, version, arch))
			failed = true
			continue
		}
		for _, w := range res.Warnings {
			a.p.Result(pkgName, version, arch, "lintian-warning", w)
		}
		a.p.Result(pkgName, version, arch, "lintian", "pass")
	}
	if failed {
		return errReported
	}
	return nil
}

// test: `test <pkg> [--arch A] [--suite S]` o `test --all`.
// level2: modos de job hospedado del nivel 2 (specs/009).
func (a *app) level2(ctx context.Context, pos []string) error {
	if len(pos) != 2 || (pos[0] != "source" && pos[0] != "build") {
		return usageError{"level2 necesita: source <pkg> | build <pkg>"}
	}
	if a.opts.out == "" {
		return usageError{"level2 " + pos[0] + " necesita --out DIR"}
	}
	out, err := filepath.Abs(a.opts.out)
	if err != nil {
		return usageError{err.Error()}
	}
	e := a.env()
	if pos[0] == "source" {
		if err := container.Preflight(ctx, nil); err != nil {
			return err
		}
		return e.Level2Source(ctx, pos[1], out)
	}
	if a.opts.suite == "" || a.opts.arch == "" || a.opts.variant < 0 || a.opts.source == "" {
		return usageError{"level2 build necesita --suite, --arch, --variant y --source"}
	}
	src, err := filepath.Abs(a.opts.source)
	if err != nil {
		return usageError{err.Error()}
	}
	if err := container.Preflight(ctx, []string{a.opts.arch}); err != nil {
		return err
	}
	return e.Level2BuildOne(ctx, pos[1], a.opts.suite, a.opts.arch, a.opts.variant, src, out, build.Options{Disk: a.opts.disk})
}

func (a *app) test(ctx context.Context, pos []string) error {
	if a.opts.deb != "" || a.opts.out != "" {
		if len(pos) != 1 || a.opts.deb == "" || a.opts.out == "" || a.opts.suite == "" || a.opts.arch == "" {
			return usageError{"test --deb necesita un paquete, --suite, --arch, --deb y --out"}
		}
		if err := container.Preflight(ctx, []string{a.opts.arch}); err != nil {
			return err
		}
		m, err := manifest.Validate(a.opts.root, a.cfg, pos[0])
		if err != nil {
			return err
		}
		rep, err := installtest.TestDeb(ctx, a.cfg, m, a.opts.suite, a.opts.arch, a.opts.deb, a.opts.out)
		if rep != nil {
			rep.Print(a.p)
		}
		return err
	}
	switch {
	case a.opts.all && (len(pos) > 0 || a.opts.arch != "" || a.opts.suite != ""):
		return usageError{"test --all no admite paquetes, --arch ni --suite"}
	case !a.opts.all && len(pos) != 1:
		return usageError{"test necesita un paquete o --all"}
	}
	if err := container.Preflight(ctx, a.cfg.Arches); err != nil {
		return err
	}
	names := pos
	if a.opts.all {
		var err error
		if names, err = build.ListPackages(a.opts.root); err != nil {
			return err
		}
	}
	arches := a.cfg.Arches
	if a.opts.arch != "" {
		arches = []string{a.opts.arch}
	}
	var only []string
	if a.opts.suite != "" {
		only = []string{a.opts.suite}
	}
	// Cada paquete y arquitectura corre en su goroutine; el Limiter reparte
	// las suites entre todos y cada informe se imprime entero al terminar.
	type job struct {
		name, arch, status string
	}
	var jobs []*job
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := false
	lim := installtest.NewLimiter(a.opts.jobs)
	for _, name := range names {
		m, err := manifest.Validate(a.opts.root, a.cfg, name)
		if err != nil {
			a.p.Fail(err)
			failed = true
			jobs = append(jobs, &job{name: name, status: "fail (validate)"})
			continue
		}
		for _, arch := range arches {
			if !slices.Contains(m.Arches(a.cfg.Arches), arch) {
				continue // exclude.arches (specs/009)
			}
			j := &job{name: name, arch: arch, status: "pass"}
			jobs = append(jobs, j)
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, flush := a.p.Buffered()
				defer flush()
				o := installtest.Options{Only: only, Limiter: lim, Force: a.opts.force}
				if _, err := installtest.TestPackage(ctx, a.opts.root, a.cfg, m, arch, o, out); err != nil {
					out.Fail(err)
					mu.Lock()
					failed, j.status = true, "fail"
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	var summary []string
	for _, j := range jobs {
		if j.arch == "" {
			summary = append(summary, fmt.Sprintf("%-12s %s", j.name, j.status))
		} else {
			summary = append(summary, fmt.Sprintf("%-12s %-6s %s", j.name, j.arch, j.status))
		}
	}
	if a.opts.all {
		a.p.Line("\nresumen de test --all:")
		for _, l := range summary {
			a.p.Line("  %s", l)
		}
	}
	if failed {
		return errReported
	}
	return nil
}

// debParts separa <name>_<version>_<arch>.deb.
func debParts(deb string) (name, version, arch string) {
	parts := strings.Split(strings.TrimSuffix(filepath.Base(deb), ".deb"), "_")
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	return filepath.Base(deb), "", ""
}

var pkgName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)

// validate valida los manifiestos indicados o todos los de packages/.
func (a *app) validate(pkgs []string) error {
	if len(pkgs) == 0 {
		var err error
		if pkgs, err = build.ListPackages(a.opts.root); err != nil {
			return err
		}
	}
	failed := false
	for _, name := range pkgs {
		m, err := manifest.Validate(a.opts.root, a.cfg, name)
		if err != nil {
			a.p.Fail(err)
			failed = true
			continue
		}
		a.p.Result(m.Name, m.Version, "", "validate", "ok")
	}
	if failed {
		return errReported
	}
	return nil
}

// parseInterspersed permite mezclar opciones y argumentos posicionales
// ("build fnm --arch amd64").
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func defaultWorkdir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "debfactory")
	}
	return filepath.Join(os.TempDir(), "debfactory")
}

func defaultData() string {
	if d := os.Getenv("DEBFACTORY_DATA"); d != "" {
		return d
	}
	if d, err := os.UserHomeDir(); err == nil {
		return filepath.Join(d, ".local", "share", "debfactory")
	}
	return filepath.Join(os.TempDir(), "debfactory-data")
}

func defaultCache() string {
	if d := os.Getenv("DEBFACTORY_CACHE"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "debfactory")
	}
	return filepath.Join(os.TempDir(), "debfactory-cache")
}

// defaultJobs: tareas simultáneas de build --all y test. Cada una es un
// contenedor que casi siempre usa un solo hilo (arm64 va por qemu), así que
// se usa una por núcleo.
func defaultJobs() int { return runtime.NumCPU() }

// verify: comprobación de un paquete publicado por un tercero, sin
// credenciales ni repositorio local (specs/009, research D18).
func (a *app) verify(ctx context.Context, pos []string) error {
	if len(pos) != 2 || !pkgName.MatchString(pos[0]) {
		return usageError{"verify necesita un paquete y una versión"}
	}
	res, err := verify.Run(ctx, verify.Options{Site: a.opts.site, Package: pos[0], Version: pos[1],
		Suite: a.opts.suite, Arch: a.opts.arch, Keyring: a.opts.keyring, NoRebuild: a.opts.noRebuild,
		Workdir: a.opts.workdir})
	if err != nil {
		return err
	}
	for _, s := range res.Steps {
		a.p.Line("verify: ✓ %s", s)
	}
	if !res.Verified {
		return &report.Error{Package: pos[0], Version: pos[1], Stage: report.Verify, Cause: "no verificado: " + res.Reason}
	}
	a.p.Line("verify: %s %s verificado", pos[0], pos[1])
	return nil
}
