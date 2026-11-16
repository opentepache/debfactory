package sign

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/redact"
	"debfactory/internal/report"
)

// ReleaseTime es el formato de fecha de los Release de Debian (RFC 1123 en
// UTC, como Date).
const ReleaseTime = "Mon, 02 Jan 2006 15:04:05 UTC"

// AddValidUntil inserta "Valid-Until: <signedAt + days>" justo después de
// la línea Date: de un Release y conserva el resto byte a byte
// (specs/003-ci-pipeline, FR-030, research D11). Si ya hay un Valid-Until lo
// sustituye.
func AddValidUntil(releasePath string, signedAt time.Time, days int) error {
	data, err := os.ReadFile(releasePath)
	if err != nil {
		return err
	}
	until := signedAt.UTC().Add(time.Duration(days) * 24 * time.Hour)
	field := "Valid-Until: " + until.Format(ReleaseTime)
	lines := strings.SplitAfter(string(data), "\n")
	var out strings.Builder
	inserted := false
	for _, l := range lines {
		if strings.HasPrefix(l, "Valid-Until:") {
			continue
		}
		out.WriteString(l)
		if !inserted && strings.HasPrefix(l, "Date:") {
			out.WriteString(field + "\n")
			inserted = true
		}
	}
	if !inserted {
		return fmt.Errorf("%s: no tiene Date:", releasePath)
	}
	tmp := releasePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, releasePath)
}

// ValidUntil lee el Valid-Until de un Release (o InRelease). Devuelve el
// tiempo cero si no lo tiene.
func ValidUntil(release []byte) (time.Time, error) {
	for _, l := range strings.Split(string(release), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(l, "\r"), "Valid-Until:"); ok {
			return time.Parse(ReleaseTime, strings.TrimSpace(v))
		}
	}
	return time.Time{}, nil
}

// SignRelease firma dir/Release con la subllave: escribe dir/InRelease
// (--clearsign) y dir/Release.gpg (--detach-sign --armor). La frase de paso
// se lee del archivo del llavero; la salida de gpg pasa por redact.
func SignRelease(ctx context.Context, home, subkey, dir string) error {
	rel := filepath.Join(dir, "Release")
	for _, s := range []struct{ mode, out string }{
		{"--clearsign", filepath.Join(dir, "InRelease")},
		{"--detach-sign", filepath.Join(dir, "Release.gpg")},
	} {
		os.Remove(s.out)
		args := []string{"--batch", "--no-tty", "--yes", "--pinentry-mode", "loopback",
			"--passphrase-file", PassphraseFile(home), "--local-user", subkey + "!",
			"--digest-algo", "SHA512", "--armor", s.mode, "--output", s.out, rel}
		cmd := exec.CommandContext(ctx, "gpg", args...)
		cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
		var errb bytes.Buffer
		cmd.Stderr = &errb
		if err := cmd.Run(); err != nil {
			lines := redact.Lines(errb.Bytes())
			if len(lines) == 0 {
				lines = []string{err.Error()}
			}
			return report.Newf(report.Sign, "gpg %s %s: %s", s.mode, filepath.Base(dir), strings.Join(lines, "; "))
		}
	}
	return nil
}
