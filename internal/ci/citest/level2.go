package citest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/srcpkg"
	"debfactory/internal/testutil"
)

// Level2Fixture es el directorio del paquete de prueba de nivel 2
// (specs/008-source-builds, research D14).
func Level2Fixture() string {
	return filepath.Join(testutil.ProjectRoot(), "..", "fixtures", "level2")
}

// InstallLevel2 copia el manifiesto, su lockfile y su carpeta debian/ a
// files (rutas relativas -> contenido), para un GitFixture.
func InstallLevel2(t testing.TB, files map[string]string) {
	t.Helper()
	fix := Level2Fixture()
	for _, rel := range []string{"tepache-hello.yaml", "tepache-hello.lock.yaml"} {
		data, err := os.ReadFile(filepath.Join(fix, rel))
		if err != nil {
			t.Fatal(err)
		}
		files["packages/"+rel] = string(data)
	}
	filepath.WalkDir(filepath.Join(fix, "tepache-hello"), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(fix, p)
			data, _ := os.ReadFile(p)
			files["packages/"+rel] = string(data)
		}
		return nil
	})
}

// FakeDistLevel2 deja en root/dist lo que producen build y test de nivel 2
// para m (sin contenedores): el .orig, el paquete fuente real de cada suite
// no excluida (dpkg-source en el host) y, por arquitectura, un .deb mínimo,
// su .buildinfo, build.json y test.json de esa suite. Devuelve los sha256
// con las claves "<suite>/dsc" y "<suite>/<arch>". Es determinista: dos
// llamadas dan los mismos archivos.
func FakeDistLevel2(t testing.TB, root string, cfg *config.Config, m *manifest.Manifest) map[string]string {
	t.Helper()
	fix := Level2Fixture()
	dist := filepath.Join(root, "dist")
	os.MkdirAll(dist, 0o755)
	tarball := filepath.Join(fix, "tepache-hello-1.0.0.tar.gz")
	orig, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dist, m.OrigName()), orig, 0o644)
	shas := map[string]string{}
	for _, su := range cfg.Suites {
		if _, excluded := m.Exclude.Suites[su.Name]; excluded {
			continue
		}
		v := m.SuiteVersion(su.Level2Suffix)
		in := srcpkg.Input{Name: m.Name, Upstream: m.Version, Version: v, Suite: su.Name, Maintainer: cfg.Maintainer,
			Epoch: 1767225600, Orig: tarball, OrigName: m.OrigName(), DebianDir: filepath.Join(root, m.DebianDir())}
		dsc := hostSource(t, in, dist)
		shas[su.Name+"/dsc"] = dsc
		for _, arch := range cfg.Arches {
			base := filepath.Join(dist, m.Name+"_"+v+"_"+arch)
			r := filepath.Join(t.TempDir(), "root")
			os.MkdirAll(filepath.Join(r, "DEBIAN"), 0o755)
			os.MkdirAll(filepath.Join(r, "usr/share/doc", m.Name), 0o755)
			os.WriteFile(filepath.Join(r, "usr/share/doc", m.Name, "README"), []byte(v+" "+arch+"\n"), 0o644)
			control := "Package: " + m.Name + "\nSource: " + m.Name + "\nVersion: " + v + "\nArchitecture: " + arch +
				"\nMaintainer: Test <test@debfactory.invalid>\nDescription: paquete de prueba\n prueba de nivel 2\n"
			os.WriteFile(filepath.Join(r, "DEBIAN/control"), []byte(control), 0o644)
			cmd := exec.Command("dpkg-deb", "--root-owner-group", "-Zgzip", "-b", r, base+".deb")
			cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1700000000")
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("dpkg-deb: %v\n%s", err, b)
			}
			deb, _ := fetch.SHA256File(base + ".deb")
			binfo := []byte("Format: 1.0\nSource: " + m.Name + "\nVersion: " + v + "\nArchitecture: " + arch + "\n")
			os.WriteFile(base+".buildinfo", binfo, 0o644)
			bi := &pkg.BuildInfo{SHA256: deb, Lintian: "pass", TestCommand: m.Test.Command, Suite: su.Name,
				DSCSHA256: dsc, BuildinfoSHA256: fetch.SHA256(binfo), Reproducible: true}
			b, _ := bi.Marshal()
			os.WriteFile(base+".build.json", b, 0o644)
			rep := installtest.Report{Package: m.Name, Version: v, Arch: arch, DebSHA256: deb, Versions: "pass",
				Overall: "pass", Results: []installtest.Result{{Suite: su.Name, Status: "pass"}}}
			tj, _ := json.MarshalIndent(rep, "", "  ")
			os.WriteFile(base+".test.json", tj, 0o644)
			shas[su.Name+"/"+arch] = deb
		}
	}
	return shas
}

// hostSource genera con dpkg-source del host el paquete fuente de in en dist
// y devuelve el sha256 del .dsc.
func hostSource(t testing.TB, in srcpkg.Input, dist string) string {
	t.Helper()
	work := t.TempDir()
	if err := srcpkg.Prepare(in, filepath.Join(work, "in")); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(work, in.TreeDir())
	os.MkdirAll(tree, 0o755)
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1767225600")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("tar", "-xf", filepath.Join(work, "in", in.OrigName), "-C", tree, "--strip-components=1")
	run("cp", "-r", filepath.Join(work, "in", "debian"), filepath.Join(tree, "debian"))
	run("cp", filepath.Join(work, "in", in.OrigName), work)
	run("dpkg-source", "-b", in.TreeDir())
	for _, f := range []string{in.Name + "_" + in.Version + ".dsc", in.Name + "_" + in.Version + ".debian.tar.xz"} {
		data, err := os.ReadFile(filepath.Join(work, f))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dist, f), data, 0o644)
	}
	sha, _ := fetch.SHA256File(filepath.Join(dist, in.Name+"_"+in.Version+".dsc"))
	return sha
}
