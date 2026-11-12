package assemble

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"debfactory/internal/manifest"
)

func TestMan(t *testing.T) {
	dir := extracted(t, map[string]os.FileMode{
		"buf/share/man/man1/buf.1": 0o644, "buf/share/man/man1/buf-lint.1": 0o644, "doc/x.3pm": 0o644,
	})
	m := baseManifest()
	m.Man = []string{"buf/share/man/man1/*.1", "doc/x.3pm"}
	files, err := Man(m, "amd64", dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var dsts []string
	for _, f := range files {
		dsts = append(dsts, f.Dst)
		if f.Mode != 0o644 {
			t.Errorf("%s: modo %04o", f.Dst, f.Mode)
		}
	}
	want := []string{"/usr/share/man/man1/buf-lint.1.gz", "/usr/share/man/man1/buf.1.gz", "/usr/share/man/man3/x.3pm.gz"}
	if !slices.Equal(dsts, want) {
		t.Errorf("Man = %v", dsts)
	}
	gz, _ := os.ReadFile(files[1].Src)
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil || zr.Name != "" || !zr.ModTime.IsZero() {
		t.Errorf("gzip no es -9n: %v %q %v", err, zr.Name, zr.ModTime)
	}
	if text, _ := io.ReadAll(zr); string(text) != "buf/share/man/man1/buf.1" {
		t.Errorf("contenido = %q", text)
	}
	m.Man = []string{"nada/*.1"}
	if _, err := Man(m, "amd64", dir, t.TempDir()); err == nil || !strings.Contains(err.Error(), `man[0]: el patrón "nada/*.1" no seleccionó ningún archivo`) {
		t.Errorf("error = %v", err)
	}
}

func TestCompletions(t *testing.T) {
	dir := extracted(t, map[string]os.FileMode{"buf/etc/bash_completion.d/buf": 0o644, "tool": 0o755})
	m := baseManifest()
	m.Completions = manifest.Completions{
		Bash: &manifest.Completion{File: "buf/etc/bash_completion.d/buf"},
		Fish: &manifest.Completion{Generate: "tool completion fish"},
	}
	installed := []File{{Dst: "/usr/bin/tool", Src: filepath.Join(dir, "tool"), Mode: 0o755}}
	var gotArgv []string
	var staged bool
	gen := func(_ context.Context, arch, stage string, argv []string) ([]byte, error) {
		gotArgv = argv
		_, err := os.Stat(filepath.Join(stage, "usr/bin/tool"))
		staged = err == nil && arch == "arm64"
		return []byte("complete -c tool\n"), nil
	}
	files, err := Completions(context.Background(), m, "arm64", dir, t.TempDir(), installed, gen)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Dst != "/usr/share/bash-completion/completions/tool" ||
		files[1].Dst != "/usr/share/fish/vendor_completions.d/tool.fish" {
		t.Fatalf("Completions = %v", files)
	}
	if !slices.Equal(gotArgv, []string{"tool", "completion", "fish"}) || !staged {
		t.Errorf("argv = %v, árbol preparado = %v", gotArgv, staged)
	}
	m.Completions = manifest.Completions{Zsh: &manifest.Completion{File: "no/existe"}}
	if _, err := Completions(context.Background(), m, "amd64", dir, t.TempDir(), installed, gen); err == nil ||
		!strings.Contains(err.Error(), `completions.zsh.file: no existe "no/existe"`) {
		t.Errorf("error = %v", err)
	}
}
