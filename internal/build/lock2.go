package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/fetch"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/srcbuild"
	"debfactory/internal/srcpkg"
	"debfactory/internal/supply"
)

// forManifest devuelve el Env con el fetcher que admite los orígenes extra
// del manifiesto (sitio de releases de upstream, toolchain; specs/009).
func (e *Env) forManifest(m *manifest.Manifest) *Env {
	hosts := m.ExtraHosts()
	if len(hosts) == 0 {
		return e
	}
	c := *e
	c.Fetcher = e.Fetcher.WithHosts(hosts...)
	return &c
}

// TagArchiveURL es el archivo del tag de git que genera GitHub.
func TagArchiveURL(m *manifest.Manifest) string {
	tag, _ := m.RenderedTag()
	return "https://github.com/" + m.Upstream + "/archive/refs/tags/" + tag + ".tar.gz"
}

// ZigFetcherFactory crea el ZigFetcher de lock y de la preparación del fuente
// (por defecto, zig fetch en un contenedor; las pruebas lo sustituyen).
type ZigFetcherFactory func(ctx context.Context, toolchain, cache string) (supply.ZigFetcher, func(), error)

func defaultZigFetcher(ctx context.Context, toolchain, cache string) (supply.ZigFetcher, func(), error) {
	f, err := supply.NewContainerZigFetcher(ctx, toolchain, cache)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

// lockLevel2 completa la entrada de un paquete de nivel 2 (specs/009): el
// tarball contra el tag de git (FR-014), el toolchain de cada arquitectura
// (FR-001) y las dependencias del lenguaje (FR-002, FR-003) con su
// componente determinista (research D3).
func (e *Env) lockLevel2(ctx context.Context, m *manifest.Manifest, entry *lock.Entry, source string,
	get func(url, arch string) (download, error)) error {
	fail := func(stage report.Stage, err error) error { return report.Wrap(err, stage, m.Name, m.Version, "") }
	// Tarball de release contra el tag (no aplica si el fuente es el propio
	// archivo del tag).
	if tagURL := TagArchiveURL(m); mustRender(m, m.Source.URL) != tagURL {
		d, err := get(tagURL, "")
		if err != nil {
			return err
		}
		diff, err := supply.CompareTag(source, m.Source.Format, d.path)
		if err != nil {
			return fail(report.Stage("tagdiff"), err)
		}
		var declared []string
		for _, x := range m.Source.ExtraFiles {
			declared = append(declared, x.Path)
		}
		if entry.ExtraFiles, err = diff.CheckExtras(declared, nil); err != nil {
			return fail(report.Stage("tagdiff"), err)
		}
		if len(entry.ExtraFiles) == 0 {
			entry.ExtraFiles = nil
		}
		entry.TagArchiveSHA256 = d.sha
	}
	if m.Toolchain == nil {
		return nil
	}
	v := &supply.Verifier{Root: e.Root, Pkg: m.Name, Sigstore: e.Sigstore}
	entry.Toolchain = map[string]string{}
	var hostTarball string
	for _, arch := range e.Cfg.Arches {
		r, err := supply.FetchToolchain(ctx, e.Fetcher, v, m.Toolchain, arch, "", "")
		if err != nil {
			return fail(report.Stage("toolchain"), err)
		}
		entry.Toolchain[arch] = r.SHA256
		if r.SigURL != "" {
			if entry.Signatures == nil {
				entry.Signatures = map[string]string{}
			}
			entry.Signatures[r.SigURL] = r.SigSHA
		}
		if arch == srcbuild.HostArch() {
			hostTarball = r.Tarball
		}
	}
	if m.Deps == nil {
		return nil
	}
	work, err := os.MkdirTemp(e.Workdir, m.Name+"-deps-")
	if err != nil {
		return fail(report.Lock, err)
	}
	defer os.RemoveAll(work)
	// lock siempre baja todo (no hay lockfile contra el que comparar una
	// copia guardada), pero guarda lo verificado para los builds.
	deps, sha, err := e.resolveDeps(ctx, m, source, hostTarball, work, time.Unix(entry.SourceDateEpoch, 0).UTC(), nil)
	if err != nil {
		return err
	}
	entry.Deps, entry.ZigdepsSHA256 = deps, sha
	return nil
}

// resolveDeps extrae el fuente y el toolchain del host en work, resuelve las
// dependencias de Zig, pasa el gate de licencias de cada una (FR-027) y
// construye el componente zigdeps. Devuelve las dependencias, el sha256 del
// componente y deja el componente en work/zigdeps.tar.xz.
//
// expected (dependencias del lockfile) permite reutilizar las copias ya
// verificadas cuyo árbol da exactamente el sha256 registrado.
func (e *Env) resolveDeps(ctx context.Context, m *manifest.Manifest, source, toolchainTarball, work string, mtime time.Time,
	expected []lock.Dep) ([]lock.Dep, string, error) {
	fail := func(stage report.Stage, err error) error { return report.Wrap(err, stage, m.Name, m.Version, "") }
	src, tc, deps := filepath.Join(work, "src"), filepath.Join(work, "toolchain"), filepath.Join(work, "deps")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return nil, "", fail(report.Lock, err)
	}
	if out, err := exec.CommandContext(ctx, "tar", "-xf", source, "-C", src, "--strip-components=1",
		"--no-same-owner").CombinedOutput(); err != nil {
		return nil, "", fail(report.Lock, fmt.Errorf("extraer el fuente: %v: %s", err, out))
	}
	if err := supply.ExtractToolchain(ctx, toolchainTarball, tc); err != nil {
		return nil, "", fail(report.Stage("toolchain"), err)
	}
	factory := e.ZigFetcher
	if factory == nil {
		factory = defaultZigFetcher
	}
	f, closeFn, err := factory(ctx, tc, filepath.Join(work, "zigcache"))
	if err != nil {
		return nil, "", fail(report.Stage("deps"), err)
	}
	defer closeFn()
	var store *supply.DepStore
	if e.Fetcher != nil && e.Fetcher.Cache != "" {
		store = &supply.DepStore{Dir: filepath.Join(e.Fetcher.Cache, "zigdeps")}
		if len(expected) > 0 {
			store.Expected = map[string]string{}
			for _, d := range expected {
				store.Expected[d.Hash] = d.SHA256
			}
		}
	}
	list, err := supply.ResolveZigDeps(ctx, src, m.Deps.ExcludePaths, m.Deps.Skip, f, deps, store)
	if err != nil {
		return nil, "", fail(report.Stage("deps"), err)
	}
	if err := e.depsLicenses(m, deps, list); err != nil {
		return nil, "", fail(report.License, err)
	}
	comp := filepath.Join(work, "zigdeps.tar.xz")
	if err := srcpkg.BuildComponent(ctx, deps, "zigdeps", mtime, comp); err != nil {
		return nil, "", fail(report.Source, err)
	}
	sha, err := fetchSHA(comp)
	if err != nil {
		return nil, "", fail(report.Source, err)
	}
	return list, sha, nil
}

// mustRender renderiza una plantilla del manifiesto sin arquitectura.
func mustRender(m *manifest.Manifest, tmpl string) string {
	u, _ := m.RenderURL(tmpl, "")
	return u
}

// toolchainFile es el contenido de debian/upstream/toolchain (research D3).
func toolchainFile(m *manifest.Manifest, entry *lock.Entry, arches []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\nversion: %s\n", m.Toolchain.Name, m.Toolchain.Version)
	for _, a := range arches {
		u, _ := m.Toolchain.RenderURL(m.Toolchain.URL, a)
		fmt.Fprintf(&b, "%s: {url: %s, sha256: %s}\n", a, u, entry.Toolchain[a])
	}
	return b.String()
}

// fetchSHA es el sha256 de un archivo.
func fetchSHA(p string) (string, error) { return fetch.SHA256File(p) }
