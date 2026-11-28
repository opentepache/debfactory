package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/lint"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
	"debfactory/internal/srcbuild"
	"debfactory/internal/srcpkg"
)

const level2URL = "https://github.com/opentepache/tepache-hello/archive/refs/tags/v1.0.0.tar.gz"

// fakeLevel2 simula el contenedor de la suite: registra lo que se pidió y
// produce archivos deterministas (o distintos en la segunda compilación de
// una combinación, para simular que no es reproducible).
type fakeLevel2 struct {
	mu       sync.Mutex
	sources  []string // suites
	builds   []string // suite/arch/variante
	unstable string   // suite/arch cuya segunda compilación difiere
	broken   string   // suite/arch que no compila
}

func (f *fakeLevel2) source(_ context.Context, s config.Suite, in srcpkg.Input, work string) (*srcbuild.Source, error) {
	f.mu.Lock()
	f.sources = append(f.sources, s.Name)
	f.mu.Unlock()
	if in.Version != "1.0.0-0~tepache1+"+s.Level2Suffix || in.Epoch != 1767225600 {
		return nil, fmt.Errorf("entrada inesperada: %+v", in)
	}
	out := filepath.Join(work, "source")
	os.MkdirAll(out, 0o755)
	src := &srcbuild.Source{Dir: out, DSC: in.Name + "_" + in.Version + ".dsc",
		DebianTar: in.Name + "_" + in.Version + ".debian.tar.xz", Orig: in.OrigName}
	os.WriteFile(filepath.Join(out, src.DSC), []byte("dsc "+in.Version), 0o644)
	os.WriteFile(filepath.Join(out, src.DebianTar), []byte("debian "+in.Version), 0o644)
	data, err := os.ReadFile(in.Orig)
	if err != nil {
		return nil, err
	}
	os.WriteFile(filepath.Join(out, src.Orig), data, 0o644)
	return src, nil
}

func (f *fakeLevel2) build(_ context.Context, spec srcbuild.Spec, out string) (*srcbuild.Binary, error) {
	key := spec.Suite.Name + "/" + spec.Arch
	f.mu.Lock()
	f.builds = append(f.builds, fmt.Sprintf("%s/%d", key, spec.Variant))
	f.mu.Unlock()
	if key == f.broken {
		return nil, &srcbuild.Failure{What: "dpkg-buildpackage", Code: 2, Tail: []string{"make: *** error"}}
	}
	os.MkdirAll(out, 0o755)
	body := "deb " + spec.Version + " " + spec.Arch
	if key == f.unstable && spec.Variant == 1 {
		body += " (otra fecha)"
	}
	b := &srcbuild.Binary{
		Deb:       filepath.Join(out, fmt.Sprintf("%s_%s_%s.deb", spec.Package, spec.Version, spec.Arch)),
		Buildinfo: filepath.Join(out, fmt.Sprintf("%s_%s_%s.buildinfo", spec.Package, spec.Version, spec.Arch)),
	}
	os.WriteFile(b.Deb, []byte(body), 0o644)
	os.WriteFile(b.Buildinfo, []byte("buildinfo "+spec.Version), 0o644)
	return b, nil
}

// loadLevel2 instala el paquete de prueba de nivel 2 y simula sus contenedores.
func (h *harness) loadLevel2() *fakeLevel2 {
	h.t.Helper()
	dir := filepath.Join(repo, level2Fixture)
	if err := os.CopyFS(filepath.Join(h.root, "packages"), os.DirFS(dir)); err != nil {
		h.t.Fatal(err)
	}
	os.Chmod(filepath.Join(h.root, "packages/tepache-hello/debian/rules"), 0o755)
	os.RemoveAll(filepath.Join(h.root, "packages/src"))
	os.Remove(filepath.Join(h.root, "packages/tepache-hello-1.0.0.tar.gz"))
	data, err := os.ReadFile(filepath.Join(dir, "tepache-hello-1.0.0.tar.gz"))
	if err != nil {
		h.t.Fatal(err)
	}
	h.serve(level2URL, data)
	f := &fakeLevel2{}
	h.env.SourceGen, h.env.BinaryBuild = f.source, f.build
	return f
}

