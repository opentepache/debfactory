//go:build e2e

package repotest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/sign"
	"debfactory/internal/testutil"
)

// gpgAt ejecuta gpg con GNUPGHOME=home (pasos con la maestra, que en la
// operación real se hacen en un llavero temporal desde el respaldo).
func gpgAt(t *testing.T, home string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("gpg", append([]string{"--batch", "--no-tty", "--pinentry-mode", "loopback"}, args...)...)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("gpg %v: %v\n%s", args, err, errb.String())
	}
	return out.String()
}

// installSubkey reproduce docs/operacion-llave.md §1 ("Subllave para la PC de
// build" e "Instalar"): desde el llavero maestro, exporta sub con la frase de
// paso de build y la importa en un llavero de build nuevo.
func installSubkey(t *testing.T, masterHome, master, sub string) string {
	t.Helper()
	cer := t.TempDir()
	os.Chmod(cer, 0o700)
	secret := gpgAt(t, masterHome, "", "--passphrase", testutil.TestMasterPassphrase, "--armor", "--export-secret-keys", master)
	gpgAt(t, cer, secret, "--import")
	gpgAt(t, cer, testutil.TestMasterPassphrase+"\n"+testutil.TestPassphrase+"\n", "--command-fd", "0", "--passwd", master)
	subData := gpgAt(t, cer, "", "--passphrase", testutil.TestPassphrase, "--armor", "--export-secret-subkeys", sub+"!")
	home := filepath.Join(t.TempDir(), "gnupg")
	os.Mkdir(home, 0o700)
	gpgAt(t, home, subData, "--import")
	os.WriteFile(filepath.Join(home, "passphrase"), []byte(testutil.TestPassphrase+"\n"), 0o600)
	exec.Command("gpgconf", "--homedir", cer, "--kill", "gpg-agent").Run()
	return home
}

func fprs(t *testing.T, home string) []string {
	var out []string
	for _, l := range strings.Split(gpgAt(t, home, "", "--with-colons", "--list-keys"), "\n") {
		if f := strings.Split(l, ":"); f[0] == "fpr" {
			out = append(out, f[9])
		}
	}
	return out
}

// TestE2EKey recorre docs/operacion-llave.md §1–§4 con una llave desechable
// y un cliente trixie (SC-006, US6).
func TestE2EKey(t *testing.T) {
	ctx := context.Background()
	top := t // los llaveros deben sobrevivir a cada subtest
	e := e2eEnv(t)
	gnupg := sign.Home(e.DataDir)
	master := e.Cfg.Repo.Signing.Masters[0]
	sub1 := e.Cfg.Repo.Signing.Subkey
	real, _ := filepath.EvalSymlinks(gnupg)
	masterHome := testutil.MasterHome(real)

	publishAll(t, ctx, e)
	serveCurrent(t, ctx, e)
	blocks := guide(t, e)
	trixie, _ := e.Cfg.Suite("trixie")
	c, err := StartClient(ctx, trixie, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	addRepo(t, ctx, c, e, blocks)
	var keyLine string
	for _, l := range strings.Split(Render(blocks["add"], e.Cfg.Repo.URL, TestURL, nil), "\n") {
		if strings.Contains(l, "keyrings/debfactory.gpg") && strings.HasPrefix(l, "curl") {
			keyLine = l
		}
	}
	update := func(t *testing.T, refreshKeyring bool) {
		t.Helper()
		script := "apt-get update"
		if refreshKeyring {
			script = keyLine + "\n" + script
		}
		out, err := c.Script(ctx, script)
		if err != nil {
			t.Fatalf("apt-get update: %v", err)
		}
		if bad := AptWarnings(out, TestURL); len(bad) > 0 {
			t.Fatalf("apt avisa:\n%s", strings.Join(bad, "\n"))
		}
	}
	setHome := func(home string) {
		os.Remove(gnupg)
		if err := os.Symlink(home, gnupg); err != nil {
			t.Fatal(err)
		}
	}
	rebuild := func(t *testing.T) {
		t.Helper()
		if err := e.Rebuild(ctx); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}

	t.Run("restaurar desde el respaldo", func(t *testing.T) {
		os.Remove(gnupg) // se pierde el llavero de build
		setHome(installSubkey(top, masterHome, master, sub1))
		rebuild(t)
		update(t, false)
		keys, _ := sign.List(ctx, gnupg, true)
		for _, k := range keys {
			if k.Kind == "sec" && !k.Stub {
				t.Errorf("la maestra está en el llavero de build (US6-3)")
			}
		}
	})

	t.Run("renovar la caducidad", func(t *testing.T) {
		gpgAt(t, masterHome, "", "--passphrase", testutil.TestMasterPassphrase, "--quick-set-expire", master, "3y", sub1)
		gpgAt(t, gnupg, gpgAt(t, masterHome, "", "--armor", "--export", master), "--import")
		rebuild(t)
		update(t, false)
	})

	var sub2 string
	t.Run("rotar la subllave", func(t *testing.T) {
		gpgAt(t, masterHome, "", "--passphrase", testutil.TestMasterPassphrase, "--quick-add-key", master, "ed25519", "sign", "2y")
		all := fprs(t, masterHome)
		sub2 = all[len(all)-1]
		// Transición: keyring con la nueva, firmando aún con la vieja.
		setHome(installSubkey(top, masterHome, master, sub2))
		gpgAt(t, gnupg, gpgAt(t, installSubkey(top, masterHome, master, sub1), "", "--passphrase", testutil.TestPassphrase,
			"--armor", "--export-secret-subkeys", sub1+"!"), "--import")
		rebuild(t)
		update(t, true) // el usuario refresca el keyring
		e.Cfg.Repo.Signing.Subkey = sub2
		rebuild(t)
		update(t, false)
		if e.Cfg.Repo.Signing.Masters[0] != master {
			t.Error("el fingerprint publicado cambió")
		}
	})

	t.Run("revocar la subllave vieja", func(t *testing.T) {
		gpgAt(t, masterHome, "key 1\nrevkey\ny\n0\n\ny\nsave\n",
			"--passphrase", testutil.TestMasterPassphrase, "--command-fd", "0", "--edit-key", master)
		gpgAt(t, gnupg, gpgAt(t, masterHome, "", "--armor", "--export", master), "--import")
		rebuild(t)
		update(t, true)
	})
}
