package ci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"debfactory/internal/lint"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/supply"
	"debfactory/internal/testutil"
)

// acceptFixture: un repositorio con config de prueba, artefactos simulados de
// un run y ganchos de atestación y de prueba de humo.
type acceptFixture struct {
	t        *testing.T
	root     string
	art      string
	cfg      *config.Config
	r        *Runner
	rejected map[string]bool // archivos cuya atestación no verifica
	smoked   []string
}

var testRun = supply.Run{Repo: "opentepache/packages", Workflow: ".github/workflows/main.yml", Ref: "refs/heads/main", Commit: "c0ffee"}

func newAcceptFixture(t *testing.T) *acceptFixture {
	t.Helper()
	f := &acceptFixture{t: t, root: t.TempDir(), art: t.TempDir(), rejected: map[string]bool{}}
	if out, err := exec.Command("cp", "-a", filepath.Join(testutil.ProjectRoot(), "config"), f.root).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	os.MkdirAll(filepath.Join(f.root, "packages"), 0o755)
	ok := filepath.Join(testutil.RepoRoot(), "testdata", "fixtures", "ok")
	files := map[string][]byte{}
	for _, token := range []string{"x86_64", "aarch64"} {
		b, err := os.ReadFile(filepath.Join(ok, "example-"+token+".tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		files["/example/example/releases/download/v1.0.0/example-"+token+".tar.gz"] = b
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	cfg, err := config.Load(f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = cfg
	cache := t.TempDir()
	var out bytes.Buffer
	f.r = &Runner{Root: f.root, Cfg: cfg, Out: &report.Printer{Out: &out, Err: &out}, Workdir: t.TempDir(),
		Build: func() *build.Env {
			return &build.Env{Root: f.root, Workdir: t.TempDir(), Cfg: cfg,
				Fetcher: fetch.New(cache, time.Minute, testutil.Redirect{Server: srv}),
				GitHub:  &fetch.GitHub{Client: srv.Client(), API: srv.URL},
				Out:     &report.Printer{Out: &out, Err: &out},
				Lint:    func(context.Context, *config.Config, string) (*lint.Result, error) { return &lint.Result{}, nil },
			}
		}}
	return f
}

func (f *acceptFixture) accept() *Accept {
	return &Accept{Dir: f.art, Run: testRun,
		Attest: func(_ context.Context, run supply.Run, file, _ string) error {
			if run != testRun {
				f.t.Errorf("run = %+v", run)
			}
			if f.rejected[filepath.Base(file)] {
				return &supply.AttestError{File: file, Reason: "otro workflow"}
			}
			return nil
		},
		Smoke: func(_ context.Context, s config.Suite, d installtest.Deb) installtest.Result {
			f.smoked = append(f.smoked, s.Name+"/"+d.Arch)
			return installtest.Result{Suite: s.Name, Status: "pass"}
		}}
}

func (f *acceptFixture) write(rel string, data []byte) {
	p := filepath.Join(f.art, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *acceptFixture) testJSON(pkg, suite, arch, debSHA string) {
	rep := installtest.Report{Package: pkg, Arch: arch, DebSHA256: debSHA, Versions: "pass", Overall: "pass",
		Results: []installtest.Result{{Suite: suite, Status: "pass"}}}
	b, _ := json.Marshal(rep)
	f.write(filepath.Join(testArtifact(pkg, suite, arch), "test.json"), b)
}

// TestAcceptLevel1 (T029): reconstrucción en la PC con el mismo sha256,
// informes de cada suite fusionados, prueba de humo amd64; un .deb alterado,
// una atestación ajena o un informe faltante no se aceptan.
func TestAcceptLevel1(t *testing.T) {
	ctx := context.Background()
	f := newAcceptFixture(t)
	ok := filepath.Join(testutil.RepoRoot(), "testdata", "fixtures", "ok")
	for _, n := range []string{"example.yaml", "example.lock.yaml"} {
		b, _ := os.ReadFile(filepath.Join(ok, n))
		os.WriteFile(filepath.Join(f.root, "packages", n), b, 0o644)
	}
	m, err := manifest.Validate(f.root, f.cfg, "example")
	if err != nil {
		t.Fatal(err)
	}
	// "GitHub": el mismo build, en el artefacto de cada arquitectura.
	debSHA := map[string]string{}
	for _, arch := range f.cfg.Arches {
		b := f.r.Build()
		b.Dist = filepath.Join(f.art, l1Artifact("example", arch))
		if err := b.Build(ctx, "example", build.Options{Arch: arch}); err != nil {
			t.Fatal(err)
		}
		debSHA[arch], _ = fetch.SHA256File(filepath.Join(b.Dist, m.DebBase(arch)+".deb"))
		for _, s := range f.cfg.Suites {
			f.testJSON("example", s.Name, arch, debSHA[arch])
		}
	}
	shas, err := f.r.AcceptPackage(ctx, f.root, "example", f.accept())
	if err != nil {
		t.Fatal(err)
	}
	if shas["amd64"] != debSHA["amd64"] || shas["arm64"] != debSHA["arm64"] {
		t.Errorf("shas = %v", shas)
	}
	var merged installtest.Report
	b, _ := os.ReadFile(filepath.Join(f.root, "dist", m.DebBase("amd64")+".test.json"))
	if json.Unmarshal(b, &merged) != nil || len(merged.Results) != len(f.cfg.Suites) || merged.Overall != "pass" {
		t.Errorf("test.json fusionado = %s", b)
	}
	if strings.Join(f.smoked, ",") != f.cfg.Suites[0].Name+"/amd64" {
		t.Errorf("prueba de humo = %v", f.smoked)
	}

	armDeb := filepath.Join(l1Artifact("example", "arm64"), m.DebBase("arm64")+".deb")
	good, _ := os.ReadFile(filepath.Join(f.art, armDeb))
	cases := []struct {
		name  string
		setup func()
		stage string
	}{
		{"deb alterado", func() { f.write(armDeb, []byte("otro .deb")) }, "compare"},
		{"atestación ajena", func() { f.rejected[m.DebBase("amd64")+".deb"] = true }, "attest"},
		{"falta un informe", func() {
			os.RemoveAll(filepath.Join(f.art, testArtifact("example", f.cfg.Suites[1].Name, "amd64")))
		}, "inputs"},
	}
	for _, c := range cases {
		os.RemoveAll(filepath.Join(f.root, "dist"))
		f.write(armDeb, good)
		f.rejected = map[string]bool{}
		c.setup()
		_, err := f.r.AcceptPackage(ctx, f.root, "example", f.accept())
		if err == nil || !strings.Contains(err.Error(), ": "+c.stage+":") {
			t.Errorf("%s: %v (se esperaba %s)", c.name, err, c.stage)
		}
	}
}

// TestAcceptLevel2 (T029): fuente fiel al lockfile, dos variantes idénticas
// por combinación, informes y prueba de humo; variantes distintas, un .orig
// ajeno o un informe de otro .deb no se aceptan.
func TestAcceptLevel2(t *testing.T) {
	ctx := context.Background()
	f := newAcceptFixture(t)
	l2 := filepath.Join(testutil.RepoRoot(), "testdata", "fixtures", "level2")
	for _, n := range []string{"tepache-hello.yaml", "tepache-hello.lock.yaml", "tepache-hello"} {
		if out, err := exec.Command("cp", "-a", filepath.Join(l2, n), filepath.Join(f.root, "packages")).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	m, err := manifest.Validate(f.root, f.cfg, "tepache-hello")
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(filepath.Join(l2, "tepache-hello-1.0.0.tar.gz"))
	sr := build.SourceResult{Package: m.Name, Version: m.Version, Suites: map[string]build.SourceSuite{}}
	debSHA := map[string]string{}
	for _, s := range build.Level2Suites(f.cfg, m) {
		v := m.SuiteVersion(s.Level2Suffix)
		dir := filepath.Join(sourceArtifact(m.Name), s.Name)
		dsc := []byte("dsc " + v)
		f.write(filepath.Join(dir, m.Name+"_"+v+".dsc"), dsc)
		f.write(filepath.Join(dir, m.Name+"_"+v+".debian.tar.xz"), []byte("debian "+v))
		f.write(filepath.Join(dir, m.OrigName()), orig)
		sr.Suites[s.Name] = build.SourceSuite{DSC: m.Name + "_" + v + ".dsc", DSCSHA256: fetch.SHA256(dsc)}
		for _, arch := range f.cfg.Arches {
			deb := []byte("deb " + v + " " + arch)
			debSHA[s.Name+"/"+arch] = fetch.SHA256(deb)
			for vi := range 2 {
				art := buildArtifact(m.Name, s.Name, arch, vi)
				hb := build.HostedBuild{Package: m.Name, Version: v, Suite: s.Name, Arch: arch, Variant: vi,
					Deb: fmt.Sprintf("%s_%s_%s.deb", m.Name, v, arch), Buildinfo: fmt.Sprintf("%s_%s_%s.buildinfo", m.Name, v, arch),
					SHA256: fetch.SHA256(deb), BuildinfoSHA256: fetch.SHA256([]byte("bi")), DSCSHA256: fetch.SHA256(dsc)}
				f.write(filepath.Join(art, hb.Deb), deb)
				f.write(filepath.Join(art, hb.Buildinfo), []byte("bi"))
				b, _ := json.Marshal(hb)
				f.write(filepath.Join(art, "build.json"), b)
			}
			f.testJSON(m.Name, s.Name, arch, fetch.SHA256(deb))
		}
	}
	b, _ := json.Marshal(sr)
	f.write(filepath.Join(sourceArtifact(m.Name), "source.json"), b)
	f.write(filepath.Join(sourceArtifact(m.Name), "supply.json"), []byte(`{"package":"tepache-hello"}`))

	shas, err := f.r.AcceptPackage(ctx, f.root, m.Name, f.accept())
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range debSHA {
		if shas[k] != v {
			t.Errorf("%s: %s != %s", k, shas[k], v)
		}
	}
	// Lo aceptado pasa las comprobaciones de publicación de la fase 008.
	if _, err := repo.Inputs(f.root, f.cfg, m.Name, m.DebVersion()); err != nil {
		t.Errorf("inputs de publicación: %v", err)
	}
	if len(f.smoked) != 1 {
		t.Errorf("prueba de humo = %v", f.smoked)
	}

	s0 := build.Level2Suites(f.cfg, m)[0]
	v0 := m.SuiteVersion(s0.Level2Suffix)
	cases := []struct {
		name  string
		setup func()
		stage string
	}{
		{"variantes distintas", func() {
			art := buildArtifact(m.Name, s0.Name, "arm64", 1)
			var hb build.HostedBuild
			b, _ := os.ReadFile(filepath.Join(f.art, art, "build.json"))
			json.Unmarshal(b, &hb)
			f.write(filepath.Join(art, hb.Deb), []byte("otro"))
			hb.SHA256 = fetch.SHA256([]byte("otro"))
			b, _ = json.Marshal(hb)
			f.write(filepath.Join(art, "build.json"), b)
		}, "compare"},
		{"orig ajeno", func() {
			f.write(filepath.Join(sourceArtifact(m.Name), s0.Name, m.OrigName()), []byte("otro tarball"))
		}, "verify"},
		{"informe de otro deb", func() { f.testJSON(m.Name, s0.Name, "amd64", strings.Repeat("0", 64)) }, "inputs"},
	}
	for _, c := range cases {
		os.RemoveAll(filepath.Join(f.root, "dist"))
		c.setup()
		_, err := f.r.AcceptPackage(ctx, f.root, m.Name, f.accept())
		if err == nil || !strings.Contains(err.Error(), ": "+c.stage+":") {
			t.Errorf("%s: %v (se esperaba %s)", c.name, err, c.stage)
		}
	}
	_ = v0
}
