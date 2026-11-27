package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"debfactory/internal/report"
)

// CheckDebianDir valida la carpeta packages/<name>/debian/ de un paquete de
// nivel 2 (FR-003, data-model §1): control, rules ejecutable y copyright;
// sin changelog (lo genera la herramienta); Source: igual al nombre; en esta
// fase exactamente un paquete binario, con el mismo nombre y Architecture: any.
func CheckDebianDir(root string, m *Manifest) error {
	var errs report.Errors
	add := func(field, format string, args ...any) {
		errs = append(errs, validateErr(m.Name, m.Version, m.File, field, fmt.Sprintf(format, args...)))
	}
	dir := filepath.Join(root, m.DebianDir())
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		add("debian", "falta la carpeta %s/", m.DebianDir())
		return errs.Err()
	}
	for _, f := range []string{"control", "rules", "copyright"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			add("debian/"+f, "falta %s/%s", m.DebianDir(), f)
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "rules")); err == nil && st.Mode().Perm()&0o111 == 0 {
		add("debian/rules", "debe ser ejecutable")
	}
	if _, err := os.Stat(filepath.Join(dir, "changelog")); err == nil {
		add("debian/changelog", "no se incluye: lo genera la herramienta por suite (FR-002)")
	}
	// Nada fuera de archivos regulares y directorios: un enlace podría apuntar
	// fuera de la carpeta al copiarla al paquete fuente.
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type()&fs.ModeSymlink != 0 {
			rel, _ := filepath.Rel(root, p)
			add("debian", "%s es un enlace simbólico; solo se admiten archivos", rel)
		}
		return nil
	})
	if data, err := os.ReadFile(filepath.Join(dir, "control")); err == nil {
		checkControl(data, m, add)
	}
	return errs.Err()
}

// checkControl revisa los párrafos de debian/control.
func checkControl(data []byte, m *Manifest, add func(field, format string, args ...any)) {
	paras := controlParagraphs(data)
	if len(paras) == 0 {
		add("debian/control", "está vacío")
		return
	}
	if src := paras[0]["source"]; src != m.Name {
		add("debian/control", "Source: %q no coincide con el nombre del paquete %q", src, m.Name)
	}
	bins := paras[1:]
	if len(bins) != 1 {
		add("debian/control", "debe declarar exactamente un paquete binario (hay %d)", len(bins))
		return
	}
	if p := bins[0]["package"]; p != m.Name {
		add("debian/control", "Package: %q no coincide con el nombre del paquete %q", p, m.Name)
	}
	if a := bins[0]["architecture"]; a != "any" {
		add("debian/control", "Architecture: debe ser any (hay %q)", a)
	}
}

// controlParagraphs separa debian/control en párrafos con los campos en
// minúsculas (solo el primer renglón de cada campo).
func controlParagraphs(data []byte) []map[string]string {
	var out []map[string]string
	cur := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = map[string]string{}
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if ok {
			cur[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
