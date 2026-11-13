package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeDeb construye con dpkg-deb un .deb mínimo y válido para aptly.
func fakeDeb(t testing.TB, name, version, arch string) []byte {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	os.MkdirAll(filepath.Join(root, "DEBIAN"), 0o755)
	os.MkdirAll(filepath.Join(root, "usr/share/doc", name), 0o755)
	os.WriteFile(filepath.Join(root, "usr/share/doc", name, "README"), []byte(name+" "+version+" "+arch+"\n"), 0o644)
	control := "Package: " + name + "\nVersion: " + version + "\nArchitecture: " + arch +
		"\nMaintainer: Test <test@debfactory.invalid>\nDescription: paquete de prueba\n prueba de debfactory\n"
	os.WriteFile(filepath.Join(root, "DEBIAN/control"), []byte(control), 0o644)
	out := filepath.Join(dir, "out.deb")
	cmd := exec.Command("dpkg-deb", "--root-owner-group", "-Zgzip", "-b", root, out)
	cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1700000000")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v\n%s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fakeRelease construye los .deb de amd64 y arm64 de una versión.
func fakeRelease(t testing.TB, name, version string, excluded ...string) release {
	return release{name: name, version: version, excluded: excluded, debs: map[string][]byte{
		"amd64": fakeDeb(t, name, version, "amd64"),
		"arm64": fakeDeb(t, name, version, "arm64"),
	}}
}
