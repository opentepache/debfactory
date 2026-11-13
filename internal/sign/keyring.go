// Package sign gestiona el llavero dedicado de la PC de build
// (<data>/gnupg): comprueba que solo tiene la subllave de firma, lee su
// caducidad, firma una prueba y exporta el keyring público
// (specs/002-signed-apt-repo, research D4).
package sign

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/redact"
	"debfactory/internal/report"
)

// Home es el llavero dedicado dentro de <data>.
func Home(dataDir string) string { return filepath.Join(dataDir, "gnupg") }

// PassphraseFile es la frase de paso de la subllave (permisos 600).
func PassphraseFile(home string) string { return filepath.Join(home, "passphrase") }

// Key es una llave del llavero según gpg --with-colons.
type Key struct {
	Fingerprint string
	Kind        string // sec | ssb | pub | sub
	Caps        string // capacidades (s, c, e, a)
	Stub        bool   // la parte secreta no está (sec#)
	Expires     time.Time
	Master      string // fingerprint de la maestra (para subllaves)
}

// Run ejecuta gpg con GNUPGHOME=home en modo no interactivo. La salida de
// error se filtra con redact.
func Run(ctx context.Context, home string, args ...string) ([]byte, error) {
	full := append([]string{"--batch", "--no-tty"}, args...)
	cmd := exec.CommandContext(ctx, "gpg", full...)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		lines := redact.Lines(errb.Bytes())
		if len(lines) == 0 {
			lines = []string{err.Error()}
		}
		return out.Bytes(), fmt.Errorf("gpg: %s", strings.Join(lines, "; "))
	}
	return out.Bytes(), nil
}

// List lee las llaves secretas (secret=true) o públicas del llavero.
func List(ctx context.Context, home string, secret bool) ([]Key, error) {
	flag := "--list-keys"
	if secret {
		flag = "--list-secret-keys"
	}
	out, err := Run(ctx, home, "--with-colons", "--fixed-list-mode", flag)
	if err != nil {
		return nil, err
	}
	var keys []Key
	var master string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 10 {
			continue
		}
		switch f[0] {
		case "sec", "ssb", "pub", "sub":
			k := Key{Kind: f[0]}
			if len(f) > 11 {
				k.Caps = f[11]
			}
			if len(f) > 14 && f[14] == "#" {
				k.Stub = true
			}
			if ts, err := strconv.ParseInt(f[6], 10, 64); err == nil && ts > 0 {
				k.Expires = time.Unix(ts, 0).UTC()
			}
			keys = append(keys, k)
		case "fpr":
			if len(keys) == 0 || keys[len(keys)-1].Fingerprint != "" {
				continue
			}
			k := &keys[len(keys)-1]
			k.Fingerprint = f[9]
			if k.Kind == "sec" || k.Kind == "pub" {
				master = k.Fingerprint
			} else {
				k.Master = master
			}
		}
	}
	return keys, nil
}

func envErr(format string, args ...any) error {
	return &report.EnvError{Cause: fmt.Sprintf(format, args...)}
}

// Check comprueba el llavero de build (FR-018, FR-018a). Falta de llavero o
// permisos incorrectos son entorno incompleto (código 3); una subllave
// ausente, ajena o con la maestra secreta presente es un fallo de la etapa
// sign (código 1).
func Check(ctx context.Context, cfg *config.Config, home string) error {
	sg := cfg.Repo.Signing
	if sg.Subkey == "" || len(sg.Masters) == 0 {
		return envErr("repo.signing.subkey y repo.signing.masters deben estar configurados (docs/operacion-llave.md)")
	}
	st, err := os.Stat(home)
	if err != nil {
		return envErr("no existe el llavero de firma %s (docs/operacion-llave.md)", home)
	}
	if st.Mode().Perm() != 0o700 {
		return envErr("%s debe tener permisos 700 (tiene %o)", home, st.Mode().Perm())
	}
	pf, err := os.Stat(PassphraseFile(home))
	if err != nil {
		return envErr("falta %s", PassphraseFile(home))
	}
	if pf.Mode().Perm() != 0o600 {
		return envErr("%s debe tener permisos 600 (tiene %o)", PassphraseFile(home), pf.Mode().Perm())
	}
	keys, err := List(ctx, home, true)
	if err != nil {
		return envErr("%v", err)
	}
	var sub *Key
	for i, k := range keys {
		if k.Kind == "sec" && slices.Contains(sg.Masters, k.Fingerprint) && !k.Stub {
			return report.Newf(report.Sign, "la llave maestra no debe estar en la PC de build (FR-018a): %s", k.Fingerprint)
		}
		if k.Kind == "ssb" && k.Fingerprint == sg.Subkey {
			sub = &keys[i]
		}
	}
	if sub == nil || sub.Stub || !strings.Contains(sub.Caps, "s") {
		return report.Newf(report.Sign, "gpg: no hay llave secreta para %s en %s", short(sg.Subkey), home)
	}
	if !slices.Contains(sg.Masters, sub.Master) {
		return report.Newf(report.Sign, "la subllave %s no pertenece a las llaves maestras configuradas", short(sg.Subkey))
	}
	return nil
}

// Expiry devuelve la caducidad de la subllave (cero si no caduca).
func Expiry(ctx context.Context, home, subkey string) (time.Time, error) {
	keys, err := List(ctx, home, false)
	if err != nil {
		return time.Time{}, err
	}
	for _, k := range keys {
		if k.Fingerprint == subkey {
			return k.Expires, nil
		}
	}
	return time.Time{}, fmt.Errorf("no se encuentra la subllave %s", short(subkey))
}

// TestSign firma un texto fijo con la subllave y el archivo de frase de
// paso. Si falla, la publicación no llega a aptly (etapa sign).
func TestSign(ctx context.Context, home, subkey string) error {
	cmd := exec.CommandContext(ctx, "gpg", "--batch", "--no-tty", "--pinentry-mode", "loopback",
		"--passphrase-file", PassphraseFile(home), "--local-user", subkey+"!", "--clearsign")
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	cmd.Stdin = strings.NewReader("debfactory\n")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		lines := redact.Lines(errb.Bytes())
		if len(lines) == 0 {
			lines = []string{err.Error()}
		}
		return report.Newf(report.Sign, "gpg: %s", strings.Join(lines, "; "))
	}
	return nil
}

// ExportKeyring escribe en dst el keyring binario con las maestras públicas
// (y sus subllaves) que estén en el llavero (FR-022).
func ExportKeyring(ctx context.Context, home string, masters []string, dst string) error {
	out, err := Run(ctx, home, append([]string{"--export"}, masters...)...)
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return fmt.Errorf("gpg --export no devolvió ninguna llave para %v", masters)
	}
	return os.WriteFile(dst, out, 0o644)
}

func short(fpr string) string {
	if len(fpr) > 16 {
		return fpr[len(fpr)-16:]
	}
	return fpr
}
