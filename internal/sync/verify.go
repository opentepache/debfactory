package sync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"debfactory/internal/fetch"
	"debfactory/internal/repo"
	"debfactory/internal/report"
)

// fetcher lee archivos de un destino: por HTTP (public_url) o, para un
// destino local sin URL, del disco.
type fetcher interface {
	get(ctx context.Context, rel string) ([]byte, http.Header, error)
}

type httpFetcher struct {
	base   string
	client *http.Client
}

func (f httpFetcher) get(ctx context.Context, rel string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(f.base, "/")+"/"+rel, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "debfactory")
	res, err := f.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(res.Body)
	return b, res.Header, err
}

type dirFetcher struct{ dir string }

func (f dirFetcher) get(_ context.Context, rel string) ([]byte, http.Header, error) {
	b, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(rel)))
	return b, nil, err
}

// VerifyDest comprueba un destino ya sincronizado (repo verify --dest):
// cada InRelease verifica con el debfactory.gpg remoto, cuyo fingerprint es
// una maestra configurada; los índices de cada suite y una muestra
// determinista de pool/ coinciden con la generación vigente; y, si el
// destino usa caché, los encabezados Cache-Control son los configurados.
func VerifyDest(ctx context.Context, e *repo.Env, destName string) error {
	dest, ok := e.Cfg.Repo.Destinations[destName]
	if !ok {
		return report.Newf(report.VerifyRepo, "%s: destino desconocido (repo.destinations)", destName)
	}
	cur, err := repo.Current(e.DataDir)
	if err != nil || cur == nil {
		return report.Newf(report.VerifyRepo, "%s: no hay generación local con la que comparar", destName)
	}
	var f fetcher
	switch {
	case dest.PublicURL != "":
		f = httpFetcher{base: dest.PublicURL, client: &http.Client{Timeout: 60 * time.Second}}
	case filepath.IsAbs(dest.Remote):
		f = dirFetcher{dir: dest.Remote}
	default:
		return report.Newf(report.VerifyRepo, "%s: falta public_url para verificar el destino", destName)
	}
	var errs report.Errors
	fail := func(suite, format string, args ...any) {
		errs = append(errs, &report.Error{Suite: suite, Stage: report.VerifyRepo,
			Cause: destName + ": " + fmt.Sprintf(format, args...)})
	}
	keyring, _, err := f.get(ctx, "debfactory.gpg")
	if err != nil {
		fail("", "debfactory.gpg: %v", err)
		return errs
	}
	tmp, err := os.MkdirTemp("", "debfactory-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	kr := filepath.Join(tmp, "debfactory.gpg")
	os.WriteFile(kr, keyring, 0o644)
	if err := keyringHasMaster(ctx, tmp, kr, e.Cfg.Repo.Signing.Masters); err != nil {
		fail("", "debfactory.gpg: %v", err)
	}
	// Archivos a comparar: lo de dists/ salvo by-hash y Contents, más 5 de pool/.
	var check, pool []string
	for p := range cur.Files {
		switch {
		case strings.HasPrefix(p, "pool/"):
			pool = append(pool, p)
		case strings.HasPrefix(p, "dists/") && !strings.Contains(p, "/by-hash/") && !strings.Contains(p, "Contents"):
			check = append(check, p)
		}
	}
	sort.Strings(check)
	sort.Strings(pool)
	check = append(check, pool[:min(5, len(pool))]...)
	headers := map[string]http.Header{}
	for _, p := range check {
		data, h, err := f.get(ctx, p)
		if err != nil {
			fail(suiteOf(p), "%s: %v", p, err)
			continue
		}
		if fetch.SHA256(data) != cur.Files[p] {
			fail(suiteOf(p), "%s no coincide con la generación local %s", p, cur.Name)
			continue
		}
		headers[p] = h
		if strings.HasSuffix(p, "/InRelease") {
			in := filepath.Join(tmp, "InRelease")
			os.WriteFile(in, data, 0o644)
			if out, err := exec.CommandContext(ctx, "gpgv", "--keyring", kr, in).CombinedOutput(); err != nil {
				fail(suiteOf(p), "InRelease no verifica con el debfactory.gpg publicado: %s",
					strings.TrimSpace(string(bytes.TrimSpace(out))))
			}
		}
	}
	if dest.CacheControl {
		for p, h := range headers {
			_, imm := Classify(p)
			want := e.Cfg.Repo.CacheControl.Mutable
			if imm {
				want = e.Cfg.Repo.CacheControl.Immutable
			}
			if got := h.Get("Cache-Control"); got != want {
				fail(suiteOf(p), "%s: Cache-Control %q, se esperaba %q", p, got, want)
			}
		}
	}
	if err := errs.Err(); err != nil {
		return err
	}
	e.Out.Event("verify-dest", map[string]any{"dest": destName, "serial": cur.Serial, "files": len(check)},
		fmt.Sprintf("%s: serial %d verificado (%d archivos, firmas y encabezados)", destName, cur.Serial, len(check)))
	return nil
}

// keyringHasMaster comprueba que el keyring publicado contiene alguna de
// las maestras configuradas.
func keyringHasMaster(ctx context.Context, home, keyring string, masters []string) error {
	cmd := exec.CommandContext(ctx, "gpg", "--batch", "--no-tty", "--with-colons", "--show-keys", keyring)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("no se puede leer: %v", err)
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Split(l, ":")
		if len(f) > 9 && f[0] == "fpr" && slices.Contains(masters, f[9]) {
			return nil
		}
	}
	return fmt.Errorf("no contiene ninguna llave maestra de repo.signing.masters")
}

func suiteOf(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) > 1 && parts[0] == "dists" {
		return parts[1]
	}
	return ""
}
