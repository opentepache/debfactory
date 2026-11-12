package build

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/lint"
	"debfactory/internal/lock"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

var update = flag.Bool("update", false, "regenera testdata/fixtures/ok")

const repo = "../.."

// harness es un repositorio temporal con la config real, un servidor TLS
// local que hace de GitHub y lintian simulado (los tests no necesitan Docker).
type harness struct {
	t      *testing.T
	root   string
	files  map[string][]byte // host+ruta -> contenido
	hits   atomic.Int32
	srv    *httptest.Server
	env    *Env
	out    bytes.Buffer
	errb   bytes.Buffer
	linted []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, root: t.TempDir(), files: map[string][]byte{}}
	for _, f := range []string{config.MainFile, config.LicensesFile} {
		data, err := os.ReadFile(filepath.Join(repo, f))
		if err != nil {
			t.Fatal(err)
		}
		h.write(f, data)
	}
	if err := os.MkdirAll(filepath.Join(h.root, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits.Add(1)
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			w.Write([]byte(`{"published_at":"2026-01-01T00:00:00Z"}`))
			return
		}
		if b, ok := h.files[r.Host+r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(h.srv.Close)
	cfg, err := config.Load(h.root)
	if err != nil {
		t.Fatal(err)
	}
	h.env = &Env{
		Root:    h.root,
		Workdir: t.TempDir(),
		Cfg:     cfg,
		Fetcher: fetch.New(t.TempDir(), time.Minute, testutil.Redirect{Server: h.srv}),
		GitHub:  &fetch.GitHub{Client: h.srv.Client(), API: h.srv.URL},
		Out:     &report.Printer{Out: &h.out, Err: &h.errb},
		Lint: func(_ context.Context, _ *config.Config, deb string) (*lint.Result, error) {
			h.linted = append(h.linted, deb)
			return &lint.Result{}, nil
		},
		Generate: func(context.Context, string, string, []string) ([]byte, error) {
			return nil, errors.New("los tests no generan completions")
		},
	}
	return h
}

func (h *harness) write(rel string, data []byte) {
	h.t.Helper()
	p := filepath.Join(h.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// serve publica data en una URL https (github.com, raw.githubusercontent.com…).
func (h *harness) serve(url string, data []byte) {
	u, err := neturl.Parse(url)
	if err != nil {
		h.t.Fatal(err)
	}
	h.files[u.Host+u.Path] = data
}

// loadOK instala el fixture de testdata/fixtures/ok.
func (h *harness) loadOK() {
	h.t.Helper()
	dir := filepath.Join(repo, "testdata/fixtures/ok")
	for _, f := range []string{"example.yaml", "example.lock.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			h.t.Fatal(err)
		}
		h.write("packages/"+f, data)
	}
	for _, token := range []string{"x86_64", "aarch64"} {
		data, err := os.ReadFile(filepath.Join(dir, "example-"+token+".tar.gz"))
		if err != nil {
			h.t.Fatal(err)
		}
		h.serve("https://github.com/example/example/releases/download/v1.0.0/example-"+token+".tar.gz", data)
	}
}

// distFiles lista dist/ (incluidos temporales).
func (h *harness) distFiles() []string {
	entries, _ := os.ReadDir(filepath.Join(h.root, "dist"))
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// exampleArchives genera los tar.gz del ejemplo: un ELF de juguete que
// imprime msg y el texto de licencia.
func exampleArchives(name, msg string, license []byte) map[string][]byte {
	out := map[string][]byte{}
	for arch, token := range map[string]string{"amd64": "x86_64", "arm64": "aarch64"} {
		out[token] = testutil.TarGz([]testutil.Entry{
			{Name: name, Mode: 0o755, Body: testutil.TinyELF(arch, msg)},
			{Name: "LICENSE", Body: license},
		})
	}
	return out
}

// lockFor genera el lockfile de un paquete de prueba.
func lockFor(t *testing.T, version string, epoch int64, license []byte, archives map[string][]byte, checksums []byte) []byte {
	t.Helper()
	e := &lock.Entry{SourceDateEpoch: epoch, LicenseSHA256: fetch.SHA256(license),
		Artifacts: map[string]string{"amd64": fetch.SHA256(archives["x86_64"]), "arm64": fetch.SHA256(archives["aarch64"])}}
	if checksums != nil {
		e.ChecksumsSHA256 = fetch.SHA256(checksums)
	}
	f := &lock.File{Schema: 1, Versions: map[string]*lock.Entry{version: e}}
	tmp := t.TempDir()
	os.Mkdir(filepath.Join(tmp, "packages"), 0o755)
	if err := f.Write(tmp, "x"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, lock.Path("x")))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
