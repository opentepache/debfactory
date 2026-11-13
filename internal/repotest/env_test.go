//go:build e2e

package repotest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	pkg2 "debfactory/internal/pkg"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// realRoot es la raíz del repositorio git.
const realRoot = "../.."

// captured guarda toda la salida de las órdenes de los E2E, para la
// búsqueda de secretos (SC-005, T054).
var captured struct {
	sync.Mutex
	bytes.Buffer
}

type captureWriter struct{ w io.Writer }

func (c captureWriter) Write(p []byte) (int, error) {
	captured.Lock()
	captured.Write(p)
	captured.Unlock()
	return c.w.Write(p)
}

// e2eEnv prepara una raíz temporal con config/, packages/ y docs/ copiados
// y dist/ enlazado al real, un <data> temporal con una llave de prueba y la
// configuración ajustada a esa llave.
func e2eEnv(t *testing.T) *repo.Env {
	t.Helper()
	if _, err := exec.LookPath("aptly"); err != nil {
		t.Skip("aptly no está instalado")
	}
	root := t.TempDir()
	for _, d := range []string{"config", "packages", "docs"} {
		copyDir(t, filepath.Join(realRoot, d), filepath.Join(root, d))
	}
	// dist/ es un directorio de enlaces a los archivos reales: los tests
	// pueden añadir, quitar o reescribir (writeFile) sin tocar dist/ real.
	abs, _ := filepath.Abs(filepath.Join(realRoot, "dist"))
	os.MkdirAll(filepath.Join(root, "dist"), 0o755)
	entries, _ := os.ReadDir(abs)
	for _, en := range entries {
		os.Symlink(filepath.Join(abs, en.Name()), filepath.Join(root, "dist", en.Name()))
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	home, master, sub := testutil.TestKey(t)
	cfg.Repo.Signing.Masters = []string{master}
	cfg.Repo.Signing.Subkey = sub
	data := t.TempDir()
	if err := os.Symlink(home, filepath.Join(data, "gnupg")); err != nil {
		t.Fatal(err)
	}
	w := captureWriter{testWriter{t}}
	return &repo.Env{Root: root, DataDir: data, Cfg: cfg, Out: &report.Printer{Out: w, Err: w}}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(bytes.TrimRight(p, "\n")))
	return len(p), nil
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// publishAll publica la versión del manifiesto de cada paquete real.
func publishAll(t *testing.T, ctx context.Context, e *repo.Env) *repo.State {
	t.Helper()
	names, err := build.ListPackages(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Publish(ctx, names, nil, false); err != nil {
		t.Skipf("no se pudo publicar dist/ (¿faltan .deb o test.json con deb_sha256? ejecuta `debfactory build --all && debfactory test --all`): %v", err)
	}
	st, err := repo.ReadState(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// serveCurrent sirve la generación vigente en http://repo.
func serveCurrent(t *testing.T, ctx context.Context, e *repo.Env) *Server {
	t.Helper()
	srv, err := StartServer(ctx, e.Cfg, filepath.Join(repo.PublicDir(e.DataDir), "current"), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return srv
}

// guide carga los bloques de la guía de la raíz de e.
func guide(t *testing.T, e *repo.Env) map[string]string {
	t.Helper()
	b, err := Blocks(filepath.Join(e.Root, GuideFile))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// packagesIn devuelve los paquetes del estado publicados en una suite y su
// versión más alta.
func packagesIn(st *repo.State, suite string) map[string]string {
	out := map[string]string{}
	for _, name := range st.PackageNames() {
		for _, p := range st.Packages[name] {
			for _, s := range p.Suites {
				if s == suite {
					if _, ok := out[name]; !ok {
						out[name] = p.Version // la lista va de mayor a menor
					}
				}
			}
		}
	}
	return out
}

// writeFile reemplaza path (aunque sea un enlace a dist/ real) por data.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	os.Remove(path)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// editReport modifica un informe JSON de dist/ sin tocar el original.
func editReport(t *testing.T, path string, f func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	out, _ := json.MarshalIndent(m, "", "  ")
	writeFile(t, path, out)
}

// repack crea en dist/ una versión nueva de pkg a partir de la publicada
// (from), cambiando solo Version en el control (técnica del spike, research
// §1), con build.json y test.json aprobados para todas las suites.
func repack(t *testing.T, e *repo.Env, pkg, from, to string, suites []string) {
	t.Helper()
	dist := filepath.Join(e.Root, "dist")
	for _, arch := range e.Cfg.Arches {
		src := filepath.Join(dist, repo.DebFile(pkg, from, arch))
		dir := t.TempDir()
		x := filepath.Join(dir, "x")
		run(t, "dpkg-deb", "-R", src, x)
		ctl := filepath.Join(x, "DEBIAN", "control")
		data, _ := os.ReadFile(ctl)
		data = regexp.MustCompile(`(?m)^Version: .*$`).ReplaceAll(data, []byte("Version: "+to))
		os.WriteFile(ctl, data, 0o644)
		dst := filepath.Join(dist, repo.DebFile(pkg, to, arch))
		os.Remove(dst)
		run(t, "dpkg-deb", "--root-owner-group", "-b", x, dst)
		sha, err := fetch.SHA256File(dst)
		if err != nil {
			t.Fatal(err)
		}
		base := strings.TrimSuffix(dst, ".deb")
		bi := &pkg2.BuildInfo{SHA256: sha, Lintian: "pass", TestCommand: pkg + " --version"}
		b, _ := bi.Marshal()
		writeFile(t, base+".build.json", b)
		rep := installtest.Report{Package: pkg, Version: to, Arch: arch, DebSHA256: sha, Versions: "pass", Overall: "pass"}
		for _, s := range e.Cfg.Suites {
			status := "excluded"
			for _, ok := range suites {
				if ok == s.Name {
					status = "pass"
				}
			}
			rep.Results = append(rep.Results, installtest.Result{Suite: s.Name, Status: status})
		}
		tj, _ := json.MarshalIndent(rep, "", "  ")
		writeFile(t, base+".test.json", tj)
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
