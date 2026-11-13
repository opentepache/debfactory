//go:build e2e

package repotest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"debfactory/internal/testutil"
)

// Un bloque de llave privada con contenido (no solo el marcador).
var armoredSecret = regexp.MustCompile(`-----BEGIN PGP PRIVATE[ ]KEY BLOCK-----\r?\n(\r?\n)?([A-Za-z0-9+/=]{20,}\r?\n){2,}`)

// Credenciales con valor (no el marcador "…" de los ejemplos).
var credential = regexp.MustCompile(`(?m)^\+?\s*(secret_access_key|access_key_id)\s*=\s*[A-Za-z0-9/+]{8,}`)

// secretPacket indica si data empieza como un paquete OpenPGP binario de
// llave secreta (etiquetas 5 y 7) y gpg lo confirma.
func secretPacket(t *testing.T, path string, data []byte) bool {
	if len(data) == 0 || data[0]&0x80 == 0 {
		return false
	}
	tag := int(data[0] & 0x3f)
	if data[0]&0x40 == 0 {
		tag = int(data[0]>>2) & 0x0f
	}
	if tag != 5 && tag != 7 {
		return false
	}
	cmd := exec.Command("gpg", "--batch", "--list-packets", path)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+t.TempDir())
	out, _ := cmd.CombinedOutput()
	return bytes.Contains(out, []byte("secret key packet")) || bytes.Contains(out, []byte("secret sub key packet"))
}

// TestE2ESecrets (SC-005): ni la salida de las órdenes de los E2E de este
// proceso, ni los archivos versionados, ni el historial de git contienen
// material de llave secreta ni frases de paso. Corre al final (zz_).
func TestE2ESecrets(t *testing.T) {
	captured.Lock()
	out := captured.String()
	captured.Unlock()
	for _, s := range []string{testutil.TestPassphrase, testutil.TestMasterPassphrase} {
		if strings.Contains(out, s) {
			t.Errorf("la salida de las órdenes contiene una frase de paso")
		}
	}
	if armoredSecret.MatchString(out) {
		t.Error("la salida de las órdenes contiene un bloque de llave privada")
	}
	t.Logf("salida revisada: %d bytes", len(out))

	files, err := exec.Command("git", "-C", realRoot, "ls-files").Output()
	if err != nil {
		t.Skipf("git: %v", err)
	}
	n := 0
	for _, f := range strings.Split(strings.TrimSpace(string(files)), "\n") {
		p := filepath.Join(realRoot, f)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		n++
		if armoredSecret.Match(data) || secretPacket(t, p, data) {
			t.Errorf("%s contiene material de llave secreta", f)
		}
		if credential.Match(data) {
			t.Errorf("%s contiene una credencial", f)
		}
	}
	hist, err := exec.Command("git", "-C", realRoot, "log", "-p", "--all").Output()
	if err != nil {
		t.Fatal(err)
	}
	if armoredSecret.Match(hist) {
		t.Error("el historial de git contiene un bloque de llave privada")
	}
	if credential.Match(hist) {
		t.Error("el historial de git contiene una credencial")
	}
	t.Logf("archivos versionados revisados: %d; historial: %d bytes", n, len(hist))
}
