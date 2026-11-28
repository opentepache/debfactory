package supply

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"

	"debfactory/internal/manifest"
)

// TestMinisignReal: la firma real de buf v1.75.0 sobre sha256.txt con su
// llave publicada.
func TestMinisignReal(t *testing.T) {
	dir := "../../testdata/supply/buf"
	key, _ := os.ReadFile(filepath.Join(dir, "minisign.pub"))
	file, sig := filepath.Join(dir, "sha256.txt"), filepath.Join(dir, "sha256.txt.minisig")
	if err := VerifyMinisign(strings.TrimSpace(string(key)), file, sig); err != nil {
		t.Fatal(err)
	}
	// El mismo archivo con un byte cambiado.
	data, _ := os.ReadFile(file)
	data[0] ^= 1
	alt := filepath.Join(t.TempDir(), "sha256.txt")
	os.WriteFile(alt, data, 0o644)
	if err := VerifyMinisign(strings.TrimSpace(string(key)), alt, sig); err == nil || !strings.Contains(err.Error(), "no corresponde") {
		t.Errorf("archivo alterado: %v", err)
	}
}

// minisignPair genera una llave y firma file al estilo minisign (prehash).
func minisignPair(t *testing.T, id byte, file, comment string) (pub, sigPath string) {
	t.Helper()
	pk, sk, _ := ed25519.GenerateKey(nil)
	keyID := []byte{id, 2, 3, 4, 5, 6, 7, 8}
	pub = base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), keyID...), pk...))
	data, _ := os.ReadFile(file)
	h := blake2b.Sum512(data)
	s := ed25519.Sign(sk, h[:])
	global := ed25519.Sign(sk, append(append([]byte{}, s...), comment...))
	body := fmt.Sprintf("untrusted comment: test\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(append(append([]byte("ED"), keyID...), s...)), comment,
		base64.StdEncoding.EncodeToString(global))
	sigPath = file + ".minisig"
	os.WriteFile(sigPath, []byte(body), 0o644)
	return pub, sigPath
}

// TestMinisignRejects: otra llave, comentario alterado, llave mal formada.
func TestMinisignRejects(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.tar.gz")
	os.WriteFile(file, []byte("contenido"), 0o644)
	pub, sig := minisignPair(t, 1, file, "timestamp:1")
	if err := VerifyMinisign(pub, file, sig); err != nil {
		t.Fatal(err)
	}
	other, _ := minisignPair(t, 9, filepath.Join(t.TempDir(), "y"), "c")
	if err := VerifyMinisign(other, file, sig); err == nil || !strings.Contains(err.Error(), "otra llave") {
		t.Errorf("otra llave: %v", err)
	}
	b, _ := os.ReadFile(sig)
	os.WriteFile(sig, []byte(strings.Replace(string(b), "timestamp:1", "timestamp:2", 1)), 0o644)
	if err := VerifyMinisign(pub, file, sig); err == nil || !strings.Contains(err.Error(), "comentario") {
		t.Errorf("comentario alterado: %v", err)
	}
	if err := VerifyMinisign("nope", file, sig); err == nil {
		t.Error("llave mal formada aceptada")
	}
	var se *SignatureError
	if err := VerifyMinisign(other, file, sig); !errors.As(err, &se) {
		t.Errorf("tipo de error: %T", err)
	}
}

// TestGPG: firma separada con una llave generada en el test; otra llave
// fijada falla.
func TestGPG(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("sin gpg")
	}
	home := t.TempDir()
	gpg := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command("gpg", append([]string{"--batch", "--homedir", home, "--pinentry-mode", "loopback", "--passphrase", ""}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("gpg %v: %v\n%s", args, err, out)
		}
		return out
	}
	gpg("--quick-gen-key", "upstream <u@example.invalid>", "ed25519", "sign", "never")
	fpr := ""
	for _, l := range strings.Split(string(gpg("--with-colons", "--list-keys")), "\n") {
		if strings.HasPrefix(l, "fpr:") && fpr == "" {
			fpr = strings.Split(l, ":")[9]
		}
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "packages/p"), 0o755)
	os.WriteFile(filepath.Join(root, "packages/p/upstream.asc"), gpg("--armor", "--export", fpr), 0o644)
	file := filepath.Join(root, "x.tar.gz")
	os.WriteFile(file, []byte("contenido"), 0o644)
	gpg("--detach-sign", "-o", file+".sig", file)
	v := &Verifier{Root: root, Pkg: "p"}
	sig := &manifest.Signing{GPG: &manifest.GPGSig{Fingerprint: fpr, KeyFile: "upstream.asc"}}
	if err := v.Verify(context.Background(), sig, file, file+".sig"); err != nil {
		t.Fatal(err)
	}
	sig.GPG.Fingerprint = strings.Repeat("A", 40)
	if err := v.Verify(context.Background(), sig, file, file+".sig"); err == nil || !strings.Contains(err.Error(), "distinta") {
		t.Errorf("otro fingerprint: %v", err)
	}
}

// TestSigstoreHook: el tipo sigstore usa el verificador inyectado y envuelve
// su error.
func TestSigstoreHook(t *testing.T) {
	var got Origin
	v := &Verifier{Repo: "aquaproj/aqua", Tag: "v2.66.1", Sigstore: func(_ context.Context, _ *manifest.SigstoreSig, o Origin, _, _ string) error {
		got = o
		return errors.New("identidad ajena")
	}}
	err := v.Verify(context.Background(), &manifest.Signing{Sigstore: &manifest.SigstoreSig{}}, "a", "b")
	var se *SignatureError
	if !errors.As(err, &se) || se.Kind != "sigstore" || got != (Origin{"aquaproj/aqua", "v2.66.1"}) {
		t.Fatalf("error = %v, origen %+v", err, got)
	}
}

// TestCosignBlobArgs: con certificados de GitHub Actions se exige además el
// repositorio y el tag de origen (la identidad puede ser un workflow
// reutilizable de otro repositorio, como el de aqua).
func TestCosignBlobArgs(t *testing.T) {
	s := &manifest.SigstoreSig{Identity: "^https://github.com/suzuki-shunsuke/go-release-workflow/", Issuer: githubIssuer}
	args := strings.Join(CosignBlobArgs(s, Origin{"aquaproj/aqua", "v2.66.1"}), " ")
	for _, want := range []string{"--certificate-github-workflow-repository aquaproj/aqua",
		"--certificate-github-workflow-ref refs/tags/v2.66.1", "--certificate-identity-regexp " + s.Identity} {
		if !strings.Contains(args, want) {
			t.Errorf("falta %q en %s", want, args)
		}
	}
	other := &manifest.SigstoreSig{Identity: "x", Issuer: "https://accounts.google.com"}
	if a := strings.Join(CosignBlobArgs(other, Origin{"r", "t"}), " "); strings.Contains(a, "workflow-repository") {
		t.Errorf("otro emisor: %s", a)
	}
}
