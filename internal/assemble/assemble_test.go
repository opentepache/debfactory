package assemble

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"debfactory/internal/manifest"
	"debfactory/internal/testutil"
)

func writeArchive(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractKeepsExecBit(t *testing.T) {
	entries := []testutil.Entry{
		{Name: "pkg", Dir: true},
		{Name: "pkg/bin/tool", Mode: 0o755, Body: []byte("x")},
		{Name: "pkg/LICENSE", Body: []byte("MIT")},
		{Name: "pkg/link", Link: "bin/tool"},
	}
	for format, data := range map[string][]byte{"tar.gz": testutil.TarGz(entries), "zip": testutil.Zip(entries)} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			if err := Extract(writeArchive(t, "a", data), format, dir, "unused"); err != nil {
				t.Fatal(err)
			}
			for p, want := range map[string]os.FileMode{"pkg/bin/tool": 0o755, "pkg/LICENSE": 0o644} {
				fi, err := os.Stat(filepath.Join(dir, p))
				if err != nil || fi.Mode().Perm() != want {
					t.Errorf("%s: %v %v; se esperaba %04o", p, fi, err, want)
				}
			}
			if target, err := os.Readlink(filepath.Join(dir, "pkg/link")); err != nil || target != "bin/tool" {
				t.Errorf("enlace: %q %v", target, err)
			}
			text, err := ExtractFile(writeArchive(t, "b", data), format, "pkg/LICENSE")
			if err != nil || string(text) != "MIT" {
				t.Errorf("ExtractFile = %q, %v", text, err)
			}
			if _, err := ExtractFile(writeArchive(t, "c", data), format, "pkg/NOPE"); err == nil {
				t.Error("ExtractFile de un miembro inexistente debería fallar")
			}
		})
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	if err := Extract(writeArchive(t, "tool-linux-amd64", []byte("bin")), "binary", dir, "tool"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "tool")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("binario: %v %v", fi, err)
	}
}

func TestExtractRejectsEscapes(t *testing.T) {
	cases := map[string][]testutil.Entry{
		"dotdot":        {{Name: "../evil", Body: []byte("x")}},
		"absolute":      {{Name: "/etc/evil", Body: []byte("x")}},
		"link fuera":    {{Name: "a", Link: "../../etc/passwd"}},
		"link absoluto": {{Name: "a", Link: "/etc/passwd"}},
		"link anidado":  {{Name: "d/a", Link: "../../x"}},
	}
	for name, entries := range cases {
		for format, data := range map[string][]byte{"tar.gz": testutil.TarGz(entries), "zip": testutil.Zip(entries)} {
			t.Run(name+"/"+format, func(t *testing.T) {
				outer := t.TempDir()
				dir := filepath.Join(outer, "x")
				err := Extract(writeArchive(t, "a", data), format, dir, "unused")
				if err == nil {
					t.Fatal("se esperaba error")
				}
				if _, err := os.Stat(filepath.Join(outer, "evil")); err == nil {
					t.Fatal("se escribió fuera del directorio")
				}
			})
		}
	}
}

