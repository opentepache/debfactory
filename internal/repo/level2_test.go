//go:build integration

package repo

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/pkg"
	"debfactory/internal/srcpkg"
)

const level2Fix = "../../testdata/fixtures/level2"

// installLevel2 copia el manifiesto de prueba (con exclude.suites ajustado a
// excluded) y deja en dist/ lo que producen build y test de nivel 2 para las
// suites no excluidas: el .orig, el paquete fuente real (dpkg-source en el
// host) y, por arquitectura, un .deb mínimo, su .buildinfo, build.json y
// test.json de esa suite.
func installLevel2(t *testing.T, e *Env, excluded ...string) {
	t.Helper()
	pk := filepath.Join(e.Root, "packages")
	os.MkdirAll(pk, 0o755)
	if err := os.CopyFS(filepath.Join(pk, "tepache-hello"), os.DirFS(filepath.Join(level2Fix, "tepache-hello"))); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(pk, "tepache-hello/debian/rules"), 0o755)
	man, _ := os.ReadFile(filepath.Join(level2Fix, "tepache-hello.yaml"))
	i := strings.Index(string(man), "exclude:")
	body := string(man[:i]) + "exclude:\n  suites:\n"
	for _, s := range excluded {
		body += "    " + s + ": fuera de la prueba de integración\n"
	}
	if len(excluded) == 0 {
		body = string(man[:i])
	}
	os.WriteFile(filepath.Join(pk, "tepache-hello.yaml"), []byte(body), 0o644)
	lockData, _ := os.ReadFile(filepath.Join(level2Fix, "tepache-hello.lock.yaml"))
	os.WriteFile(filepath.Join(pk, "tepache-hello.lock.yaml"), lockData, 0o644)

	dist := filepath.Join(e.Root, "dist")
	os.MkdirAll(dist, 0o755)
	orig, _ := os.ReadFile(filepath.Join(level2Fix, "tepache-hello-1.0.0.tar.gz"))
	os.WriteFile(filepath.Join(dist, "tepache-hello_1.0.0.orig.tar.gz"), orig, 0o644)
	for _, su := range e.Cfg.Suites {
		if slices.Contains(excluded, su.Name) {
			continue
		}
		v := "1.0.0-0~tepache1+" + su.Level2Suffix
		dscSHA := hostSource(t, e.Cfg, su, v, dist)
		for _, arch := range e.Cfg.Arches {
			base := filepath.Join(dist, "tepache-hello_"+v+"_"+arch)
			deb := fakeDeb(t, "tepache-hello", v, arch)
			os.WriteFile(base+".deb", deb, 0o644)
			binfo := []byte("Format: 1.0\nSource: tepache-hello\nVersion: " + v + "\nArchitecture: " + arch + "\n")
			os.WriteFile(base+".buildinfo", binfo, 0o644)
			bi := &pkg.BuildInfo{SHA256: fetch.SHA256(deb), Lintian: "pass", TestCommand: "tepache-hello",
				Suite: su.Name, DSCSHA256: dscSHA, BuildinfoSHA256: fetch.SHA256(binfo), Reproducible: true}
			b, _ := bi.Marshal()
			os.WriteFile(base+".build.json", b, 0o644)
			rep := installtest.Report{Package: "tepache-hello", Version: v, Arch: arch, DebSHA256: fetch.SHA256(deb),
				Versions: "pass", Overall: "pass", Results: []installtest.Result{{Suite: su.Name, Status: "pass"}}}
			tj, _ := json.MarshalIndent(rep, "", "  ")
			os.WriteFile(base+".test.json", tj, 0o644)
		}
	}
}

// hostSource genera con dpkg-source el paquete fuente de una suite en dist/
// y devuelve el sha256 del .dsc.
func hostSource(t *testing.T, cfg *config.Config, su config.Suite, version, dist string) string {
	t.Helper()
	in := srcpkg.Input{Name: "tepache-hello", Upstream: "1.0.0", Version: version, Suite: su.Name,
		Maintainer: cfg.Maintainer, Epoch: 1767225600, Orig: filepath.Join(level2Fix, "tepache-hello-1.0.0.tar.gz"),
		OrigName: "tepache-hello_1.0.0.orig.tar.gz", DebianDir: filepath.Join(level2Fix, "tepache-hello", "debian")}
	work := t.TempDir()
	if err := srcpkg.Prepare(in, filepath.Join(work, "in")); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(work, in.TreeDir())
	os.MkdirAll(tree, 0o755)
	run := func(dir string, args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1767225600")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run(work, "tar", "-xf", filepath.Join(work, "in", in.OrigName), "-C", tree, "--strip-components=1")
	run(work, "cp", "-r", filepath.Join(work, "in", "debian"), filepath.Join(tree, "debian"))
	run(work, "cp", filepath.Join(work, "in", in.OrigName), work)
	run(work, "dpkg-source", "-b", in.TreeDir())
	for _, f := range []string{"tepache-hello_" + version + ".dsc", "tepache-hello_" + version + ".debian.tar.xz"} {
		data, err := os.ReadFile(filepath.Join(work, f))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dist, f), data, 0o644)
	}
	sha, _ := fetch.SHA256File(filepath.Join(dist, "tepache-hello_"+version+".dsc"))
	return sha
}

