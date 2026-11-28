package ci

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/manifest"
	"debfactory/internal/testutil"
)

// TestBuildMatrix (specs/009, T024): nivel 1 por arquitectura, nivel 2 por
// suite × arquitectura × variante con un job de fuente, pruebas por suite ×
// arquitectura, runners estándar, exclusiones y ampliaciones.
func TestBuildMatrix(t *testing.T) {
	cfg := testConfig(t)
	root := fixtureRoot(t)
	l2 := filepath.Join(testutil.RepoRoot(), "testdata", "fixtures", "level2")
	for _, f := range []string{"tepache-hello.yaml", "tepache-hello.lock.yaml", "tepache-hello"} {
		if out, err := exec.Command("cp", "-a", filepath.Join(l2, f), filepath.Join(root, "packages")).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	editFile(t, filepath.Join(root, "packages/buf/buf.yaml"), "upstream:", "exclude:\n  arches:\n    arm64: no cabe en el runner estandar\nupstream:")
	p := &Plan{
		Publish: []AffectedPackage{{Package: "buf", Version: "1.75.0-0~tepache1"}, {Package: "tepache-hello", Version: "1.0.0-0~tepache1"}},
		Extend:  []AffectedPackage{{Package: "aqua", Version: "2.66.1-0~tepache1", Reason: Extend, Suites: []string{"forky"}}},
	}
	mx, err := BuildMatrix(root, cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	th, err := manifest.Validate(root, cfg, "tepache-hello")
	if err != nil {
		t.Fatal(err)
	}
	l2Suites := 0 // suites de la configuración que tepache-hello no excluye
	for _, s := range cfg.Suites {
		if _, ex := th.Exclude.Suites[s.Name]; !ex {
			l2Suites++
		}
	}
	if len(mx.L1Build) != 1 || mx.L1Build[0].Arch != "amd64" || mx.L1Build[0].Runner != "ubuntu-24.04" {
		t.Errorf("l1_build = %+v (buf excluye arm64; aqua es una ampliación y va por la PC)", mx.L1Build)
	}
	if len(mx.L2Source) != 1 || mx.L2Source[0].Package != "tepache-hello" {
		t.Errorf("l2_source = %+v", mx.L2Source)
	}
	if len(mx.L2Build) != l2Suites*2*2 {
		t.Errorf("l2_build = %d, se esperaban %d", len(mx.L2Build), l2Suites*2*2)
	}
	variants := map[string]int{}
	for _, e := range mx.L2Build {
		if e.Variant == nil || (e.Arch == "arm64") != (e.Runner == "ubuntu-24.04-arm") {
			t.Errorf("l2_build %+v", e)
			continue
		}
		variants[e.Suite+"/"+e.Arch] += 1 << *e.Variant
	}
	for k, v := range variants {
		if v != 3 {
			t.Errorf("%s: faltan variantes (%d)", k, v)
		}
	}
	tests := map[string]int{}
	for _, e := range mx.Test {
		tests[e.Package]++
		if e.Package == "buf" && e.Arch == "arm64" {
			t.Errorf("se prueba una arquitectura excluida: %+v", e)
		}
		if e.Package == "aqua" {
			t.Errorf("las ampliaciones se validan en la PC, no en la matriz: %+v", e)
		}
	}
	if tests["aqua"] != 0 || tests["buf"] != len(cfg.Suites) || tests["tepache-hello"] != l2Suites*2 {
		t.Errorf("test = %v", tests)
	}
	var out bytes.Buffer
	if err := writeMatrix(&out, mx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), `packages=["buf","tepache-hello"]`) || !strings.Contains(out.String(), "\nl2_source=[{") {
		t.Errorf("salida:\n%s", out.String())
	}
	empty, _ := BuildMatrix(root, cfg, &Plan{})
	out.Reset()
	writeMatrix(&out, empty)
	if !strings.Contains(out.String(), "l1_build=[]\n") {
		t.Errorf("listas vacías:\n%s", out.String())
	}
}