func extracted(t *testing.T, files map[string]os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	for p, mode := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(p), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func baseManifest(items ...manifest.InstallItem) *manifest.Manifest {
	return &manifest.Manifest{Name: "tool", Version: "1.0.0", Tag: "v{{version}}",
		ArchMap:  map[string]string{"amd64": "amd64", "arm64": "arm64"},
		Artifact: manifest.Artifact{Format: "tar.gz"}, Install: items}
}

func TestInstallRenameAndModes(t *testing.T) {
	dir := extracted(t, map[string]os.FileMode{
		"tool-linux-amd64":    0o755,
		"bob-linux-amd64/bob": 0o644, // como el zip de bob: sin bit ejecutable
		"doc/README":          0o600,
	})
	m := baseManifest(
		manifest.InstallItem{Src: "tool-linux-{{arch}}", Dst: "/usr/bin/tool"},
		manifest.InstallItem{Src: "bob-linux-{{arch}}/bob", Dst: "/usr/bin/", Mode: "0755"},
		manifest.InstallItem{Src: "doc/README", Dst: "/usr/share/doc/tool/"},
	)
	files, err := Install(m, "amd64", dir)
	if err != nil {
		t.Fatal(err)
	}
	files, _ = Sorted(files)
	got := []string{}
	for _, f := range files {
		got = append(got, f.Dst+" "+f.Mode.String())
	}
	want := []string{"/usr/bin/bob -rwxr-xr-x", "/usr/bin/tool -rwxr-xr-x", "/usr/share/doc/tool/README -rw-r--r--"}
	if !slices.Equal(got, want) {
		t.Errorf("Install = %v\nse esperaba %v", got, want)
	}
}

func TestInstallGlobs(t *testing.T) {
	dir := extracted(t, map[string]os.FileMode{
		"buf/bin/buf": 0o755, "buf/bin/protoc-gen-buf-lint": 0o755, "buf/bin/protoc-gen-buf-breaking": 0o755,
	})
	files, err := Install(baseManifest(manifest.InstallItem{Src: "buf/bin/*", Dst: "/usr/bin/"}), "amd64", dir)
	if err != nil || len(files) != 3 {
		t.Fatalf("Install = %v, %v", files, err)
	}
	_, err = Install(baseManifest(manifest.InstallItem{Src: "buf/man/*.1", Dst: "/usr/bin/"}), "amd64", dir)
	if err == nil || !strings.Contains(err.Error(), `assemble: install[0].src: el patrón "buf/man/*.1" no seleccionó ningún archivo`) {
		t.Errorf("error = %v", err)
	}
	_, err = Install(baseManifest(manifest.InstallItem{Src: "buf/bin/*", Dst: "/usr/bin/buf"}), "amd64", dir)
	if err == nil || !strings.Contains(err.Error(), `dst debe terminar en "/"`) {
		t.Errorf("error = %v", err)
	}
	files, _ = Install(baseManifest(
		manifest.InstallItem{Src: "buf/bin/*", Dst: "/usr/bin/"},
		manifest.InstallItem{Src: "buf/bin/buf", Dst: "/usr/bin/"},
	), "amd64", dir)
	if _, err := Sorted(files); err == nil || !strings.Contains(err.Error(), "destino duplicado /usr/bin/buf") {
		t.Errorf("error = %v", err)
	}
}

func TestInstallMissing(t *testing.T) {
	dir := extracted(t, map[string]os.FileMode{"x": 0o755})
	_, err := Install(baseManifest(manifest.InstallItem{Src: "fnm", Dst: "/usr/bin/"}), "amd64", dir)
	if err == nil || !strings.Contains(err.Error(), `install[0].src: no existe "fnm" en el artefacto`) {
		t.Errorf("error = %v", err)
	}
}

func TestELFDepends(t *testing.T) {
	mapping := map[string]string{
		"libc.so.6": "libc6 (>= {glibc})", "libm.so.6": "libc6 (>= {glibc})",
		"libgcc_s.so.1": "libgcc-s1", "libselinux.so.1": "libselinux1",
		"libpcre2-8.so.0": "libpcre2-8-0", "libcap.so.2": "libcap2",
	}
	static := filepath.Join(t.TempDir(), "static")
	os.WriteFile(static, testutil.TinyELF("amd64", "hola\n"), 0o755)
	text := filepath.Join(t.TempDir(), "README")
	os.WriteFile(text, []byte("no soy ELF"), 0o644)

	deps, err := ELFDepends([]File{{Dst: "/usr/bin/static", Src: static}, {Dst: "/usr/share/doc/x/README", Src: text}}, mapping)
	if err != nil || len(deps) != 0 {
		t.Errorf("estático: %v, %v", deps, err)
	}
	if _, err := os.Stat("/usr/bin/ls"); err != nil {
		t.Skip("sin /usr/bin/ls")
	}
	deps, err = ELFDepends([]File{{Dst: "/usr/bin/ls", Src: "/usr/bin/ls"}}, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(deps, func(d string) bool { return strings.HasPrefix(d, "libc6 (>= 2.") }) {
		t.Errorf("Depends de ls = %v", deps)
	}
	delete(mapping, "libselinux.so.1")
	_, err = ELFDepends([]File{{Dst: "/usr/bin/ls", Src: "/usr/bin/ls"}}, mapping)
	if err == nil || !strings.Contains(err.Error(), "/usr/bin/ls necesita libselinux.so.1") {
		t.Errorf("error = %v", err)
	}
}

func TestMergeDepends(t *testing.T) {
	got := MergeDepends([]string{"libc6 (>= 2.39)", "libgcc-s1"}, []string{"ca-certificates", "libc6 (>= 2.40)"})
	want := []string{"ca-certificates", "libc6 (>= 2.40)", "libgcc-s1"}
	if !slices.Equal(got, want) {
		t.Errorf("MergeDepends = %v", got)
	}
}

func TestVersionLess(t *testing.T) {
	if !versionLess("2.4", "2.39") || versionLess("2.39", "2.4") || !versionLess("", "2.2.5") {
		t.Error("orden de versiones incorrecto")
	}
}
