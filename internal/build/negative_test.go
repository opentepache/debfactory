package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/fetch"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

const negDir = repo + "/testdata/negative"

// loadNegative instala el caso negativo <name> de testdata/negative: el
// manifiesto, los tar.gz (generados: son deterministas), el archivo de
// checksums si lo declara y el lockfile (el del fixture si existe; si no, uno
// correcto generado aquí).
func (h *harness) loadNegative(name, licenseFile string) {
	h.t.Helper()
	manifest, err := os.ReadFile(filepath.Join(negDir, name+".yaml"))
	if err != nil {
		h.t.Fatal(err)
	}
	h.write("packages/"+name+".yaml", manifest)
	text := []byte(testutil.MITText)
	if licenseFile != "" {
		if text, err = os.ReadFile(filepath.Join(negDir, "licenses", licenseFile)); err != nil {
			h.t.Fatal(err)
		}
	}
	archives := exampleArchives(name, name+" 1.0.0\n", text)
	base := "https://github.com/example/" + name + "/releases/download/v1.0.0/"
	for token, data := range archives {
		h.serve(base+name+"-"+token+".tar.gz", data)
	}
	checksums, err := os.ReadFile(filepath.Join(negDir, name+".sha256.txt"))
	if err == nil {
		h.serve(base+"sha256.txt", checksums)
	} else {
		checksums = nil
	}
	lockData, err := os.ReadFile(filepath.Join(negDir, name+".lock.yaml"))
	if err != nil {
		lockData = lockFor(h.t, "1.0.0", 1767225600, text, archives, checksums)
	}
	h.write("packages/"+name+".lock.yaml", lockData)
}

