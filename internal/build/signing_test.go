package build

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"

	"debfactory/internal/lock"
)

const exampleSigTmpl = "https://github.com/example/example/releases/download/{{tag}}/example-{{arch}}.tar.gz.minisig"

// minisignKey es una llave minisign de prueba.
type minisignKey struct {
	pub string
	sk  ed25519.PrivateKey
	id  []byte
}

func newMinisignKey(id byte) minisignKey {
	pk, sk, _ := ed25519.GenerateKey(nil)
	kid := []byte{id, 1, 2, 3, 4, 5, 6, 7}
	return minisignKey{pub: base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), kid...), pk...)), sk: sk, id: kid}
}

func (k minisignKey) sign(data []byte) []byte {
	h := blake2b.Sum512(data)
	s := ed25519.Sign(k.sk, h[:])
	c := "timestamp:1"
	g := ed25519.Sign(k.sk, append(append([]byte{}, s...), c...))
	return fmt.Appendf(nil, "untrusted comment: test\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(append(append([]byte("ED"), k.id...), s...)), c, base64.StdEncoding.EncodeToString(g))
}

// loadSigned instala el ejemplo con signing.minisign fijado y sirve las
// firmas de signer.
func (h *harness) loadSigned(pinned, signer minisignKey) {
	h.t.Helper()
	h.loadOK()
	p := filepath.Join(h.root, "packages/example.yaml")
	m, _ := os.ReadFile(p)
	m = append(m, fmt.Sprintf("signing:\n  minisign:\n    key: %s\n    file: %s\n", pinned.pub, exampleSigTmpl)...)
	os.WriteFile(p, m, 0o644)
	for _, token := range []string{"x86_64", "aarch64"} {
		data, _ := os.ReadFile(filepath.Join(repo, "testdata/fixtures/ok/example-"+token+".tar.gz"))
		h.serve(exampleURL+token+".tar.gz.minisig", signer.sign(data))
	}
}

// TestLockAndBuildSigned (FR-015): lock verifica la firma fijada y registra
// su sha256; build la vuelve a verificar; otra llave falla antes de construir.
func TestLockAndBuildSigned(t *testing.T) {
	ctx := context.Background()
	key := newMinisignKey(1)
	h := newHarness(t)
	h.loadSigned(key, key)
	if err := h.env.Lock(ctx, "example", true); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	lk, _ := lock.Read(h.root, "example")
	if n := len(lk.Versions["1.0.0"].Signatures); n != 2 {
		t.Fatalf("signatures = %v", lk.Versions["1.0.0"].Signatures)
	}
	if err := h.env.Build(ctx, "example", Options{}); err != nil {
		t.Fatal(err)
	}

	// Otra llave firma: lock falla.
	other := newHarness(t)
	other.loadSigned(key, newMinisignKey(9))
	err := other.env.Lock(ctx, "example", true)
	if err == nil || !strings.Contains(err.Error(), "signature") || !strings.Contains(err.Error(), "otra llave") {
		t.Fatalf("otra llave: %v", err)
	}
	if d := other.distFiles(); len(d) != 0 {
		t.Errorf("dist/ = %v", d)
	}
}
