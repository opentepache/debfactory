package assemble

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"debfactory/internal/manifest"
)

// Man resuelve los globs de man, comprime cada página como `gzip -9n` en
// outDir y la instala en /usr/share/man/man<N>/<archivo>.gz (0644). La
// sección sale de la extensión (.1 → man1, .3pm → man3).
func Man(m *manifest.Manifest, arch, root, outDir string) ([]File, error) {
	vars, err := m.Vars(arch)
	if err != nil {
		return nil, err
	}
	var out []File
	for i, g := range m.Man {
		pattern, err := manifest.Render(g, vars)
		if err != nil {
			return nil, assembleErr("man[%d]: %v", i, err)
		}
		matches, err := globFiles(root, pattern)
		if err != nil {
			return nil, assembleErr("man[%d]: %v", i, err)
		}
		if len(matches) == 0 {
			return nil, assembleErr("man[%d]: el patrón %q no seleccionó ningún archivo", i, pattern)
		}
		for _, rel := range matches {
			base := path.Base(rel)
			ext := path.Ext(base)
			if len(ext) < 2 || ext[1] < '1' || ext[1] > '9' {
				return nil, assembleErr("man[%d]: %q no tiene extensión de sección (.1 … .9)", i, rel)
			}
			sec := string(ext[1])
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				return nil, assembleErr("man[%d]: %v", i, err)
			}
			gz, err := Gzip(data)
			if err != nil {
				return nil, err
			}
			p := filepath.Join(outDir, "man"+sec, base+".gz")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(p, gz, 0o644); err != nil {
				return nil, err
			}
			out = append(out, File{Dst: fmt.Sprintf("/usr/share/man/man%s/%s.gz", sec, base), Src: p, Mode: 0o644})
		}
	}
	return out, nil
}

// Gzip comprime como `gzip -9n`: nivel 9, sin nombre de archivo y fecha 0.
func Gzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
