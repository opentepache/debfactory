package supply

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeZig simula zig fetch: url -> hash y tarball de la caché de Zig.
type fakeZig struct {
	dir   string
	pkgs  map[string]struct{ hash, zon string }
	calls []string
	wrong map[string]string // url -> hash distinto
}

// cacheTarball imita p/<hash>.tar.gz: envoltorio <hash>/ y el directorio
// superior original del archivo.
func (f *fakeZig) cacheTarball(hash, zon string) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name, body string) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.WriteHeader(&tar.Header{Name: hash + "/", Mode: 0o755, Typeflag: tar.TypeDir})
	tw.WriteHeader(&tar.Header{Name: hash + "/orig-top/", Mode: 0o755, Typeflag: tar.TypeDir})
	add(hash+"/orig-top/src.zig", "pub fn x() void {}\n")
	if zon != "" {
		add(hash+"/orig-top/build.zig.zon", zon)
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(f.dir, hash+".tar.gz")
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p
}

func (f *fakeZig) Fetch(_ context.Context, url string) (string, string, error) {
	f.calls = append(f.calls, url)
	p, ok := f.pkgs[url]
	if !ok {
		return "", "", errors.New("404")
	}
	h := p.hash
	if w, ok := f.wrong[url]; ok {
		h = w
	}
	return h, f.cacheTarball(h, p.zon), nil
}

func zonWith(deps ...string) string {
	return ".{ .name = .x, .dependencies = .{ " + strings.Join(deps, ", ") + " } }"
}

func remote(name, url, hash string) string {
	return fmt.Sprintf(".%s = .{ .url = %q, .hash = %q }", name, url, hash)
}

// TestResolveZigDeps: directas, de una dependencia local (pkg/b), transitivas
// (dentro de A) y sin las de example/.
func TestResolveZigDeps(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "build.zig.zon"), []byte(zonWith(remote("a", "https://d/a.tgz", "a-1-AAA"), `.b = .{ .path = "pkg/b" }`)), 0o644)
	os.MkdirAll(filepath.Join(src, "pkg/b"), 0o755)
	os.WriteFile(filepath.Join(src, "pkg/b/build.zig.zon"), []byte(zonWith(remote("c", "https://d/c.tgz", "c-1-CCC"))), 0o644)
	os.MkdirAll(filepath.Join(src, "example/e"), 0o755)
	os.WriteFile(filepath.Join(src, "example/e/build.zig.zon"), []byte(zonWith(remote("bad", "https://d/COMMIT.tgz", "x-1-XXX"))), 0o644)
	f := &fakeZig{dir: t.TempDir(), pkgs: map[string]struct{ hash, zon string }{
		"https://d/a.tgz":     {"a-1-AAA", zonWith(remote("dd", "git+https://d/d#123", "d-1-DDD"))},
		"https://d/c.tgz":     {"c-1-CCC", ""},
		"git+https://d/d#123": {"d-1-DDD", ""},
	}}
	out := t.TempDir()
	deps, err := ResolveZigDeps(context.Background(), src, []string{"example/"}, nil, f, out, nil)
	if err != nil {
		t.Fatal(err)
	}
	var hashes []string
	for _, d := range deps {
		hashes = append(hashes, d.Hash)
		if len(d.SHA256) != 64 {
			t.Errorf("%s sin sha256", d.Hash)
		}
	}
	if strings.Join(hashes, ",") != "a-1-AAA,c-1-CCC,d-1-DDD" {
		t.Fatalf("deps = %v (llamadas %v)", hashes, f.calls)
	}
	// Extraído como lo espera zig build --system: sin envoltorio ni orig-top.
	if _, err := os.Stat(filepath.Join(out, "a-1-AAA", "src.zig")); err != nil {
		t.Errorf("extracción: %v", err)
	}
	if err := VerifyZigDeps(out, deps); err != nil {
		t.Errorf("VerifyZigDeps: %v", err)
	}
	// Un archivo cambiado en una dependencia ya no coincide.
	os.WriteFile(filepath.Join(out, "c-1-CCC", "src.zig"), []byte("otro"), 0o644)
	if err := VerifyZigDeps(out, deps); err == nil || !strings.Contains(err.Error(), "c-1-CCC") {
		t.Errorf("alterada: %v", err)
	}
}

// TestResolveZigDepsMismatch: un hash distinto del declarado falla.
func TestResolveZigDepsMismatch(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "build.zig.zon"), []byte(zonWith(remote("a", "https://d/a.tgz", "a-1-AAA"))), 0o644)
	f := &fakeZig{dir: t.TempDir(), pkgs: map[string]struct{ hash, zon string }{"https://d/a.tgz": {"a-1-AAA", ""}},
		wrong: map[string]string{"https://d/a.tgz": "a-1-OTRO"}}
	_, err := ResolveZigDeps(context.Background(), src, nil, nil, f, t.TempDir(), nil)
	var hm *HashMismatchError
	if !errors.As(err, &hm) || hm.Declared != "a-1-AAA" || hm.Got != "a-1-OTRO" {
		t.Fatalf("error = %v", err)
	}
}