// TestBuildLevel2: fuente por suite, doble compilación por suite y
// arquitectura, lintian sobre .dsc y .deb, y todo en dist/ (contracts/build.md).
func TestBuildLevel2(t *testing.T) {
	h := newHarness(t)
	f := h.loadLevel2()
	if err := h.env.Build(context.Background(), "tepache-hello", Options{}); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	suites := []string{"bookworm", "trixie", "noble"} // forky, sid y resolute excluidas
	if !slices.Equal(f.sources, suites) {
		t.Errorf("fuentes generadas = %v", f.sources)
	}
	if len(f.builds) != len(suites)*2*2 {
		t.Errorf("compilaciones = %d (%v)", len(f.builds), f.builds)
	}
	want := []string{"tepache-hello_1.0.0.orig.tar.gz"}
	suffix := map[string]string{"bookworm": "deb12", "trixie": "deb13", "noble": "u2404"}
	for _, s := range suites {
		v := "1.0.0-0~tepache1+" + suffix[s]
		want = append(want, "tepache-hello_"+v+".dsc", "tepache-hello_"+v+".debian.tar.xz")
		for _, a := range []string{"amd64", "arm64"} {
			base := "tepache-hello_" + v + "_" + a
			want = append(want, base+".deb", base+".buildinfo", base+".build.json")
			bi, err := pkg.ReadBuildInfo(filepath.Join(h.root, "dist", base+".build.json"))
			if err != nil {
				t.Fatal(err)
			}
			if bi.Suite != s || !bi.Reproducible || bi.Lintian != "pass" || bi.DSCSHA256 == "" ||
				bi.BuildinfoSHA256 == "" || bi.TestCommand != "tepache-hello" {
				t.Errorf("%s: build.json = %+v", base, bi)
			}
		}
	}
	slices.Sort(want)
	got := h.distFiles()
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("dist/ =\n%v\nwant\n%v", got, want)
	}
	dscs := 0
	for _, l := range h.linted {
		if strings.HasSuffix(l, ".dsc") {
			dscs++
		}
	}
	if dscs != 3 || len(h.linted) != 3+6 {
		t.Errorf("lintian: %v", h.linted)
	}
	// El .orig de dist/ es el tarball de upstream byte a byte (FR-010).
	a, _ := os.ReadFile(filepath.Join(repo, level2Fixture, "tepache-hello-1.0.0.tar.gz"))
	b, _ := os.ReadFile(filepath.Join(h.root, "dist", "tepache-hello_1.0.0.orig.tar.gz"))
	if string(a) != string(b) {
		t.Error("el .orig no es el tarball de upstream")
	}
}

// TestBuildLevel2Suite: --suite y --arch acotan la compilación.
func TestBuildLevel2Suite(t *testing.T) {
	h := newHarness(t)
	f := h.loadLevel2()
	if err := h.env.Build(context.Background(), "tepache-hello", Options{Suite: "trixie", Arch: "arm64"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.sources, []string{"trixie"}) || !slices.Equal(f.builds, []string{"trixie/arm64/0", "trixie/arm64/1"}) {
		t.Errorf("fuentes %v, compilaciones %v", f.sources, f.builds)
	}
	err := h.env.Build(context.Background(), "tepache-hello", Options{Suite: "sid"})
	if err == nil || !strings.Contains(err.Error(), "excluye") {
		t.Errorf("suite excluida: error = %v", err)
	}
}

// TestBuildLevel2Failures: no reproducible, fallo de compilación y tarball
// alterado; en ningún caso llega nada a dist/ (constitución VII).
func TestBuildLevel2Failures(t *testing.T) {
	cases := map[string]struct {
		setup func(h *harness, f *fakeLevel2)
		stage string
	}{
		"no reproducible":  {func(h *harness, f *fakeLevel2) { f.unstable = "trixie/arm64" }, "compare"},
		"no compila":       {func(h *harness, f *fakeLevel2) { f.broken = "noble/amd64" }, "build"},
		"tarball alterado": {func(h *harness, f *fakeLevel2) { h.serve(level2URL, []byte("otra cosa")) }, "verify"},
		// lintian del .dsc corre a la vez que las compilaciones: si falla,
		// las cancela y nada llega a dist/.
		"lintian del fuente": {func(h *harness, f *fakeLevel2) {
			ok := h.env.Lint
			h.env.Lint = func(ctx context.Context, cfg *config.Config, p string) (*lint.Result, error) {
				if strings.HasSuffix(p, ".dsc") && strings.Contains(p, "+deb13") {
					return nil, &report.Error{Stage: report.Lintian, Cause: "E: fuente roto"}
				}
				return ok(ctx, cfg, p)
			}
		}, "lintian"},
	}
	for _, parallel := range []bool{false, true} {
		for name, c := range cases {
			t.Run(fmt.Sprintf("%s/paralelo=%v", name, parallel), func(t *testing.T) {
				h := newHarness(t)
				f := h.loadLevel2()
				c.setup(h, f)
				err := h.env.Build(context.Background(), "tepache-hello", Options{Parallel: parallel})
				if err == nil || !strings.Contains(err.Error(), ": "+c.stage+":") {
					t.Fatalf("error = %v (se esperaba la etapa %s)", err, c.stage)
				}
				if d := h.distFiles(); len(d) != 0 {
					t.Errorf("dist/ no está vacío: %v", d)
				}
				if c.stage == "verify" && len(f.sources) > 0 {
					t.Error("se generó el fuente con un tarball sin verificar")
				}
				if c.stage == "build" {
					var errs interface{ Unwrap() []error }
					if !errors.As(err, &errs) && !strings.Contains(err.Error(), "make: *** error") {
						t.Errorf("sin el final del log: %v", err)
					}
				}
			})
		}
	}
}
