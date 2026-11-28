package supply

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WriteTree escribe dir como tar determinista en w: rutas bajo prefix en
// orden, dueño 0:0, mtime fijo, permisos normalizados (0755 si es ejecutable
// o directorio, si no 0644) y enlaces simbólicos conservados.
func WriteTree(w io.Writer, dir, prefix string, mtime time.Time) error {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(paths, func(i, j int) bool {
		a, _ := filepath.Rel(dir, paths[i])
		b, _ := filepath.Rel(dir, paths[j])
		return filepath.ToSlash(a) < filepath.ToSlash(b)
	})
	tw := tar.NewWriter(w)
	for _, p := range paths {
		rel, _ := filepath.Rel(dir, p)
		name := path.Join(prefix, filepath.ToSlash(rel))
		st, err := os.Lstat(p)
		if err != nil {
			return err
		}
		h := &tar.Header{Name: name, ModTime: mtime, Uname: "root", Gname: "root", Format: tar.FormatPAX}
		switch {
		case st.IsDir():
			h.Typeflag, h.Name, h.Mode = tar.TypeDir, name+"/", 0o755
		case st.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			h.Typeflag, h.Linkname, h.Mode = tar.TypeSymlink, target, 0o777
		case st.Mode().IsRegular():
			h.Typeflag, h.Size, h.Mode = tar.TypeReg, st.Size(), 0o644
			if st.Mode().Perm()&0o111 != 0 {
				h.Mode = 0o755
			}
		default:
			return fmt.Errorf("%s: tipo de archivo no admitido", p)
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg {
			fh, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, fh)
			fh.Close()
			if err != nil {
				return err
			}
		}
	}
	return tw.Close()
}

// TreeSHA256 es el sha256 del tar determinista de dir (sin prefijo).
func TreeSHA256(dir string) (string, error) {
	h := sha256.New()
	if err := WriteTree(h, dir, "", time.Unix(0, 0).UTC()); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ExtractZigCache extrae un paquete de la caché de Zig 0.17 (p/<hash>.tar.gz)
// en dst como lo espera `zig build --system`: sin el directorio del envoltorio
// y, si todo cuelga de un único directorio, sin él (research D2). Rechaza
// rutas absolutas, con ".." y tipos especiales.
func ExtractZigCache(tarball, dst string) error {
	fh, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer fh.Close()
	gz, err := gzip.NewReader(fh)
	if err != nil {
		return err
	}
	type entry struct {
		h    *tar.Header
		data []byte
	}
	var entries []entry
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		var data []byte
		if h.Typeflag == tar.TypeReg {
			if data, err = io.ReadAll(tr); err != nil {
				return err
			}
		}
		entries = append(entries, entry{h, data})
	}
	strip := func(name string, n int) string {
		parts := strings.Split(strings.Trim(path.Clean(name), "/"), "/")
		if len(parts) <= n {
			return ""
		}
		return strings.Join(parts[n:], "/")
	}
	// Directorio superior único tras quitar el envoltorio.
	levels := 1
	top := ""
	single := true
	for _, e := range entries {
		rest := strip(e.h.Name, 1)
		if rest == "" {
			continue
		}
		first, _, nested := strings.Cut(rest, "/")
		if !nested && e.h.Typeflag != tar.TypeDir {
			single = false
		}
		if top == "" {
			top = first
		} else if first != top {
			single = false
		}
	}
	if single && top != "" {
		levels = 2
	}
	for _, e := range entries {
		rel := strip(e.h.Name, levels)
		if rel == "" {
			continue
		}
		if path.IsAbs(rel) || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			return fmt.Errorf("%s: ruta inválida %q", tarball, e.h.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		switch e.h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if e.h.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := os.WriteFile(target, e.data, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			resolved := filepath.Join(filepath.Dir(target), filepath.FromSlash(e.h.Linkname))
			if r, err := filepath.Rel(dst, resolved); path.IsAbs(e.h.Linkname) || err != nil || r == ".." || strings.HasPrefix(r, "../") {
				return fmt.Errorf("%s: enlace que sale del paquete %q -> %q", tarball, e.h.Name, e.h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(e.h.Linkname, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: tipo no admitido en %q", tarball, e.h.Name)
		}
	}
	return nil
}
