package build

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/fetch"
	"debfactory/internal/supply"
	"debfactory/internal/testutil"
)

// TestFixturesOK mantiene testdata/fixtures/ok al día con su generador: el
// manifiesto es contracts/examples/example.yaml tal cual, más dos tar.gz con
// un ELF estático de juguete y un LICENSE MIT, y su lockfile.
// Regenerar: go test ./internal/build -run TestFixturesOK -update
func TestFixturesOK(t *testing.T) {
	manifest, err := os.ReadFile(filepath.Join(repo, "specs/001-manifest-deb-builder/contracts/examples/example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	license := []byte(testutil.MITText)
	archives := exampleArchives("example", "example 1.0.0\n", license)
	want := map[string][]byte{
		"example.yaml":           manifest,
		"example.lock.yaml":      lockFor(t, "1.0.0", 1767225600, license, archives, nil),
		"example-x86_64.tar.gz":  archives["x86_64"],
		"example-aarch64.tar.gz": archives["aarch64"],
	}
	dir := filepath.Join(repo, "testdata/fixtures/ok")
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
			t.Errorf("%s no está al día; ejecuta go test ./internal/build -run TestFixturesOK -update", p)
		}
	}
}

// TestBuildReproducible (SC-006): dos construcciones independientes (otra
// caché y otro workdir) dan .deb idénticos, con los metadatos de control
// esperados.
func TestBuildReproducible(t *testing.T) {
	h := newHarness(t)
	h.loadOK()
	ctx := context.Background()
	if err := h.env.Build(ctx, "example", Options{Reproducible: true}); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	debs := map[string]string{}
	for _, arch := range []string{"amd64", "arm64"} {
		p := filepath.Join(h.root, "dist", "example_1.0.0-0~tepache1_"+arch+".deb")
		sha, err := fetch.SHA256File(p)
		if err != nil {
			t.Fatal(err)
		}
		debs[arch] = sha
		if !strings.Contains(h.out.String(), "example 1.0.0 "+arch+": reproducible: "+sha) {
			t.Errorf("falta la línea reproducible de %s:\n%s", arch, h.out.String())
		}
	}
	if len(h.linted) != 2 {
		t.Errorf("lintian se llamó %d veces", len(h.linted))
	}

	os.RemoveAll(filepath.Join(h.root, "dist"))
	h.env.Workdir = t.TempDir()
	h.env.Fetcher = fetch.New(t.TempDir(), time.Minute, testutil.Redirect{Server: h.srv})
	if err := h.env.Build(ctx, "example", Options{}); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	for arch, sha := range debs {
		got, _ := fetch.SHA256File(filepath.Join(h.root, "dist", "example_1.0.0-0~tepache1_"+arch+".deb"))
		if got != sha {
			t.Errorf("%s: %s != %s", arch, got, sha)
		}
	}
	if files := h.distFiles(); len(files) != 6 {
		t.Errorf("dist/ = %v; se esperaban 2 .deb, 2 build.json y 2 supply.json", files)
	}
	// Informe de cadena de suministro (data-model §7): example no firma ni
	// publica procedencia (el servidor de prueba no tiene atestaciones).
	var rep supply.Report
	if b, err := os.ReadFile(filepath.Join(h.root, "dist", "example_1.0.0-0~tepache1_amd64.supply.json")); err != nil {
		t.Error(err)
	} else if json.Unmarshal(b, &rep) != nil || rep.Package != "example" || rep.Arch != "amd64" ||
		rep.Signature.Type != "none" || rep.Provenance.Present {
		t.Errorf("supply.json = %s", b)
	}

	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("sin dpkg-deb")
	}
	out, err := exec.Command("dpkg-deb", "-f", filepath.Join(h.root, "dist", "example_1.0.0-0~tepache1_arm64.deb"),
		"Package", "Version", "Maintainer", "Homepage", "Architecture", "Description").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Package: example\n", "Version: 1.0.0-0~tepache1\n", "Maintainer: " + h.env.Cfg.Maintainer + "\n",
		"Homepage: https://github.com/example/example\n", "Architecture: arm64\n",
		"Description: example single static binary tool\n Placeholder showing the smallest valid manifest",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("dpkg-deb -f no contiene %q:\n%s", want, out)
		}
	}
}
