// Package assemble prepara el árbol del paquete: extrae el artefacto, resuelve
// install[], man pages y completions, y calcula Depends desde ELF.
package assemble

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// maxMember limita lo que se lee de un solo miembro con ExtractFile.
const maxMember = 16 << 20

// Extract extrae el artefacto (zip, tar.gz o binary) en dir. Un binario suelto
// se copia como dir/<name>. Rechaza rutas absolutas, ".." y enlaces que salgan
// de dir; conserva el bit ejecutable de origen.
func Extract(archive, format, dir, name string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	switch format {
	case "binary":
		return copyFile(archive, filepath.Join(dir, name), 0o755)
	case "zip":
		return extractZip(archive, dir)
	case "tar.gz":
		return extractTarGz(archive, dir)
	}
	return fmt.Errorf("formato desconocido %q", format)
}

// ExtractFile devuelve el contenido de un único miembro regular del artefacto.
func ExtractFile(archive, format, member string) ([]byte, error) {
	want := path.Clean(member)
	switch format {
	case "zip":
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if path.Clean(f.Name) == want && f.Mode().IsRegular() {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return readLimited(rc)
			}
		}
	case "tar.gz":
		var found []byte
		err := walkTarGz(archive, func(h *tar.Header, r io.Reader) error {
			if found == nil && path.Clean(h.Name) == want && h.Typeflag == tar.TypeReg {
				b, err := readLimited(r)
				found = b
				return err
			}
			return nil
		})
		if err != nil || found != nil {
			return found, err
		}
	default:
		return nil, fmt.Errorf("un artefacto %q no tiene archivos internos", format)
	}
	return nil, fmt.Errorf("no existe %q en el artefacto", member)
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxMember+1))
	if err == nil && len(b) > maxMember {
		err = errors.New("miembro demasiado grande")
	}
	return b, err
}

// safeJoin devuelve dir/name si name es relativa y no sale de dir.
func safeJoin(dir, name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("entrada con ruta insegura %q", name)
	}
	return filepath.Join(dir, filepath.FromSlash(clean)), nil
}

// checkLink exige que el destino de un enlace quede dentro de dir.
func checkLink(name, target string) error {
	if path.IsAbs(target) {
		return fmt.Errorf("enlace %q apunta fuera del artefacto (%q)", name, target)
	}
	resolved := path.Join(path.Dir(path.Clean(name)), target)
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("enlace %q apunta fuera del artefacto (%q)", name, target)
	}
	return nil
}

func modeFor(m os.FileMode) os.FileMode {
	if m&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func extractZip(archive, dir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		dst, err := safeJoin(dir, f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			rc, err := f.Open()
			if err != nil {
				return err
			}
			target, err := readLimited(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := checkLink(f.Name, string(target)); err != nil {
				return err
			}
			if err := symlink(string(target), dst); err != nil {
				return err
			}
		case mode.IsRegular():
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = writeFile(dst, rc, modeFor(mode))
			rc.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("tipo de entrada no soportado en %q", f.Name)
		}
	}
	return nil
}

func walkTarGz(archive string, fn func(*tar.Header, io.Reader) error) error {
	fh, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

func extractTarGz(archive, dir string) error {
	return walkTarGz(archive, func(h *tar.Header, r io.Reader) error {
		if h.Typeflag == tar.TypeXGlobalHeader {
			return nil
		}
		dst, err := safeJoin(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			return os.MkdirAll(dst, 0o755)
		case tar.TypeReg:
			return writeFile(dst, r, modeFor(os.FileMode(h.Mode)))
		case tar.TypeSymlink:
			if err := checkLink(h.Name, h.Linkname); err != nil {
				return err
			}
			return symlink(h.Linkname, dst)
		case tar.TypeLink:
			src, err := safeJoin(dir, h.Linkname)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.Link(src, dst)
		}
		return fmt.Errorf("tipo de entrada no soportado en %q", h.Name)
	})
}

func symlink(target, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	os.Remove(dst)
	return os.Symlink(target, dst)
}

func writeFile(dst string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// O_NOFOLLOW: nunca escribir a través de un enlace creado por el artefacto.
	os.Remove(dst)
	fh, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fh, r); err != nil {
		fh.Close()
		return err
	}
	if err := fh.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func copyFile(src, dst string, mode os.FileMode) error {
	fh, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fh.Close()
	return writeFile(dst, fh, mode)
}