// TestPublishLevel2 (US2, FR-009–FR-011, SC-005): fuente y binarios por suite
// junto a un paquete de nivel 1; el .orig es un solo archivo en el pool;
// Sources y los .buildinfo coinciden con el estado (VerifyTree, dentro de
// Publish); publicar otra vez no cambia nada.
func TestPublishLevel2(t *testing.T) {
	ctx := context.Background()
	e, out := testEnv(t)
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.0.0-0~tepache1"))
	installLevel2(t, e, "forky", "sid", "resolute")
	versions := map[string]string{"foo": "1.0.0-0~tepache1", "tepache-hello": "1.0.0-0~tepache1"}
	if err := e.Publish(ctx, []string{"foo", "tepache-hello"}, versions, false); err != nil {
		t.Fatalf("publish: %v\n%s", err, out)
	}
	st, err := ReadState(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Validate(e.Cfg); err != nil {
		t.Fatal(err)
	}
	p, ok := st.Find("tepache-hello", "1.0.0-0~tepache1")
	if !ok || !p.Level2() || !slices.Equal(p.Suites, []string{"bookworm", "trixie", "noble"}) {
		t.Fatalf("estado de nivel 2: %+v", p)
	}
	data, _ := os.ReadFile(filepath.Join(e.Root, StateFile))
	if !strings.Contains(string(data), "\nschema: 2\n") {
		t.Errorf("se esperaba schema 2:\n%s", data)
	}
	cur, _ := Current(e.DataDir)
	files := poolFiles(t, cur.Dir(e.DataDir))
	count := func(suffix string) int {
		n := 0
		for _, f := range files {
			if strings.HasSuffix(f, suffix) {
				n++
			}
		}
		return n
	}
	if count(".orig.tar.gz") != 1 || count(".dsc") != 3 || count(".debian.tar.xz") != 3 ||
		count(".buildinfo") != 6 || count(".deb") != 2+6 {
		t.Errorf("pool = %v", files)
	}
	sources, err := readIndex(filepath.Join(cur.Dir(e.DataDir), "dists", "trixie", "main", "source", "Sources"))
	if err != nil || !strings.Contains(string(sources), "Version: 1.0.0-0~tepache1+deb13") {
		t.Errorf("Sources de trixie: %v\n%s", err, sources)
	}
	// Publicar otra vez: sin cambios (las versiones no se reescriben).
	out.Reset()
	if err := e.Publish(ctx, []string{"tepache-hello"}, versions, false); err != nil {
		t.Fatalf("republicar: %v\n%s", err, out)
	}
	if again, _ := ReadState(e.Root); again.Serial != st.Serial {
		t.Errorf("republicar cambió el serial: %d → %d", st.Serial, again.Serial)
	}
}

// TestExtendLevel2 (FR-015): al dejar de excluir una suite se publica su
// compilación y las demás no cambian.
func TestExtendLevel2(t *testing.T) {
	ctx := context.Background()
	e, out := testEnv(t)
	installLevel2(t, e, "forky", "sid", "noble", "resolute")
	versions := map[string]string{"tepache-hello": "1.0.0-0~tepache1"}
	if err := e.Publish(ctx, []string{"tepache-hello"}, versions, false); err != nil {
		t.Fatalf("publish: %v\n%s", err, out)
	}
	before, _ := ReadState(e.Root)
	installLevel2(t, e, "forky", "sid", "resolute")
	if err := e.Publish(ctx, []string{"tepache-hello"}, versions, false); err != nil {
		t.Fatalf("ampliar: %v\n%s", err, out)
	}
	after, _ := ReadState(e.Root)
	p, _ := after.Find("tepache-hello", "1.0.0-0~tepache1")
	old, _ := before.Find("tepache-hello", "1.0.0-0~tepache1")
	if !slices.Equal(p.Suites, []string{"bookworm", "trixie", "noble"}) || p.Builds["noble"].Version != "1.0.0-0~tepache1+u2404" {
		t.Fatalf("ampliación: %+v", p)
	}
	for _, su := range []string{"bookworm", "trixie"} {
		if !slices.Equal(differentArtifacts(old, Published{Level: 2, Orig: p.Orig, Suites: []string{su},
			Builds: map[string]Build{su: p.Builds[su]}}), nil) {
			t.Errorf("%s cambió al ampliar", su)
		}
	}
}

// TestInputsLevel2Incomplete: si falta la prueba de una suite no se publica
// en ninguna (constitución VII).
func TestInputsLevel2Incomplete(t *testing.T) {
	e, _ := testEnv(t)
	installLevel2(t, e, "forky", "sid", "resolute")
	os.Remove(filepath.Join(e.Root, "dist", "tepache-hello_1.0.0-0~tepache1+u2404_arm64.test.json"))
	_, err := Inputs(e.Root, e.Cfg, "tepache-hello", "1.0.0-0~tepache1")
	if err == nil || !strings.Contains(err.Error(), "noble/arm64") || !strings.Contains(err.Error(), "test.json") {
		t.Fatalf("error = %v", err)
	}
}
