package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const level2Dir = "../../testdata/fixtures/level2"

func readLevel2(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(level2Dir, "tepache-hello.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// level2Root copia el paquete de prueba a una raíz temporal y aplica edit a
// su carpeta debian/ (puede ser nil).
func level2Root(t *testing.T, edit func(debian string)) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "packages"), os.DirFS(level2Dir)); err != nil {
		t.Fatal(err)
	}
	// os.CopyFS no conserva el bit de ejecución.
	if err := os.Chmod(filepath.Join(root, "packages/tepache-hello/debian/rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(filepath.Join(root, "packages/tepache-hello/debian"))
	}
	return root
}

// TestLevel2Valid: el paquete de prueba pasa el schema, las reglas
// semánticas y la carpeta debian/ (FR-001, FR-003).
func TestLevel2Valid(t *testing.T) {
	m, err := Parse([]byte(readLevel2(t)), "packages/tepache-hello.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Level2() || m.Source.Format != "tar.gz" || !strings.Contains(m.Justification, "libcrypto") {
		t.Fatalf("decodificación incompleta: %+v", m)
	}
	if err := Check(m, repoConfig(t), nil); err != nil {
		t.Fatal(err)
	}
	if err := CheckDebianDir(level2Root(t, nil), m); err != nil {
		t.Fatal(err)
	}
	if got := m.SuiteVersion("deb12"); got != "1.0.0-0~tepache1+deb12" {
		t.Errorf("SuiteVersion = %s", got)
	}
	if got := m.OrigName(); got != "tepache-hello_1.0.0.orig.tar.gz" {
		t.Errorf("OrigName = %s", got)
	}
}

// TestLevel2Schema: lo que el schema rechaza en nivel 2 y en nivel 1.
func TestLevel2Schema(t *testing.T) {
	base := readLevel2(t)
	cases := map[string]string{
		"sin justificación": strings.Replace(base, "justification: |\n  Links against the system libcrypto, which changes per suite (libssl3 in\n  bookworm, libssl3t64 in trixie); a single upstream binary cannot serve all.\n", "", 1),
		"sin source":        strings.Replace(base, "source:\n  url: https://github.com/opentepache/tepache-hello/archive/refs/tags/{{tag}}.tar.gz\n  format: tar.gz\n", "", 1),
		"formato zip":       strings.Replace(base, "format: tar.gz", "format: zip", 1),
		"nivel 3":           strings.Replace(base, "level: 2", "level: 3", 1),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if src == base {
				t.Fatal("el caso no cambió el manifiesto")
			}
			if _, err := Parse([]byte(src), "packages/tepache-hello.yaml"); err == nil {
				t.Fatal("se esperaba un error del schema")
			}
		})
	}
}

// TestLevelFields: cada nivel rechaza los campos del otro.
func TestLevelFields(t *testing.T) {
	base := readLevel2(t)
	fnm := readExample(t, "fnm")
	cases := map[string]struct{ src, want string }{
		"nivel 2 con artifact":      {base + "artifact: {url: https://github.com/x/y/releases/download/v1/y, format: binary}\n", "artifact: no se admite en nivel 2"},
		"nivel 2 con install":       {base + "install:\n  - {src: tepache-hello, dst: /usr/bin/}\n", "install: no se admite en nivel 2"},
		"nivel 1 con source":        {fnm + "source: {url: https://github.com/Schniz/fnm/archive/refs/tags/v1.39.0.tar.gz, format: tar.gz}\n", "source: solo se admite en nivel 2"},
		"nivel 1 con justification": {fnm + "justification: needs the system libraries of each suite\n", "justification: solo se admite en nivel 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rel := "packages/tepache-hello.yaml"
			if strings.HasPrefix(name, "nivel 1") {
				rel = "packages/fnm.yaml"
			}
			m, err := Parse([]byte(c.src), rel)
			if err != nil {
				t.Fatalf("no pasa el schema: %v", err)
			}
			err = Check(m, repoConfig(t), nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v\nse esperaba %q", err, c.want)
			}
		})
	}
}