// TestNegative cubre SC-005: cada caso falla antes de producir un .deb, con
// el mensaje exacto de contracts/cli.md, y dist/ queda sin archivos.
func TestNegative(t *testing.T) {
	cases := []struct {
		name, license string
		want          string   // primera línea exacta (o prefijo si termina en "…")
		detail        []string // prefijos de las líneas de detalle
		noRequests    bool
	}{
		{name: "missing-license-file", noRequests: true,
			want: `debfactory: missing-license-file 1.0.0 -: validate: packages/missing-license-file.yaml: license: falta la propiedad "file"`},
		{name: "foreign-url", noRequests: true,
			want: "debfactory: foreign-url 1.0.0 -: validate: packages/foreign-url.yaml: artifact.url: fuera de los releases de example/foreign-url"},
		{name: "unknown-var", noRequests: true,
			want: "debfactory: unknown-var 1.0.0 -: validate: packages/unknown-var.yaml: artifact.url: variable desconocida {{arc}}"},
		{name: "altered-hash",
			want:   "debfactory: altered-hash 1.0.0 amd64: verify: sha256 no coincide para altered-hash-x86_64.tar.gz",
			detail: []string{"  esperado (lock):  " + strings.Repeat("0000aaaa", 8), "  obtenido:         "}},
		{name: "checksums-mismatch",
			want:   "debfactory: checksums-mismatch 1.0.0 amd64: verify: sha256 no coincide con sha256.txt de upstream para checksums-mismatch-x86_64.tar.gz",
			detail: []string{"  esperado (sha256.txt):  " + strings.Repeat("1111", 16), "  obtenido:              "}},
		{name: "sspl", license: "SSPL-1.0.txt",
			want: `debfactory: sspl 1.0.0 amd64: license: licencia declarada "SSPL-1.0" no está en config/licenses.yaml (detectada "SSPL-1.0")`},
		{name: "gibberish", license: "gibberish.txt",
			want: `debfactory: gibberish 1.0.0 amd64: license: texto de licencia no identificable (cobertura 0 % < 90 %; declarada "MIT")`},
		{name: "mit-as-apache", license: "MIT-declared-Apache.txt",
			want: `debfactory: mit-as-apache 1.0.0 amd64: license: licencia detectada "MIT" no coincide con la declarada "Apache-2.0"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.loadNegative(c.name, c.license)
			err := h.env.Build(context.Background(), c.name, Options{})
			if err == nil {
				t.Fatal("el build debería fallar")
			}
			lines := strings.Split(err.Error(), "\n")
			if lines[0] != c.want {
				t.Errorf("mensaje:\n  %s\nse esperaba:\n  %s", lines[0], c.want)
			}
			for i, d := range c.detail {
				if len(lines) <= i+1 || !strings.HasPrefix(lines[i+1], d) {
					t.Errorf("detalle %d = %q; se esperaba que empezara por %q", i, lines[min(i+1, len(lines)-1)], d)
				}
			}
			var re *report.Error
			var list report.Errors
			var env *report.EnvError
			if (!errors.As(err, &re) && !errors.As(err, &list)) || errors.As(err, &env) {
				t.Errorf("tipo de error %T: no corresponde al código de salida 1", err)
			}
			if files := h.distFiles(); len(files) != 0 {
				t.Errorf("dist/ no está vacío: %v", files)
			}
			if c.noRequests && h.hits.Load() != 0 {
				t.Errorf("el servidor recibió %d peticiones", h.hits.Load())
			}
			if len(h.linted) != 0 {
				t.Error("se llegó a lintian")
			}
		})
	}
}

// TestNegativeLock: lock verifica los checksums upstream antes del gate de
// licencias y, si algo falla, no escribe el lockfile ni guarda nada en caché.
func TestNegativeLock(t *testing.T) {
	cases := map[string]struct{ license, want string }{
		"checksums-mismatch": {"", "debfactory: checksums-mismatch 1.0.0 amd64: verify: sha256 no coincide con sha256.txt de upstream para checksums-mismatch-x86_64.tar.gz"},
		"sspl":               {"SSPL-1.0.txt", `debfactory: sspl 1.0.0 -: license: licencia declarada "SSPL-1.0" no está en config/licenses.yaml (detectada "SSPL-1.0")`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.loadNegative(name, c.license)
			lockPath := filepath.Join(h.root, "packages", name+".lock.yaml")
			os.Remove(lockPath)
			err := h.env.Lock(context.Background(), name, false)
			if err == nil || strings.Split(err.Error(), "\n")[0] != c.want {
				t.Fatalf("error = %v\nse esperaba %s", err, c.want)
			}
			if _, err := os.Stat(lockPath); err == nil {
				t.Error("se escribió el lockfile")
			}
			for _, sub := range []string{"sha256", "tmp"} {
				if entries, _ := os.ReadDir(filepath.Join(h.env.Fetcher.Cache, sub)); len(entries) != 0 {
					t.Errorf("caché/%s tiene %d archivos", sub, len(entries))
				}
			}
		})
	}
}

// TestLicenseFromRepo: el texto de license.file.repo se descarga de
// raw.githubusercontent.com al tag exacto y se verifica contra el lock.
func TestLicenseFromRepo(t *testing.T) {
	h := newHarness(t)
	h.loadOK()
	m, _ := os.ReadFile(filepath.Join(h.root, "packages/example.yaml"))
	h.write("packages/example.yaml", []byte(strings.Replace(string(m), "file: {archive: LICENSE}", "file: {repo: LICENSE}", 1)))
	h.serve("https://raw.githubusercontent.com/example/example/v1.0.0/LICENSE", []byte(testutil.MITText))
	if err := h.env.Build(context.Background(), "example", Options{Arch: "amd64"}); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	h.serve("https://raw.githubusercontent.com/example/example/v1.0.0/LICENSE", []byte(testutil.MITText+"\nextra\n"))
	h.env.Fetcher = fetch.New(t.TempDir(), h.env.Fetcher.Client.Timeout, testutil.Redirect{Server: h.srv})
	err := h.env.Build(context.Background(), "example", Options{Arch: "amd64"})
	if err == nil || !strings.HasPrefix(err.Error(), "debfactory: example 1.0.0 amd64: verify: sha256 no coincide para LICENSE") {
		t.Errorf("error = %v", err)
	}
}
