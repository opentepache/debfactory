package supply

import (
	"archive/tar"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/testutil"
)

// TestFetchToolchain: sha256 y firma minisign del toolchain; firma de otra
// llave o sha distinto fallan; la extracción deja el binario arriba.
func TestFetchToolchain(t *testing.T) {
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	tw.WriteHeader(&tar.Header{Name: "zig-x86_64-linux-0.17.0/", Mode: 0o755, Typeflag: tar.TypeDir})
	body := "#!/bin/sh\necho zig\n"
	tw.WriteHeader(&tar.Header{Name: "zig-x86_64-linux-0.17.0/zig", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	tarball := tb.Bytes()
	tmp := t.TempDir()
	tf := filepath.Join(tmp, "zig.tar")
	os.WriteFile(tf, tarball, 0o644)
	pub, sigPath := minisignPair(t, 3, tf, "timestamp:1")
	sig, _ := os.ReadFile(sigPath)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".minisig"):
			w.Write(sig)
		case strings.HasSuffix(r.URL.Path, ".tar.xz"):
			w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := fetch.New(t.TempDir(), time.Minute, testutil.Redirect{Server: srv}).WithHosts("ziglang.org")
	tc := &manifest.Toolchain{Name: "zig", Version: "0.17.0", URL: "https://ziglang.org/download/{{version}}/zig-{{arch}}-linux-{{version}}.tar.xz",
		ArchMap: map[string]string{"amd64": "x86_64", "arm64": "aarch64"},
		Signing: &manifest.Signing{Minisign: &manifest.MinisignSig{Key: pub, File: "https://ziglang.org/download/{{version}}/zig-{{arch}}-linux-{{version}}.tar.xz.minisig"}}}
	ctx := context.Background()
	r, err := FetchToolchain(ctx, f, &Verifier{}, tc, "amd64", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != "https://ziglang.org/download/0.17.0/zig-x86_64-linux-0.17.0.tar.xz" || r.SigSHA == "" {
		t.Errorf("resultado = %+v", r)
	}
	// Con el sha del lockfile: sale de la caché y vuelve a verificar la firma.
	if _, err := FetchToolchain(ctx, f, &Verifier{}, tc, "amd64", r.SHA256, r.SigSHA); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := ExtractToolchain(ctx, r.Tarball, dst); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(ToolchainBinary(dst, tc)); err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Errorf("binario: %v", err)
	}
	// Otra llave fijada.
	other, _ := minisignPair(t, 7, filepath.Join(tmp, "otro"), "c")
	tc.Signing.Minisign.Key = other
	if _, err := FetchToolchain(ctx, fetch.New(t.TempDir(), time.Minute, testutil.Redirect{Server: srv}).WithHosts("ziglang.org"), &Verifier{}, tc, "amd64", "", ""); err == nil {
		t.Error("otra llave aceptada")
	}
	// sha del lockfile distinto.
	tc.Signing.Minisign.Key = pub
	if _, err := FetchToolchain(ctx, fetch.New(t.TempDir(), time.Minute, testutil.Redirect{Server: srv}).WithHosts("ziglang.org"), &Verifier{}, tc, "amd64", strings.Repeat("0", 64), ""); err == nil {
		t.Error("sha distinto aceptado")
	}
}