// TestLevel2Semantic: reglas de nivel 2 que el schema no expresa.
func TestLevel2Semantic(t *testing.T) {
	base := readLevel2(t)
	cases := map[string]struct{ src, want string }{
		"source fuera de upstream": {strings.Replace(base, "https://github.com/opentepache/tepache-hello/archive", "https://example.com/tepache-hello/archive", 1), "source.url: fuera de los releases y tags"},
		"tag con otro formato":     {strings.Replace(base, "refs/tags/{{tag}}.tar.gz\n  format: tar.gz", "refs/tags/{{tag}}.tar.xz\n  format: tar.xz", 1), "source.url: fuera de los releases y tags"},
		"tmpfs excesivo":           {base + "build: {tmpfs: 64g}\n", "build.tmpfs: 64g supera level2.tmpfs_max"},
		"licencia en tar.xz":       {strings.Replace(base, "archive/refs/tags/{{tag}}.tar.gz\n  format: tar.gz", "releases/download/{{tag}}/tepache-hello-{{version}}.tar.xz\n  format: tar.xz", 1), "license.file.archive: con source.format tar.xz usa license.file.repo"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := Parse([]byte(c.src), "packages/tepache-hello.yaml")
			if err != nil {
				t.Fatalf("no pasa el schema: %v", err)
			}
			err = Check(m, repoConfig(t), nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v\nse esperaba %q", err, c.want)
			}
		})
	}
}

// TestLevel2DebianDir: la carpeta debian/ (FR-003 y clarify: un binario,
// Architecture: any, sin changelog).
func TestLevel2DebianDir(t *testing.T) {
	m, err := Parse([]byte(readLevel2(t)), "packages/tepache-hello.yaml")
	if err != nil {
		t.Fatal(err)
	}
	control := func(old, new string) func(string) {
		return func(d string) {
			p := filepath.Join(d, "control")
			b, _ := os.ReadFile(p)
			if !strings.Contains(string(b), old) {
				t.Fatalf("control no contiene %q", old)
			}
			os.WriteFile(p, []byte(strings.Replace(string(b), old, new, 1)), 0o644)
		}
	}
	cases := map[string]struct {
		edit func(string)
		want string
	}{
		"sin carpeta":      {func(d string) { os.RemoveAll(d) }, "falta la carpeta packages/tepache-hello/debian/"},
		"sin control":      {func(d string) { os.Remove(filepath.Join(d, "control")) }, "falta packages/tepache-hello/debian/control"},
		"sin copyright":    {func(d string) { os.Remove(filepath.Join(d, "copyright")) }, "falta packages/tepache-hello/debian/copyright"},
		"rules sin x":      {func(d string) { os.Chmod(filepath.Join(d, "rules"), 0o644) }, "debian/rules: debe ser ejecutable"},
		"con changelog":    {func(d string) { os.WriteFile(filepath.Join(d, "changelog"), []byte("x"), 0o644) }, "lo genera la herramienta"},
		"enlace":           {func(d string) { os.Symlink("/etc/passwd", filepath.Join(d, "evil")) }, "es un enlace simbólico"},
		"Source distinto":  {control("Source: tepache-hello", "Source: otro"), `Source: "otro" no coincide`},
		"Package distinto": {control("Package: tepache-hello", "Package: otro"), `Package: "otro" no coincide`},
		"dos Package":      {control("Description: prints", "Description: prints\n x\n\nPackage: tepache-hello-doc\nArchitecture: all\nDescription: docs"), "exactamente un paquete binario (hay 2)"},
		"Architecture all": {control("Architecture: any", "Architecture: all"), `Architecture: debe ser any (hay "all")`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := CheckDebianDir(level2Root(t, c.edit), m)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v\nse esperaba %q", err, c.want)
			}
		})
	}
}
