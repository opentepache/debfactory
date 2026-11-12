package assemble

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"debfactory/internal/manifest"
	"debfactory/internal/report"
)

// File es una entrada del BuildPlan: un archivo del paquete.
type File struct {
	Dst  string      // ruta absoluta dentro del paquete
	Src  string      // ruta en el directorio de trabajo
	Mode os.FileMode // 0755 o 0644 normalmente
}

// Install resuelve install[] del manifiesto contra el artefacto extraído en
// root. src admite plantillas y globs (*, ?, [...]); dst terminado en "/"
// conserva el nombre de origen y si no es la ruta completa (renombra). El modo
// es el declarado o, si no, 0755 si el origen es ejecutable y 0644 si no.
func Install(m *manifest.Manifest, arch, root string) ([]File, error) {
	vars, err := m.SrcVars(arch)
	if err != nil {
		return nil, err
	}
	var out []File
	for i, it := range m.Install {
		field := fmt.Sprintf("install[%d]", i)
		src, err := manifest.Render(it.Src, vars)
		if err != nil {
			return nil, assembleErr("%s.src: %v", field, err)
		}
		matches := []string{src}
		glob := manifest.IsGlob(src)
		if glob {
			if matches, err = globFiles(root, src); err != nil {
				return nil, assembleErr("%s.src: %v", field, err)
			}
			if len(matches) == 0 {
				return nil, assembleErr("%s.src: el patrón %q no seleccionó ningún archivo", field, src)
			}
			if len(matches) > 1 && !strings.HasSuffix(it.Dst, "/") {
				return nil, assembleErr("%s.dst: el patrón %q selecciona %d archivos; dst debe terminar en \"/\"",
					field, src, len(matches))
			}
		}
		for _, rel := range matches {
			full, err := within(root, rel)
			if err != nil {
				return nil, assembleErr("%s.src: %v", field, err)
			}
			fi, err := os.Lstat(full)
			if err != nil {
				return nil, assembleErr("%s.src: no existe %q en el artefacto", field, rel)
			}
			if !fi.Mode().IsRegular() {
				return nil, assembleErr("%s.src: %q no es un archivo regular", field, rel)
			}
			mode := modeFor(fi.Mode())
			if it.Mode != "" {
				v, err := strconv.ParseUint(it.Mode, 8, 32)
				if err != nil {
					return nil, assembleErr("%s.mode: %v", field, err)
				}
				mode = os.FileMode(v)
			}
			out = append(out, File{Dst: manifest.InstallDst(it.Dst, rel), Src: full, Mode: mode})
		}
	}
	return out, nil
}

// globFiles resuelve un glob relativo a root y devuelve solo archivos
// regulares, en orden.
func globFiles(root, pattern string) ([]string, error) {
	full, err := within(root, pattern)
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(full)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		if fi, err := os.Lstat(m); err == nil && fi.Mode().IsRegular() {
			rel, _ := filepath.Rel(root, m)
			out = append(out, filepath.ToSlash(rel))
		}
	}
	sort.Strings(out)
	return out, nil
}

// within une root y rel y comprueba que el resultado no salga de root.
func within(root, rel string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", fmt.Errorf("ruta fuera del artefacto %q", rel)
	}
	return full, nil
}

// Sorted ordena por destino y falla si un destino se repite (después de
// resolver globs, man pages y completions).
func Sorted(files []File) ([]File, error) {
	out := append([]File(nil), files...)
	sort.Slice(out, func(i, j int) bool { return out[i].Dst < out[j].Dst })
	for i := 1; i < len(out); i++ {
		if out[i].Dst == out[i-1].Dst {
			return nil, assembleErr("destino duplicado %s", out[i].Dst)
		}
	}
	return out, nil
}

func assembleErr(format string, args ...any) *report.Error {
	return report.Newf(report.Assemble, format, args...)
}
