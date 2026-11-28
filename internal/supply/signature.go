// Package supply verifica la cadena de suministro de upstream
// (specs/009-hosted-hardened-builds): firmas con llave fijada, procedencia,
// tarball contra tag, dependencias del lenguaje y toolchains.
package supply

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/blake2b"

	"debfactory/internal/container"
	"debfactory/internal/manifest"
)

// SignatureError es una firma que no corresponde a la llave fijada (FR-015).
type SignatureError struct {
	Kind, File, Reason string
}

func (e *SignatureError) Error() string {
	return fmt.Sprintf("firma %s de %s: %s", e.Kind, filepath.Base(e.File), e.Reason)
}

// Verifier verifica una firma de upstream. CosignImage es la imagen con
// cosign para los bundles de sigstore (por defecto CosignImageTag); las
// pruebas sustituyen Sigstore.
type Verifier struct {
	// Root es la raíz del repositorio de paquetes (para key_file de GPG).
	Root string
	// Pkg es el nombre del paquete (key_file es relativo a packages/<p>/).
	Pkg string
	// Sigstore verifica un bundle de sigstore; por defecto, cosign en un
	// contenedor de Debian.
	Sigstore func(ctx context.Context, s *manifest.SigstoreSig, file, bundle string) error
}

// Verify comprueba que file está firmado con la llave o identidad fijada en
// sig; sigFile es la firma o el bundle ya descargados.
func (v *Verifier) Verify(ctx context.Context, sig *manifest.Signing, file, sigFile string) error {
	switch sig.Kind() {
	case "minisign":
		return VerifyMinisign(sig.Minisign.Key, file, sigFile)
	case "gpg":
		return v.verifyGPG(ctx, sig.GPG, file, sigFile)
	case "sigstore":
		f := v.Sigstore
		if f == nil {
			f = CosignBlob
		}
		if err := f(ctx, sig.Sigstore, file, sigFile); err != nil {
			return &SignatureError{Kind: "sigstore", File: file, Reason: err.Error()}
		}
		return nil
	}
	return errors.New("signing sin tipo")
}

// VerifyMinisign verifica una firma minisign (Ed25519; "Ed" sobre el
// archivo o "ED" sobre su BLAKE2b-512) y su comentario de confianza, con la
// llave pública fijada en base64.
func VerifyMinisign(pubKey, file, sigFile string) error {
	fail := func(format string, args ...any) error {
		return &SignatureError{Kind: "minisign", File: file, Reason: fmt.Sprintf(format, args...)}
	}
	pk, err := base64.StdEncoding.DecodeString(pubKey)
	if err != nil || len(pk) != 2+8+32 || string(pk[:2]) != "Ed" {
		return fail("llave pública fijada inválida")
	}
	raw, err := os.ReadFile(sigFile)
	if err != nil {
		return fail("%v", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "untrusted comment:") ||
		!strings.HasPrefix(lines[2], "trusted comment: ") {
		return fail("formato de firma inválido")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(sig) != 2+8+64 {
		return fail("firma mal codificada")
	}
	if !bytes.Equal(sig[2:10], pk[2:10]) {
		return fail("firmada con otra llave (id %X, se esperaba %X)", sig[2:10], pk[2:10])
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fail("%v", err)
	}
	msg := data
	switch string(sig[:2]) {
	case "ED":
		h := blake2b.Sum512(data)
		msg = h[:]
	case "Ed":
	default:
		return fail("algoritmo %q desconocido", sig[:2])
	}
	pub := ed25519.PublicKey(pk[10:])
	if !ed25519.Verify(pub, msg, sig[10:]) {
		return fail("la firma no corresponde al archivo")
	}
	trusted := strings.TrimPrefix(lines[2], "trusted comment: ")
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || !ed25519.Verify(pub, append(append([]byte{}, sig[10:]...), trusted...), global) {
		return fail("comentario de confianza alterado")
	}
	return nil
}

// verifyGPG verifica una firma OpenPGP separada con gpgv (de Debian) contra
// la llave de key_file y exige el fingerprint fijado.
func (v *Verifier) verifyGPG(ctx context.Context, g *manifest.GPGSig, file, sigFile string) error {
	fail := func(format string, args ...any) error {
		return &SignatureError{Kind: "gpg", File: file, Reason: fmt.Sprintf(format, args...)}
	}
	keyPath := filepath.Join(v.Root, "packages", v.Pkg, g.KeyFile)
	tmp, err := os.MkdirTemp("", "debfactory-gpgv-*")
	if err != nil {
		return fail("%v", err)
	}
	defer os.RemoveAll(tmp)
	keyring := filepath.Join(tmp, "keyring.gpg")
	dearmor := exec.CommandContext(ctx, "gpg", "--batch", "--homedir", tmp, "--dearmor", "-o", keyring, keyPath)
	if b, err := dearmor.CombinedOutput(); err != nil {
		return fail("llave %s: %v: %s", g.KeyFile, err, b)
	}
	cmd := exec.CommandContext(ctx, "gpgv", "--status-fd", "1", "--keyring", keyring, sigFile, file)
	out, err := cmd.Output()
	if err != nil {
		return fail("gpgv rechaza la firma")
	}
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) >= 3 && f[1] == "VALIDSIG" &&
			(strings.EqualFold(f[2], g.Fingerprint) || len(f) >= 12 && strings.EqualFold(f[11], g.Fingerprint)) {
			return nil
		}
	}
	return fail("firmada por una llave distinta de %s", g.Fingerprint)
}

// CosignImageTag es la imagen local con el cosign de Debian.
const CosignImageTag = "debfactory-cosign"

// CosignBlob verifica un bundle de sigstore con cosign en un contenedor de
// Debian (con red, para la raíz de confianza de sigstore).
func CosignBlob(ctx context.Context, s *manifest.SigstoreSig, file, bundle string) error {
	return runCosign(ctx, file, bundle, []string{"verify-blob", "--new-bundle-format",
		"--certificate-identity-regexp", s.Identity, "--certificate-oidc-issuer", s.Issuer})
}

// runCosign ejecuta cosign <args> --bundle /in/bundle /in/file.
func runCosign(ctx context.Context, file, bundle string, args []string) error {
	if err := container.EnsureImage(ctx, CosignImageTag,
		"FROM debian:trixie\nRUN apt-get update && apt-get install -y --no-install-recommends ca-certificates cosign && rm -rf /var/lib/apt/lists/*\n",
		"", imageMaxAge); err != nil {
		return fmt.Errorf("imagen de cosign: %v", err)
	}
	dir, err := os.MkdirTemp("", "debfactory-cosign-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for src, dst := range map[string]string{file: "file", bundle: "bundle"} {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, dst), data, 0o644); err != nil {
			return err
		}
	}
	cmd := append([]string{"cosign"}, args...)
	cmd = append(cmd, "--bundle", "/in/bundle", "/in/file")
	res, err := container.Run(ctx, container.Opts{Image: CosignImageTag, Network: "bridge",
		Mounts: []container.Mount{{Src: dir, Dst: "/in", ReadOnly: true}}, Env: []string{"HOME=/tmp"}, Cmd: cmd})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.Join(container.TailLines(append(res.Stdout, res.Stderr...), 3), " | "))
	}
	return nil
}