// TestTreeSHA256Stable: el sha256 no depende del orden de creación ni del mtime.
func TestTreeSHA256Stable(t *testing.T) {
	mk := func(order []string) string {
		d := t.TempDir()
		for _, n := range order {
			os.MkdirAll(filepath.Dir(filepath.Join(d, n)), 0o755)
			os.WriteFile(filepath.Join(d, n), []byte(n), 0o644)
		}
		s, err := TreeSHA256(d)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if a, b := mk([]string{"a", "z/b", "m"}), mk([]string{"m", "a", "z/b"}); a != b {
		t.Errorf("%s ≠ %s", a, b)
	}
}

// TestResolveZigDepsSkip: deps.skip no baja la dependencia ni sus
// transitivas, y un nombre que no aparece falla.
func TestResolveZigDepsSkip(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "build.zig.zon"), []byte(zonWith(remote("a", "https://d/a.tgz", "a-1-AAA"),
		remote("c", "https://d/c.tgz", "c-1-CCC"))), 0o644)
	f := &fakeZig{dir: t.TempDir(), pkgs: map[string]struct{ hash, zon string }{
		"https://d/a.tgz":     {"a-1-AAA", zonWith(remote("dd", "git+https://d/d#123", "d-1-DDD"))},
		"https://d/c.tgz":     {"c-1-CCC", ""},
		"git+https://d/d#123": {"d-1-DDD", ""},
	}}
	deps, err := ResolveZigDeps(context.Background(), src, nil, map[string]string{"a": "no se usa en Linux"}, f, t.TempDir(), nil)
	if err != nil || len(deps) != 1 || deps[0].Hash != "c-1-CCC" || deps[0].Name != "c" {
		t.Fatalf("deps = %+v, %v (llamadas %v)", deps, err, f.calls)
	}
	_, err = ResolveZigDeps(context.Background(), src, nil, map[string]string{"zz": "no existe en ningún zon"}, f, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "deps.skip: zz") {
		t.Fatalf("nombre desconocido: %v", err)
	}
}

// TestResolveZigDepsSkipLocal: omitir una dependencia local (pkg/l) deja
// fuera su carpeta y sus descargas.
func TestResolveZigDepsSkipLocal(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "build.zig.zon"), []byte(zonWith(remote("c", "https://d/c.tgz", "c-1-CCC"), `.l = .{ .path = "pkg/l", .lazy = true }`)), 0o644)
	os.MkdirAll(filepath.Join(src, "pkg/l"), 0o755)
	os.WriteFile(filepath.Join(src, "pkg/l/build.zig.zon"), []byte(zonWith(remote("g", "https://d/g.tgz", "g-1-GGG"))), 0o644)
	f := &fakeZig{dir: t.TempDir(), pkgs: map[string]struct{ hash, zon string }{"https://d/c.tgz": {"c-1-CCC", ""}}}
	deps, err := ResolveZigDeps(context.Background(), src, nil, map[string]string{"l": "solo para Darwin"}, f, t.TempDir(), nil)
	if err != nil || len(deps) != 1 || deps[0].Hash != "c-1-CCC" {
		t.Fatalf("deps = %+v, %v (llamadas %v)", deps, err, f.calls)
	}
}

// TestDepStore: la segunda resolución reutiliza la copia guardada solo si su
// árbol da el sha256 del lockfile; una copia alterada se descarta y se baja.
func TestDepStore(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "build.zig.zon"), []byte(zonWith(remote("c", "https://d/c.tgz", "c-1-CCC"))), 0o644)
	f := &fakeZig{dir: t.TempDir(), pkgs: map[string]struct{ hash, zon string }{"https://d/c.tgz": {"c-1-CCC", ""}}}
	store := &DepStore{Dir: t.TempDir()}
	first, err := ResolveZigDeps(context.Background(), src, nil, nil, f, t.TempDir(), store)
	if err != nil || len(f.calls) != 1 {
		t.Fatalf("primera: %v, llamadas %v", err, f.calls)
	}
	store.Expected = map[string]string{"c-1-CCC": first[0].SHA256}
	second, err := ResolveZigDeps(context.Background(), src, nil, nil, f, t.TempDir(), store)
	if err != nil || len(f.calls) != 1 || second[0] != first[0] {
		t.Fatalf("reutilizada: %+v, %v, llamadas %v", second, err, f.calls)
	}
	// Copia guardada alterada: no se usa, se vuelve a bajar.
	os.WriteFile(filepath.Join(store.Dir, "c-1-CCC", "src.zig"), []byte("alterado"), 0o644)
	third, err := ResolveZigDeps(context.Background(), src, nil, nil, f, t.TempDir(), store)
	if err != nil || len(f.calls) != 2 || third[0] != first[0] {
		t.Fatalf("alterada: %+v, %v, llamadas %v", third, err, f.calls)
	}
}
