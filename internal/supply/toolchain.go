package supply

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
)

// ToolchainResult es un toolchain descargado y verificado.
type ToolchainResult struct {
	Arch    string
	URL     string
	SHA256  string
	SigURL  string // vacío si upstream no firma el toolchain
	SigSHA  string
	Tarball string // en la caché del fetcher
}

// FetchToolchain descarga el toolchain de una arquitectura, comprueba su
// sha256 (want; vacío en lock) y su firma fijada (FR-001). Con want vacío, el
// tarball y la firma verificados entran en la caché.
func FetchToolchain(ctx context.Context, f *fetch.Fetcher, v *Verifier, t *manifest.Toolchain, arch, want, wantSig string) (*ToolchainResult, error) {
	url, err := t.RenderURL(t.URL, arch)
	if err != nil {
		return nil, err
	}
	path, sha, err := f.Get(ctx, url, want)
	if err != nil {
		return nil, fmt.Errorf("toolchain %s %s (%s): %w", t.Name, t.Version, arch, err)
	}
	r := &ToolchainResult{Arch: arch, URL: url, SHA256: sha, Tarball: path}
	if t.Signing != nil {
		r.SigURL, err = t.RenderURL(t.Signing.URL(), arch)
		if err != nil {
			return nil, err
		}
		sigPath, sigSHA, err := f.Get(ctx, r.SigURL, wantSig)
		if err != nil {
			if want == "" {
				fetch.Discard(path)
			}
			return nil, fmt.Errorf("firma del toolchain: %w", err)
		}
		if err := v.Verify(ctx, t.Signing, path, sigPath); err != nil {
			if want == "" {
				fetch.Discard(path)
				fetch.Discard(sigPath)
			}
			return nil, err
		}
		r.SigSHA = sigSHA
		if wantSig == "" {
			if _, err := f.Commit(sigPath, sigSHA); err != nil {
				return nil, err
			}
		}
	}
	if want == "" {
		if r.Tarball, err = f.Commit(path, sha); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// ExtractToolchain extrae el tarball del toolchain en dst sin su directorio
// superior (zig queda en dst/zig). Usa el tar de Debian: solo extrae, no
// ejecuta nada de upstream.
func ExtractToolchain(ctx context.Context, tarball, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "tar", "-xf", tarball, "-C", dst, "--strip-components=1",
		"--no-same-owner", "--no-same-permissions")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extraer el toolchain: %v: %s", err, out)
	}
	return nil
}

// ToolchainBinary es la ruta del binario principal del toolchain extraído.
func ToolchainBinary(dir string, t *manifest.Toolchain) string { return filepath.Join(dir, t.Name) }
