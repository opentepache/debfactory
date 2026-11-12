// Package fetch descarga artefactos con una caché direccionada por contenido:
// nada entra en la caché sin haber verificado su sha256 (constitución III).
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"
)

// Hosts permitidos. Los artefactos y archivos de checksums empiezan en
// github.com y pueden redirigir a los almacenes de assets de GitHub; el texto
// de licencia se descarga de raw.githubusercontent.com.
var (
	artifactHosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}
	originHosts   = []string{"github.com", rawHost}
)

const rawHost = "raw.githubusercontent.com"

// DefaultTimeout limita cada descarga completa.
const DefaultTimeout = 10 * time.Minute

// Fetcher descarga a <Cache>/tmp y guarda en <Cache>/sha256/<hash> lo verificado.
type Fetcher struct {
	Client *http.Client
	Cache  string
}

// New crea un Fetcher con un cliente que solo sigue redirecciones https a
// hosts permitidos. rt es el transporte (nil = http.DefaultTransport); los
// tests inyectan uno que redirige a un servidor local.
func New(cache string, timeout time.Duration, rt http.RoundTripper) *Fetcher {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &Fetcher{
		Client: &http.Client{Timeout: timeout, Transport: rt, CheckRedirect: checkRedirect},
		Cache:  cache,
	}
}

// HashMismatch indica que lo descargado no coincide con el hash esperado; el
// pipeline lo informa en la etapa verify.
type HashMismatch struct {
	URL, Expected, Got string
}

func (e *HashMismatch) Error() string {
	return fmt.Sprintf("sha256 no coincide para %s (esperado %s, obtenido %s)", path.Base(e.URL), e.Expected, e.Got)
}

// HostError indica un destino no permitido (etapa fetch).
type HostError struct {
	URL, Host string
}

func (e *HostError) Error() string {
	return fmt.Sprintf("destino no permitido: %s (%s)", e.Host, e.URL)
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("demasiadas redirecciones")
	}
	allowed := artifactHosts
	if via[0].URL.Hostname() == rawHost {
		allowed = append(slices.Clone(artifactHosts), rawHost)
	}
	if req.URL.Scheme != "https" || !slices.Contains(allowed, req.URL.Hostname()) {
		return &HostError{URL: req.URL.Redacted(), Host: req.URL.Scheme + "://" + req.URL.Host}
	}
	return nil
}

func (f *Fetcher) cachePath(sha string) string {
	return filepath.Join(f.Cache, "sha256", sha)
}

// Get devuelve la ruta local de url y su sha256.
//
// Con expected no vacío: si el archivo ya está en caché no descarga; si no,
// descarga y solo lo guarda en caché si el hash coincide (si no, *HashMismatch).
// Con expected vacío (solo lo usa `lock`): devuelve un temporal sin cachear;
// el llamador lo verifica y llama a Commit o Discard.
func (f *Fetcher) Get(ctx context.Context, url, expected string) (string, string, error) {
	if expected != "" {
		if p := f.cachePath(expected); exists(p) {
			return p, expected, nil
		}
	}
	u, err := neturl.Parse(url)
	if err != nil {
		return "", "", fmt.Errorf("URL inválida %q: %w", url, err)
	}
	if u.Scheme != "https" || !slices.Contains(originHosts, u.Hostname()) {
		return "", "", &HostError{URL: url, Host: u.Scheme + "://" + u.Host}
	}
	tmp, sha, err := f.download(ctx, url)
	if err != nil {
		return "", "", err
	}
	if expected == "" {
		return tmp, sha, nil
	}
	if sha != expected {
		os.Remove(tmp)
		return "", sha, &HashMismatch{URL: url, Expected: expected, Got: sha}
	}
	p, err := f.Commit(tmp, sha)
	return p, sha, err
}

func (f *Fetcher) download(ctx context.Context, url string) (string, string, error) {
	dir := filepath.Join(f.Cache, "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	tmp, err := os.CreateTemp(dir, "dl-*")
	if err != nil {
		return "", "", err
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "debfactory")
	resp, err := f.Client.Do(req)
	if err != nil {
		var he *HostError
		if errors.As(err, &he) {
			return "", "", he
		}
		return "", "", fmt.Errorf("descarga de %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("descarga de %s: HTTP %d", url, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		return "", "", fmt.Errorf("descarga de %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}
	ok = true
	return tmp.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// Commit mueve a la caché un temporal de Get ya verificado.
func (f *Fetcher) Commit(tmp, sha string) (string, error) {
	dst := f.cachePath(sha)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// Discard borra un temporal de Get que no se va a cachear.
func Discard(tmp string) { os.Remove(tmp) }

// SHA256File calcula el sha256 de un archivo.
func SHA256File(p string) (string, error) {
	fh, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SHA256 calcula el sha256 de unos bytes.
func SHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
