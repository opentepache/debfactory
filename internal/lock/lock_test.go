package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func h(c string) string { return strings.Repeat(c, 64) }

func TestRoundTripDeterministic(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := Read(root, "fnm")
	if err != nil || len(f.Versions) != 0 {
		t.Fatalf("Read sin archivo = %+v, %v", f, err)
	}
	f.Versions["1.39.0"] = &Entry{SourceDateEpoch: 1772810627, LicenseSHA256: h("a"),
		Artifacts: map[string]string{"arm64": h("c"), "amd64": h("b")}}
	f.Versions["1.4.0"] = &Entry{SourceDateEpoch: 1700000000, LicenseSHA256: h("d"), ChecksumsSHA256: h("e"),
		Artifacts: map[string]string{"amd64": h("f"), "arm64": strings.Repeat("ab", 32)}}
	if err := f.Write(root, "fnm"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(root, Path("fnm")))
	want := "# Generado por `debfactory lock`; no se edita a mano. Se revisa en el diff de git.\n" +
		"schema: 1\nversions:\n" +
		"  1.4.0:\n    source_date_epoch: 1700000000\n    license_sha256: " + h("d") + "\n" +
		"    checksums_sha256: " + h("e") + "\n    artifacts:\n      amd64: " + h("f") + "\n      arm64: " + strings.Repeat("ab", 32) + "\n" +
		"  1.39.0:\n    source_date_epoch: 1772810627\n    license_sha256: " + h("a") + "\n" +
		"    artifacts:\n      amd64: " + h("b") + "\n      arm64: " + h("c") + "\n"
	if string(first) != want {
		t.Fatalf("salida:\n%s\nse esperaba:\n%s", first, want)
	}
	g, err := Read(root, "fnm")
	if err != nil {
		t.Fatal(err)
	}
	if !g.Versions["1.39.0"].Equal(f.Versions["1.39.0"]) {
		t.Error("la lectura no coincide con lo escrito")
	}
	if err := g.Write(root, "fnm"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(root, Path("fnm")))
	if string(first) != string(second) {
		t.Error("escribir dos veces da resultados distintos")
	}
}

func TestReadRejects(t *testing.T) {
	cases := map[string]string{
		"hash corto":        "schema: 1\nversions:\n  1.0.0:\n    source_date_epoch: 1\n    license_sha256: abc\n    artifacts: {}\n",
		"campo desconocido": "schema: 1\nversions:\n  1.0.0:\n    source_date_epoch: 1\n    license_sha256: " + h("a") + "\n    sha: x\n",
		"schema":            "schema: 2\nversions: {}\n",
		"fecha":             "schema: 1\nversions:\n  1.0.0:\n    source_date_epoch: 0\n    license_sha256: " + h("a") + "\n",
		"hash de fuente":    "schema: 1\nversions:\n  1.0.0:\n    source_date_epoch: 1\n    license_sha256: " + h("a") + "\n    source_sha256: xyz\n",
		"hash de artefacto": "schema: 1\nversions:\n  1.0.0:\n    source_date_epoch: 1\n    license_sha256: " + h("a") + "\n    artifacts: {amd64: " + h("A") + "}\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			os.Mkdir(filepath.Join(root, "packages"), 0o755)
			os.WriteFile(filepath.Join(root, Path("x")), []byte(body), 0o644)
			if _, err := Read(root, "x"); err == nil {
				t.Fatal("se esperaba error")
			}
		})
	}
}

func TestComplete(t *testing.T) {
	e := &Entry{SourceDateEpoch: 1, LicenseSHA256: h("a"), Artifacts: map[string]string{"amd64": h("b")}}
	if err := e.Complete([]string{"amd64", "arm64"}, false); err == nil || !strings.Contains(err.Error(), "artifacts.arm64") {
		t.Errorf("error = %v", err)
	}
	e.Artifacts["arm64"] = h("c")
	if err := e.Complete([]string{"amd64", "arm64"}, true); err == nil || !strings.Contains(err.Error(), "checksums_sha256") {
		t.Errorf("error = %v", err)
	}
	if err := e.Complete([]string{"amd64", "arm64"}, false); err != nil {
		t.Error(err)
	}
}

// TestCompleteSource: nivel 2 (specs/008-source-builds, data-model §1).
func TestCompleteSource(t *testing.T) {
	e := &Entry{SourceDateEpoch: 1, LicenseSHA256: h("a")}
	if err := e.CompleteSource(); err == nil || !strings.Contains(err.Error(), "source_sha256") {
		t.Errorf("error = %v", err)
	}
	e.SourceSHA256 = h("b")
	if err := e.CompleteSource(); err != nil {
		t.Error(err)
	}
	o := *e
	o.SourceSHA256 = h("c")
	if e.Equal(&o) {
		t.Error("Equal ignora source_sha256")
	}
}

// TestPhase009Fields (specs/009, data-model §2): toolchain, dependencias,
// archivo del tag, extras y componente zigdeps.
func TestPhase009Fields(t *testing.T) {
	e := &Entry{SourceDateEpoch: 1, LicenseSHA256: h("a"), SourceSHA256: h("b"), TagArchiveSHA256: h("c"),
		ExtraFiles: map[string]string{"VERSION": h("d")}, Toolchain: map[string]string{"amd64": h("e"), "arm64": h("f")},
		Deps:          []Dep{{URL: "https://x/a.tar.gz", Hash: "a-0.1.0-AAA", SHA256: h("1")}, {URL: "git+https://x/b#c", Hash: "b-0.2.0-BBB", SHA256: h("2")}},
		ZigdepsSHA256: h("3")}
	if err := e.wellFormed(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "packages"), 0o755)
	f := &File{Schema: 1, Versions: map[string]*Entry{"1.4.0": e}}
	if err := f.Write(root, "x"); err != nil {
		t.Fatal(err)
	}
	back, err := Read(root, "x")
	if err != nil {
		t.Fatal(err)
	}
	if !back.Versions["1.4.0"].Equal(e) {
		t.Fatalf("ida y vuelta: %+v", back.Versions["1.4.0"])
	}
	bad := *e
	bad.Deps = []Dep{e.Deps[1], e.Deps[0]}
	if err := bad.wellFormed(); err == nil || !strings.Contains(err.Error(), "ordenadas") {
		t.Errorf("deps desordenadas: %v", err)
	}
	other := *e
	other.Deps = append([]Dep{}, e.Deps...)
	other.Deps[0].SHA256 = h("9")
	if e.Equal(&other) {
		t.Error("Equal ignora deps")
	}
}
