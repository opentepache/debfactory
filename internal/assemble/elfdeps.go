package assemble

import (
	"debug/elf"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ELFDepends calcula Depends desde las bibliotecas NEEDED de los ELF
// instalados (research D8): cada biblioteca se traduce con mapping y {glibc}
// se reemplaza por la versión GLIBC_x.y máxima requerida. Una biblioteca no
// mapeada es un error que nombra la biblioteca y el binario.
func ELFDepends(files []File, mapping map[string]string) ([]string, error) {
	needs := map[string]string{} // biblioteca -> primer binario que la pide
	var libs []string
	glibc := ""
	for _, f := range files {
		if !isELF(f.Src) {
			continue
		}
		ef, err := elf.Open(f.Src)
		if err != nil {
			return nil, assembleErr("%s: ELF ilegible: %v", f.Dst, err)
		}
		imported, err := ef.ImportedLibraries()
		if err != nil {
			ef.Close()
			return nil, assembleErr("%s: %v", f.Dst, err)
		}
		vneeds, _ := ef.DynamicVersionNeeds()
		ef.Close()
		for _, lib := range imported {
			if _, ok := needs[lib]; !ok {
				needs[lib] = f.Dst
				libs = append(libs, lib)
			}
		}
		for _, vn := range vneeds {
			for _, d := range vn.Needs {
				if v, ok := strings.CutPrefix(d.Dep, "GLIBC_"); ok && validVersion(v) && versionLess(glibc, v) {
					glibc = v
				}
			}
		}
	}
	set := map[string]bool{}
	for _, lib := range libs {
		dep, ok := mapping[lib]
		if !ok {
			return nil, assembleErr("%s necesita %s, que no está en elf_dependencies de config/debfactory.yaml",
				needs[lib], lib)
		}
		if strings.Contains(dep, "{glibc}") {
			if glibc == "" {
				dep = strings.TrimSpace(strings.Split(dep, "(")[0])
			} else {
				dep = strings.ReplaceAll(dep, "{glibc}", glibc)
			}
		}
		set[dep] = true
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}

func validVersion(v string) bool {
	for _, p := range strings.Split(v, ".") {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return v != ""
}

// versionLess compara versiones numéricas con puntos ("2.4" < "2.39").
func versionLess(a, b string) bool {
	if a == "" {
		return true
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// MergeDepends une las dependencias calculadas con las del manifiesto; si el
// manifiesto declara el mismo paquete, gana su versión.
func MergeDepends(computed, declared []string) []string {
	name := func(dep string) string {
		return strings.TrimSpace(strings.FieldsFunc(dep, func(r rune) bool { return r == ' ' || r == '(' })[0])
	}
	declaredNames := map[string]bool{}
	for _, d := range declared {
		if !strings.Contains(d, "|") {
			declaredNames[name(d)] = true
		}
	}
	var out []string
	for _, c := range computed {
		if !declaredNames[name(c)] {
			out = append(out, c)
		}
	}
	out = append(out, declared...)
	sort.Strings(out)
	return out
}

func isELF(p string) bool {
	fh, err := os.Open(p)
	if err != nil {
		return false
	}
	defer fh.Close()
	magic := make([]byte, 4)
	_, err = io.ReadFull(fh, magic)
	return err == nil && string(magic) == "\x7fELF"
}
