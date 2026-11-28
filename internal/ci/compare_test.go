package ci

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/build"
	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/testutil"
)

// TestMatrixCompare (specs/009): un PR que cambia la herramienta reconstruye
// los paquetes publicados (plan.Compare) en la matriz, sin probarlos.
func TestMatrixCompare(t *testing.T) {
	cfg := testConfig(t)
	root := fixtureRoot(t)
	p := &Plan{Compare: []PkgVersion{{Package: "buf", Version: "1.75.0-0~tepache1"}}}
	mx, err := BuildMatrix(root, cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(mx.Compare, ",") != "buf" || len(mx.L1Build) != len(cfg.Arches) || len(mx.Test) != 0 {
		t.Fatalf("matriz = %+v", mx)
	}
}

// TestArtifactShas: sha256 de los artefactos con las claves de distShas;
// variantes distintas o un .deb que no es el de build.json se rechazan.
func TestArtifactShas(t *testing.T) {
	cfg := testConfig(t)
	root := fixtureRoot(t)
	l2 := filepath.Join(testutil.RepoRoot(), "testdata", "fixtures", "level2")
	for _, n := range []string{"tepache-hello.yaml", "tepache-hello.lock.yaml", "tepache-hello"} {
		if out, err := exec.Command("cp", "-a", filepath.Join(l2, n), filepath.Join(root, "packages")).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	dir := t.TempDir()
	put := func(rel string, b []byte) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755)
		os.WriteFile(filepath.Join(dir, rel), b, 0o644)
	}
	// Nivel 1.
	buf, _ := manifest.Validate(root, cfg, "buf")
	put(filepath.Join(l1Artifact("buf", "amd64"), buf.DebBase("amd64")+".deb"), []byte("deb buf"))
	got, miss := artifactShas(dir, buf, nil, []string{"amd64"})
	if len(miss) != 0 || got["amd64"] != fetch.SHA256([]byte("deb buf")) {
		t.Fatalf("nivel 1: %v %v", got, miss)
	}
	if _, miss := artifactShas(dir, buf, nil, []string{"amd64", "arm64"}); len(miss) != 1 {
		t.Errorf("falta arm64: %v", miss)
	}
	// Nivel 2.
	th, _ := manifest.Validate(root, cfg, "tepache-hello")
	sr := build.SourceResult{Suites: map[string]build.SourceSuite{"trixie": {DSC: "x.dsc", DSCSHA256: "d5c"}}}
	b, _ := json.Marshal(sr)
	put(filepath.Join(sourceArtifact("tepache-hello"), "source.json"), b)
	write := func(v int, deb string) {
		art := buildArtifact("tepache-hello", "trixie", "amd64", v)
		hb := build.HostedBuild{Deb: "t.deb", SHA256: fetch.SHA256([]byte(deb))}
		put(filepath.Join(art, "t.deb"), []byte(deb))
		b, _ := json.Marshal(hb)
		put(filepath.Join(art, "build.json"), b)
	}
	write(0, "deb t")
	write(1, "deb t")
	got, miss = artifactShas(dir, th, []string{"trixie"}, []string{"amd64"})
	if len(miss) != 0 || got["trixie/dsc"] != "d5c" || got["trixie/amd64"] != fetch.SHA256([]byte("deb t")) {
		t.Fatalf("nivel 2: %v %v", got, miss)
	}
	write(1, "otro")
	if _, miss := artifactShas(dir, th, []string{"trixie"}, []string{"amd64"}); len(miss) != 1 || !strings.Contains(miss[0].cause, "no reproducible") {
		t.Errorf("variantes distintas: %+v", miss)
	}
	put(filepath.Join(buildArtifact("tepache-hello", "trixie", "amd64", 1), "t.deb"), []byte("alterado"))
	if _, miss := artifactShas(dir, th, []string{"trixie"}, []string{"amd64"}); len(miss) != 1 || !strings.Contains(miss[0].cause, "no es el de build.json") {
		t.Errorf("deb alterado: %+v", miss)
	}
}
