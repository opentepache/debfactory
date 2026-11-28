package verify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/fetch"
	"debfactory/internal/supply"
	"debfactory/internal/testutil"
)

const debName = "bob_4.3.0-0~tepache1_amd64.deb"

type repoFixture struct {
	t     *testing.T
	files map[string][]byte
	srv   *httptest.Server
	home  string
}

func gpg(t *testing.T, home string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("gpg", append([]string{"--homedir", home, "--batch", "--yes"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gpg %v: %v\n%s", args, err, out)
	}
	return out
}

// newRepo publica un repositorio APT mínimo y firmado con un .deb y su
// provenance.json.
func newRepo(t *testing.T, deb []byte, provDigest string, run *supply.Run) *repoFixture {
	t.Helper()
	home, master, _ := testutil.TestKey(t)
	f := &repoFixture{t: t, files: map[string][]byte{}, home: home}
	debSHA := fetch.SHA256(deb)
	f.files["/pool/main/b/bob/"+debName] = deb
	pkgs := fmt.Sprintf("Package: bob\nVersion: 4.3.0-0~tepache1\nArchitecture: amd64\nFilename: pool/main/b/bob/%s\nSize: %d\nSHA256: %s\n\n",
		debName, len(deb), debSHA)
	f.files["/dists/trixie/main/binary-amd64/Packages"] = []byte(pkgs)
	release := fmt.Sprintf("Origin: test\nSuite: trixie\nSHA256:\n %s %d main/binary-amd64/Packages\n", fetch.SHA256([]byte(pkgs)), len(pkgs))
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Release"), []byte(release), 0o644)
	gpg(t, home, "--pinentry-mode", "loopback", "--passphrase-file", filepath.Join(home, "passphrase"),
		"--clearsign", "-o", filepath.Join(dir, "InRelease"), filepath.Join(dir, "Release"))
	f.files["/dists/trixie/InRelease"], _ = os.ReadFile(filepath.Join(dir, "InRelease"))
	f.files["/keyring.gpg"] = gpg(t, home, "--export", master)
	f.files["/site.json"] = []byte(fmt.Sprintf(`{"repo":{"keyring":{"path":"keyring.gpg","fingerprints":[%q]}}}`, master))
	runJSON := "null"
	if run != nil {
		runJSON = fmt.Sprintf(`{"repo":%q,"workflow":%q,"ref":%q,"commit":%q}`, run.Repo, run.Workflow, run.Ref, run.Commit)
	}
	f.files["/packages/bob/4.3.0-0~tepache1/provenance.json"] = []byte(fmt.Sprintf(`{"package":"bob","repository":"opentepache/packages",
"version":"4.3.0-0~tepache1","level":1,"manifest_commit":"%s","run":%s,
"attestations":[{"arch":"amd64","subject":%q,"digest":%q}],"tests":[{"suite":"trixie","arch":"amd64","status":"pass"}]}`,
		strings.Repeat("a", 40), runJSON, debName, provDigest))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := f.files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *repoFixture) opts() Options {
	return Options{Site: f.srv.URL, Package: "bob", Version: "4.3.0-0~tepache1", NoRebuild: true, Workdir: f.t.TempDir()}
}

// TestVerify (specs/009, T037): cadena firmada InRelease → Packages → .deb,
// digest de la procedencia, atestación y reconstrucción; cualquier eslabón
// alterado da "no verificado" con su motivo.
func TestVerify(t *testing.T) {
	ctx := context.Background()
	deb := []byte("contenido del .deb")
	f := newRepo(t, deb, fetch.SHA256(deb), nil)
	res, err := Run(ctx, f.opts())
	if err != nil || !res.Verified || len(res.Steps) != 3 {
		t.Fatalf("verificado: %+v, %v", res, err)
	}

	// .deb alterado en el servidor.
	f.files["/pool/main/b/bob/"+debName] = []byte("otro")
	if res, _ := Run(ctx, f.opts()); res.Verified || !strings.Contains(res.Reason, "índice firmado") {
		t.Errorf("deb alterado: %+v", res)
	}
	f.files["/pool/main/b/bob/"+debName] = deb

	// Reconstrucción con otro resultado y con el mismo.
	o := f.opts()
	o.NoRebuild = false
	o.Rebuild = func(context.Context, *Provenance, string, string, string) (string, error) { return "otro", nil }
	if res, _ := Run(ctx, o); res.Verified || !strings.Contains(res.Reason, "reconstrucción da") {
		t.Errorf("reconstrucción distinta: %+v", res)
	}
	o.Rebuild = func(_ context.Context, pv *Provenance, suite, arch, _ string) (string, error) {
		if suite != "trixie" || arch != "amd64" || pv.ManifestCommit != strings.Repeat("a", 40) {
			t.Errorf("rebuild(%s, %s, %s)", suite, arch, pv.ManifestCommit)
		}
		return fetch.SHA256(deb), nil
	}
	if res, _ := Run(ctx, o); !res.Verified {
		t.Errorf("reconstrucción igual: %+v", res)
	}

	// Keyring propio sin la llave que firmó: la firma no verifica.
	other, _, _ := testutil.TestKey(t)
	kr := filepath.Join(t.TempDir(), "other.gpg")
	os.WriteFile(kr, gpg(t, other, "--export"), 0o644)
	o = f.opts()
	o.Keyring = kr
	if res, _ := Run(ctx, o); res.Verified || !strings.Contains(res.Reason, "InRelease") {
		t.Errorf("otra llave: %+v", res)
	}
}

// TestVerifyProvenanceAndAttestation: digest distinto en la procedencia y
// atestación que no verifica.
func TestVerifyProvenanceAndAttestation(t *testing.T) {
	ctx := context.Background()
	deb := []byte("contenido del .deb")
	f := newRepo(t, deb, strings.Repeat("0", 64), nil)
	if res, _ := Run(ctx, f.opts()); res.Verified || !strings.Contains(res.Reason, "no tiene el .deb de la procedencia") {
		t.Errorf("procedencia: %+v", res)
	}
	run := &supply.Run{Repo: "opentepache/packages", Workflow: ".github/workflows/main.yml", Ref: "refs/heads/main", Commit: "c0ffee"}
	g := newRepo(t, deb, fetch.SHA256(deb), run)
	o := g.opts()
	var got supply.Run
	o.Attest = func(_ context.Context, r supply.Run, _, _ string) error { got = r; return errors.New("otro workflow") }
	if res, _ := Run(ctx, o); res.Verified || !strings.Contains(res.Reason, "atestación") || got != *run {
		t.Errorf("atestación: %+v (run %+v)", res, got)
	}
}
