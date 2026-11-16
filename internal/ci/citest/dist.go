package citest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/pkg"
)

// FakeDist deja en root/dist un .deb mínimo (dpkg-deb) por arquitectura con
// su build.json y su test.json aprobados en suites (las de excluded, como
// excluidas), como si hubiera pasado los gates 1–6. Devuelve el sha256 por
// arquitectura. Con el mismo nombre, versión y arquitectura el .deb es
// idéntico byte a byte.
func FakeDist(t testing.TB, root, name, version string, arches, suites []string, excluded map[string]string) map[string]string {
	t.Helper()
	dist := filepath.Join(root, "dist")
	os.MkdirAll(dist, 0o755)
	shas := map[string]string{}
	for _, arch := range arches {
		dir := t.TempDir()
		r := filepath.Join(dir, "root")
		os.MkdirAll(filepath.Join(r, "DEBIAN"), 0o755)
		os.MkdirAll(filepath.Join(r, "usr/share/doc", name), 0o755)
		os.WriteFile(filepath.Join(r, "usr/share/doc", name, "README"), []byte(name+" "+version+" "+arch+"\n"), 0o644)
		control := "Package: " + name + "\nVersion: " + version + "\nArchitecture: " + arch +
			"\nMaintainer: Test <test@debfactory.invalid>\nDescription: paquete de prueba\n prueba de debfactory\n"
		os.WriteFile(filepath.Join(r, "DEBIAN/control"), []byte(control), 0o644)
		base := filepath.Join(dist, name+"_"+version+"_"+arch)
		cmd := exec.Command("dpkg-deb", "--root-owner-group", "-Zgzip", "-b", r, base+".deb")
		cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1700000000")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("dpkg-deb: %v\n%s", err, b)
		}
		sha, err := fetch.SHA256File(base + ".deb")
		if err != nil {
			t.Fatal(err)
		}
		bi := &pkg.BuildInfo{SHA256: sha, Lintian: "pass", TestCommand: name + " --version"}
		b, _ := bi.Marshal()
		os.WriteFile(base+".build.json", b, 0o644)
		rep := installtest.Report{Package: name, Version: version, Arch: arch, DebSHA256: sha, Versions: "pass", Overall: "pass"}
		for _, s := range suites {
			if why, ok := excluded[s]; ok {
				rep.Results = append(rep.Results, installtest.Result{Suite: s, Status: "excluded", Reason: why})
				continue
			}
			rep.Results = append(rep.Results, installtest.Result{Suite: s, Status: "pass"})
		}
		tj, _ := json.MarshalIndent(rep, "", "  ")
		os.WriteFile(base+".test.json", tj, 0o644)
		shas[arch] = sha
	}
	return shas
}
