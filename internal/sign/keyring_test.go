//go:build integration

package sign

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

func cfgWith(t *testing.T, master, sub string) *config.Config {
	t.Helper()
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repo.Signing.Masters = []string{master}
	cfg.Repo.Signing.Subkey = sub
	return cfg
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	home, master, sub := testutil.TestKey(t)
	cfg := cfgWith(t, master, sub)
	if err := Check(ctx, cfg, home); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := TestSign(ctx, home, sub); err != nil {
		t.Fatalf("TestSign: %v", err)
	}
	exp, err := Expiry(ctx, home, sub)
	if err != nil || exp.Before(time.Now().AddDate(1, 11, 0)) {
		t.Errorf("Expiry = %v, %v", exp, err)
	}
	dst := filepath.Join(t.TempDir(), "k.gpg")
	if err := ExportKeyring(ctx, home, []string{master}, dst); err != nil {
		t.Fatal(err)
	}

	t.Run("otra subllave", func(t *testing.T) {
		c := cfgWith(t, master, strings.Repeat("A", 40))
		err := Check(ctx, c, home)
		if err == nil || !strings.Contains(err.Error(), "sign: gpg: no hay llave secreta") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("maestra presente", func(t *testing.T) {
		h2, m2, s2 := testutil.TestKey(t)
		secret, err := Run(ctx, testutil.MasterHome(h2), "--pinentry-mode", "loopback",
			"--passphrase", testutil.TestMasterPassphrase, "--export-secret-keys", m2)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("gpg", "--batch", "--import")
		cmd.Env = append(os.Environ(), "GNUPGHOME="+h2)
		cmd.Stdin = bytes.NewReader(secret)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		err = Check(ctx, cfgWith(t, m2, s2), h2)
		if err == nil || !strings.Contains(err.Error(), "la llave maestra no debe estar en la PC de build") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("permisos de passphrase", func(t *testing.T) {
		os.Chmod(PassphraseFile(home), 0o644)
		defer os.Chmod(PassphraseFile(home), 0o600)
		var env *report.EnvError
		if err := Check(ctx, cfg, home); !errors.As(err, &env) {
			t.Errorf("err = %v, se esperaba EnvError", err)
		}
	})
}

// TestExportTwoMasters: durante una rotación de la maestra el keyring
// publicado contiene las dos (FR-022).
func TestExportTwoMasters(t *testing.T) {
	ctx := context.Background()
	home, m1, _ := testutil.TestKey(t)
	home2, m2, _ := testutil.TestKey(t)
	pub2, err := Run(ctx, home2, "--export", m2)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("gpg", "--batch", "--import")
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	cmd.Stdin = bytes.NewReader(pub2)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	dst := filepath.Join(t.TempDir(), "k.gpg")
	if err := ExportKeyring(ctx, home, []string{m1, m2}, dst); err != nil {
		t.Fatal(err)
	}
	show := exec.Command("gpg", "--batch", "--with-colons", "--show-keys", dst)
	show.Env = append(os.Environ(), "GNUPGHOME="+t.TempDir())
	out, err := show.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{m1, m2} {
		if !strings.Contains(string(out), m) {
			t.Errorf("el keyring no contiene %s", m)
		}
	}
}
