package supply

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

// TagDiff es el resultado de comparar el tarball de release con el archivo
// del tag de git (FR-014).
type TagDiff struct {
	Extra    map[string]string // ruta -> sha256: en el release y no en el tag, o distinto
	Missing  []string          // en el tag y no en el release (no falla)
	Releases int               // archivos del release
}

// TarballFiles lee un tar.gz, tar.xz o tar.bz2 y devuelve ruta -> sha256 de
// los archivos regulares y enlaces, sin el directorio superior único.
func TarballFiles(p, format string) (map[string]string, error) {
	var r io.Reader
	fh, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	switch format {
	case "tar.gz":
		gz, err := gzip.NewReader(fh)
		if err != nil {
			return nil, err
		}
		r = gz
	case "tar.bz2":
		r = bzip2.NewReader(fh)
	case "tar.xz":
		// Sin xz en la biblioteca estándar: xz de Debian solo descomprime.
		out, err := exec.Command("xz", "-dc", p).Output()
		if err != nil {
			return nil, fmt.Errorf("xz -dc %s: %v", path.Base(p), err)
		}
		r = bytes.NewReader(out)
	default:
		return nil, fmt.Errorf("formato %q no admitido", format)
	}
	files := map[string]string{}
	var names []string
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch h.Typeflag {
		case tar.TypeReg:
			sum := sha256.New()
			if _, err := io.Copy(sum, tr); err != nil {
				return nil, err
			}
			files[h.Name] = hex.EncodeToString(sum.Sum(nil))
		case tar.TypeSymlink:
			files[h.Name] = "link:" + h.Linkname
		default:
			continue
		}
		names = append(names, h.Name)
	}
	// Quitar el directorio superior único.
	top := ""
	for _, n := range names {
		first, _, nested := strings.Cut(strings.TrimPrefix(n, "./"), "/")
		if !nested {
			top = ""
			break
		}
		if top == "" {
			top = first
		} else if top != first {
			top = ""
			break
		}
	}
	out := make(map[string]string, len(files))
	for n, sum := range files {
		rel := strings.TrimPrefix(n, "./")
		if top != "" {
			rel = strings.TrimPrefix(rel, top+"/")
		}
		out[rel] = sum
	}
	return out, nil
}

// CompareTag compara el tarball de release (formato del manifiesto) con el
// archivo del tag (tar.gz de GitHub).
func CompareTag(release, format, tagArchive string) (*TagDiff, error) {
	rel, err := TarballFiles(release, format)
	if err != nil {
		return nil, fmt.Errorf("tarball de release: %v", err)
	}
	tag, err := TarballFiles(tagArchive, "tar.gz")
	if err != nil {
		return nil, fmt.Errorf("archivo del tag: %v", err)
	}
	d := &TagDiff{Extra: map[string]string{}, Releases: len(rel)}
	for p, sum := range rel {
		if tag[p] != sum {
			d.Extra[p] = sum
		}
	}
	for p := range tag {
		if _, ok := rel[p]; !ok {
			d.Missing = append(d.Missing, p)
		}
	}
	sort.Strings(d.Missing)
	return d, nil
}

// TagDiffError lista los archivos del tarball que no están en el tag (o
// difieren) y no están declarados, o cuyo sha256 cambió.
type TagDiffError struct {
	Undeclared []string
	Changed    []string
}

func (e *TagDiffError) Error() string {
	var parts []string
	if len(e.Undeclared) > 0 {
		parts = append(parts, "archivos que no están en el tag de git (o difieren) sin declarar en source.extra_files: "+strings.Join(e.Undeclared, ", "))
	}
	if len(e.Changed) > 0 {
		parts = append(parts, "archivos declarados cuyo sha256 cambió (revisa y vuelve a ejecutar lock): "+strings.Join(e.Changed, ", "))
	}
	return strings.Join(parts, "; ")
}

// CheckExtras aplica FR-014: cada archivo extra MUST estar declarado y, si
// locked no es nil (build), tener el sha256 del lockfile. Devuelve los
// sha256 de los declarados para el lockfile.
func (d *TagDiff) CheckExtras(declared []string, locked map[string]string) (map[string]string, error) {
	isDeclared := map[string]bool{}
	for _, p := range declared {
		isDeclared[p] = true
	}
	e := &TagDiffError{}
	out := map[string]string{}
	for p, sum := range d.Extra {
		switch {
		case !isDeclared[p]:
			e.Undeclared = append(e.Undeclared, p)
		case locked != nil && locked[p] != sum:
			e.Changed = append(e.Changed, p)
		default:
			out[p] = sum
		}
	}
	if len(e.Undeclared)+len(e.Changed) > 0 {
		sort.Strings(e.Undeclared)
		sort.Strings(e.Changed)
		return nil, e
	}
	return out, nil
}
