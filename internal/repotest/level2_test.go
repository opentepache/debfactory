//go:build e2e

package repotest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// level2Fixture es el paquete de prueba de nivel 2 (specs/008-source-builds,
// research D14).
var level2Fixture = filepath.Join(realRoot, "testdata", "fixtures", "level2")

// level2Suites son las suites que el manifiesto de prueba no excluye, con
// la dependencia de OpenSSL que debe tener su binario (SC-001).
var level2Suites = map[string]string{"bookworm": "libssl3 ", "trixie": "libssl3t64 ", "noble": "libssl3t64 "}

// level2Env prepara una raíz con la configuración de testdata/project y solo
// el paquete de prueba, y un "GitHub" local que sirve su tarball.
func level2Env(t *testing.T) *build.Env { return level2EnvFrom(t, level2Fixture) }

// level2EnvFrom es level2Env con otro fixture (el tarball de tepache-hello
// sale siempre de level2Fixture). Solo github.com va al servidor local; el
// resto (p. ej. ziglang.org para el toolchain) va a la red real.
func level2EnvFrom(t *testing.T, fixture string) *build.Env {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"config", "docs"} {
		copyDir(t, filepath.Join(testutil.ProjectRoot(), d), filepath.Join(root, d))
	}
	pk := filepath.Join(root, "packages")
	copyDir(t, fixture, pk)
	os.RemoveAll(filepath.Join(pk, "src"))
	os.Remove(filepath.Join(pk, "tepache-hello-1.0.0.tar.gz"))
	tarball, err := os.ReadFile(filepath.Join(level2Fixture, "tepache-hello-1.0.0.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(pk, "tepache-hello", "debian", "rules"), 0o755)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "github.com" && r.URL.Path == "/opentepache/tepache-hello/archive/refs/tags/v1.0.0.tar.gz" {
			w.Write(tarball)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	w := captureWriter{testWriter{t}}
	e := &build.Env{Root: root, Workdir: t.TempDir(), Cfg: cfg,
		Fetcher: fetch.New(filepath.Join(os.TempDir(), "debfactory-e2e-l2-cache"), 10*time.Minute, githubOnly{testutil.Redirect{Server: srv}}),
		Out:     &report.Printer{Out: w, Err: w}}
	e.Defaults()
	return e
}

// githubOnly manda github.com al servidor local y lo demás a la red.
type githubOnly struct{ local http.RoundTripper }

func (g githubOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "github.com" {
		return g.local.RoundTrip(r)
	}
	return http.DefaultTransport.RoundTrip(r)
}

// TestE2ELevel2 recorre el nivel 2 de punta a punta con contenedores reales
// (specs/008-source-builds):
//   - US1, SC-001: tepache-hello se compila por separado para cada suite y
//     arquitectura, dos veces con el mismo resultado; cada binario depende
//     del OpenSSL de su suite y pasa la prueba de instalación y de ciclo de
//     vida en su suite;
//   - US2, SC-002: se publica con su paquete fuente; en un contenedor limpio
//     de cada suite y arquitectura, apt-get install instala la versión con el
//     sufijo de la suite y el binario funciona;
//   - US3, SC-003: siguiendo docs/recompilar-nivel2.md (apt-get source,
//     build-dep, dpkg-buildpackage) se obtiene el mismo .deb que el publicado.
func TestE2ELevel2(t *testing.T) {
	ctx := context.Background()
	e := level2Env(t)
	start := time.Now()
	if err := e.Build(ctx, "tepache-hello", build.Options{}); err != nil {
		t.Fatal(err)
	}
	built := time.Since(start)
	m, err := manifest.Validate(e.Root, e.Cfg, "tepache-hello")
	if err != nil {
		t.Fatal(err)
	}
	suites := build.Level2Suites(e.Cfg, m)
	for _, s := range suites {
		dep, ok := level2Suites[s.Name]
		if !ok {
			t.Fatalf("suite inesperada %s", s.Name)
		}
		for _, arch := range e.Cfg.Arches {
			deb := filepath.Join(e.Root, "dist", build.Level2Base(m, s, arch)+".deb")
			out, err := exec.Command("dpkg-deb", "-f", deb, "Depends", "Version").CombinedOutput()
			if err != nil {
				t.Fatalf("dpkg-deb -f %s: %v\n%s", deb, err, out)
			}
			if !strings.Contains(string(out), dep) || !strings.Contains(string(out), m.SuiteVersion(s.Level2Suffix)) {
				t.Errorf("%s/%s: se esperaba %q y la versión con sufijo:\n%s", s.Name, arch, dep, out)
			}
		}
	}
	lim := installtest.NewLimiter(4)
	for _, arch := range e.Cfg.Arches {
		if _, err := installtest.TestPackage(ctx, e.Root, e.Cfg, m, arch, installtest.Options{Limiter: lim}, e.Out); err != nil {
			t.Fatal(err)
		}
	}
	tested := time.Since(start)

	// US2: publicar en un repositorio de prueba y servirlo.
	rcfg := *e.Cfg
	home, master, sub := testutil.TestKey(t)
	rcfg.Repo.Signing.Masters, rcfg.Repo.Signing.Subkey = []string{master}, sub
	data := t.TempDir()
	if err := os.Symlink(home, filepath.Join(data, "gnupg")); err != nil {
		t.Fatal(err)
	}
	re := &repo.Env{Root: e.Root, DataDir: data, Cfg: &rcfg, Out: e.Out}
	if err := re.Publish(ctx, []string{"tepache-hello"}, nil, false); err != nil {
		t.Fatal(err)
	}
	serveCurrent(t, ctx, re)
	user := guide(t, re)
	rebuild, err := Blocks(filepath.Join(e.Root, "docs", "recompilar-nivel2.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"srcadd", "source", "builddeps", "toolchain", "rebuild", "compare"} {
		if rebuild[step] == "" {
			t.Fatalf("docs/recompilar-nivel2.md sin el bloque %s", step)
		}
	}

	sem := make(chan struct{}, 4)
	t.Run("matriz", func(t *testing.T) {
		for _, s := range suites {
			for _, arch := range e.Cfg.Arches {
				t.Run(s.Name+"-"+arch, func(t *testing.T) {
					t.Parallel()
					sem <- struct{}{}
					defer func() { <-sem }()
					t0 := time.Now()
					c, err := StartClient(ctx, s, arch)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					addRepo(t, ctx, c, re, user)
					run := func(step, block string) string {
						t.Helper()
						out, err := c.Script(ctx, Render(block, rcfg.Repo.URL, TestURL, nil))
						if err != nil {
							t.Fatalf("%s: %v", step, err)
						}
						return out
					}
					run("install", "apt-get install -y tepache-hello")
					got, err := c.Installed(ctx, "tepache-hello")
					if want := m.SuiteVersion(s.Level2Suffix); err != nil || got["tepache-hello"] != want {
						t.Fatalf("instalada %q, se esperaba %s (%v)", got["tepache-hello"], want, err)
					}
					if out := run("run", "tepache-hello"); !strings.Contains(out, "OpenSSL") {
						t.Errorf("tepache-hello: %s", out)
					}
					// US3: la guía de recompilación, tal cual.
					for _, step := range []string{"srcadd", "source", "builddeps", "toolchain", "rebuild"} {
						run(step, rebuild[step])
					}
					if out := run("compare", rebuild["compare"]); !strings.Contains(out, "idéntico al publicado") {
						t.Errorf("la recompilación no dio el mismo .deb:\n%s", out)
					}
					t.Logf("%s/%s: instalar y recompilar %s", s.Name, arch, time.Since(t0).Round(time.Second))
				})
			}
		}
	})
	t.Logf("compilación (3 suites × 2 arquitecturas × 2): %s; con pruebas: %s; total: %s",
		built.Round(time.Second), tested.Round(time.Second), time.Since(start).Round(time.Second))
}
