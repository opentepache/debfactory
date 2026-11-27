package build

import (
	"os"
	"path/filepath"
	"testing"

	"debfactory/internal/fetch"
	"debfactory/internal/lock"
	"debfactory/internal/testutil"
)

// Level2Fixture es el paquete de prueba de nivel 2 (specs/008-source-builds,
// research D14). Vive fuera de testdata/project para que los paquetes reales
// no cambien (SC-004); cada prueba lo instala en su raíz temporal.
const level2Fixture = "testdata/fixtures/level2"

// level2Sources son los archivos del "upstream" de tepache-hello, en el orden
// del tarball.
var level2Sources = []string{"LICENSE", "Makefile", "hello.c"}

// level2Tarball es el tarball del tag v1.0.0 de tepache-hello, determinista.
func level2Tarball(t *testing.T) []byte {
	t.Helper()
	dir := filepath.Join(repo, level2Fixture, "src", "tepache-hello-1.0.0")
	entries := []testutil.Entry{{Name: "tepache-hello-1.0.0/", Dir: true}}
	for _, f := range level2Sources {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, testutil.Entry{Name: "tepache-hello-1.0.0/" + f, Body: data})
	}
	return testutil.TarGz(entries)
}

// TestFixturesLevel2 mantiene al día el LICENSE, el tarball y el lockfile del
// paquete de prueba. Regenerar:
// go test ./internal/build -run TestFixturesLevel2 -update
func TestFixturesLevel2(t *testing.T) {
	dir := filepath.Join(repo, level2Fixture)
	license := filepath.Join(dir, "src", "tepache-hello-1.0.0", "LICENSE")
	if *update {
		if err := os.WriteFile(license, []byte(testutil.MITText), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tarball := level2Tarball(t)
	entry := &lock.Entry{SourceDateEpoch: 1767225600, LicenseSHA256: fetch.SHA256([]byte(testutil.MITText)),
		SourceSHA256: fetch.SHA256(tarball)}
	f := &lock.File{Schema: 1, Versions: map[string]*lock.Entry{"1.0.0": entry}}
	tmp := t.TempDir()
	os.Mkdir(filepath.Join(tmp, "packages"), 0o755)
	if err := f.Write(tmp, "tepache-hello"); err != nil {
		t.Fatal(err)
	}
	lockData, err := os.ReadFile(filepath.Join(tmp, lock.Path("tepache-hello")))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{
		"tepache-hello.lock.yaml":         lockData,
		"tepache-hello-1.0.0.tar.gz":      tarball,
		"src/tepache-hello-1.0.0/LICENSE": []byte(testutil.MITText),
	}
	for name, data := range want {
		p := filepath.Join(dir, name)
		if *update {
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(p)
		if err != nil || string(got) != string(data) {
			t.Errorf("%s no está al día; ejecuta go test ./internal/build -run TestFixturesLevel2 -update", p)
		}
	}
}
