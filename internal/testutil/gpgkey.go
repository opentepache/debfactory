package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPassphrase es la frase de paso de build de las llaves de prueba. Los
// tests de secretos (SC-005) la buscan en la salida.
const TestPassphrase = "debfactory-test-build-passphrase"

// TestMasterPassphrase protege la maestra de las llaves de prueba.
const TestMasterPassphrase = "master-pass"

// MasterHome es el llavero con la maestra de una llave de TestKey (lo que en
// la operación real vive solo en el respaldo fuera de línea).
func MasterHome(home string) string { return filepath.Join(filepath.Dir(home), "m") }

// TestKey crea una llave desechable como indica docs/operacion-llave.md §1:
// maestra ed25519 de certificación y subllave ed25519 de firma (2 años). El
// llavero devuelto (home, 700) solo tiene la subllave, protegida con
// TestPassphrase escrita en home/passphrase (600). Nunca se usa la llave
// real. Requiere gpg.
func TestKey(t testing.TB) (home, master, subkey string) {
	t.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg no está instalado")
	}
	base, err := os.MkdirTemp("", "dfk")
	if err != nil {
		t.Fatal(err)
	}
	masterHome := filepath.Join(base, "m")
	ceremony := filepath.Join(base, "c")
	home = filepath.Join(base, "gnupg")
	for _, d := range []string{masterHome, ceremony, home} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, "gpg-agent.conf"), []byte("allow-loopback-pinentry\n"), 0o600)
	}
	t.Cleanup(func() {
		for _, d := range []string{masterHome, ceremony, home} {
			gpgIn(t, d, nil, "--kill") // gpgconf, ver abajo
		}
		os.RemoveAll(base)
	})

	const mpass = TestMasterPassphrase
	gpgIn(t, masterHome, nil, "--passphrase", mpass, "--pinentry-mode", "loopback",
		"--quick-gen-key", "debfactory test <test@debfactory.invalid>", "ed25519", "cert", "never")
	master = firstFpr(t, masterHome, 0)
	gpgIn(t, masterHome, nil, "--passphrase", mpass, "--pinentry-mode", "loopback",
		"--quick-add-key", master, "ed25519", "sign", "2y")
	subkey = firstFpr(t, masterHome, 1)

	// Ceremonia: copia del llavero maestro, frase de paso de build, exportar
	// solo la subllave.
	secret := gpgIn(t, masterHome, nil, "--passphrase", mpass, "--pinentry-mode", "loopback",
		"--export-secret-keys", master)
	gpgIn(t, ceremony, secret, "--import")
	gpgIn(t, ceremony, []byte(mpass+"\n"+TestPassphrase+"\n"), "--pinentry-mode", "loopback",
		"--command-fd", "0", "--passwd", master)
	sub := gpgIn(t, ceremony, nil, "--passphrase", TestPassphrase, "--pinentry-mode", "loopback",
		"--export-secret-subkeys", subkey+"!")
	gpgIn(t, home, sub, "--import")
	if err := os.WriteFile(filepath.Join(home, "passphrase"), []byte(TestPassphrase+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, master, subkey
}

// gpgIn ejecuta gpg (o gpgconf --kill gpg-agent si args es "--kill") con
// GNUPGHOME=home.
func gpgIn(t testing.TB, home string, stdin []byte, args ...string) []byte {
	t.Helper()
	var cmd *exec.Cmd
	if len(args) == 1 && args[0] == "--kill" {
		cmd = exec.Command("gpgconf", "--kill", "gpg-agent")
	} else {
		cmd = exec.Command("gpg", append([]string{"--batch", "--no-tty"}, args...)...)
	}
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil && cmd.Args[0] != "gpgconf" {
		t.Fatalf("gpg %s: %v\n%s", strings.Join(args, " "), err, errb.String())
	}
	return out.Bytes()
}

// firstFpr devuelve el fingerprint número n del llavero (0 = maestra).
func firstFpr(t testing.TB, home string, n int) string {
	t.Helper()
	out := gpgIn(t, home, nil, "--with-colons", "--list-keys")
	var fprs []string
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Split(l, ":"); f[0] == "fpr" && len(f) > 9 {
			fprs = append(fprs, f[9])
		}
	}
	if n >= len(fprs) {
		t.Fatalf("el llavero tiene %d fingerprints", len(fprs))
	}
	return fprs[n]
}
