//go:build integration

package repo

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

// TestAptlyMultiKey comprueba si aptly firma con dos subllaves a la vez
// (research §3, riesgo 1; rotación de la llave maestra).
func TestAptlyMultiKey(t *testing.T) {
	ctx := context.Background()
	home, m1, s1 := testutil.TestKey(t)
	home2, m2, s2 := testutil.TestKey(t)
	sub2, err := sign.Run(ctx, home2, "--pinentry-mode", "loopback", "--passphrase", testutil.TestPassphrase,
		"--export-secret-subkeys", s2+"!")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("gpg", "--batch", "--import")
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	cmd.Stdin = bytes.NewReader(sub2)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	dir := t.TempDir()
	deb := filepath.Join(dir, "foo.deb")
	os.WriteFile(deb, fakeDeb(t, "foo", "1.0-0~pav1", "amd64"), 0o644)
	conf := filepath.Join(dir, "aptly.conf")
	os.WriteFile(conf, []byte(`{"rootDir":"`+dir+`/a","gpgProvider":"gpg2","architectures":["amd64"],
"FileSystemPublishEndpoints":{"g":{"rootDir":"`+dir+`/g","linkMethod":"hardlink"}}}`), 0o644)
	aptly := func(args ...string) string {
		c := exec.Command("aptly", append([]string{"-config=" + conf}, args...)...)
		c.Env = append(os.Environ(), "GNUPGHOME="+home)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("aptly %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	aptly("repo", "create", "-distribution=trixie", "-component=main", "r")
	aptly("repo", "add", "r", deb)
	aptly("snapshot", "create", "s", "from", "repo", "r")
	c := exec.Command("aptly", "-config="+conf, "publish", "snapshot", "-batch",
		"-gpg-key="+s1+"!", "-gpg-key="+s2+"!", "-passphrase-file="+sign.PassphraseFile(home), "s", "filesystem:g:.")
	c.Env = append(os.Environ(), "GNUPGHOME="+home)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Logf("aptly 1.6.1 no admite -gpg-key repetido: %v\n%s", err, out)
		return
	}
	kr := filepath.Join(dir, "k.gpg")
	if err := sign.ExportKeyring(ctx, home2, []string{m2}, kr); err != nil {
		t.Fatal(err)
	}
	k1, _ := sign.Run(ctx, home, "--export", m1)
	f, _ := os.OpenFile(kr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.Write(k1)
	f.Close()
	v := exec.Command("gpgv", "--status-fd", "1", "--keyring", kr, filepath.Join(dir, "g/dists/trixie/InRelease"))
	vout, _ := v.Output()
	n := strings.Count(string(vout), "VALIDSIG")
	t.Logf("aptly 1.6.1 con dos -gpg-key: InRelease con %d firmas válidas", n)
	if n != 2 {
		t.Logf("salida de gpgv:\n%s", vout)
	}
}
