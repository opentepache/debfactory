package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"debfactory/internal/license"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
)

// licensePrefixes son los comienzos habituales del nombre del archivo de
// licencia (sin distinguir mayúsculas).
var licensePrefixes = []string{"license", "licence", "copying", "copyright", "unlicense", "ofl"}

// depsLicenses aplica el gate de licencias a cada dependencia del lenguaje
// incluida en el paquete fuente (FR-027, constitución IV 1.12.0): su texto de
// licencia MUST existir y cada licencia detectada MUST estar en la allowlist o
// en la excepción del manifiesto para esa dependencia. Con excepción, un texto
// que la detección no reconoce se acepta: la excepción lo declara y se revisa
// en la PR (y el contenido de la dependencia está fijado por su hash).
func (e *Env) depsLicenses(m *manifest.Manifest, dir string, deps []lock.Dep) error {
	exc := map[string]manifest.LicenseException{}
	for _, x := range m.LicenseExceptions {
		if x.Dependency != "" {
			exc[x.Dependency] = x
		}
	}
	used := map[string]bool{}
	var bad []string
	for _, d := range deps {
		name := d.Name
		if name == "" {
			name = d.Hash
		}
		x, hasExc := exc[d.Name]
		used[d.Name] = hasExc
		texts, err := e.depLicenseTexts(m, filepath.Join(dir, d.Hash), x)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if len(texts) == 0 {
			bad = append(bad, name+": sin archivo de licencia (declara license_exceptions con repo_file)")
			continue
		}
		var ids []string
		for _, t := range texts {
			ids = append(ids, license.Detect(t).IDs...)
		}
		if len(ids) == 0 && !hasExc {
			bad = append(bad, name+": licencia no reconocida (declárala en license_exceptions)")
			continue
		}
		for _, id := range ids {
			if e.Cfg.LicenseAllowed(id) || license.Matches(id, e.Cfg.Licenses) || license.Matches(id, x.Licenses) {
				continue
			}
			bad = append(bad, fmt.Sprintf("%s: %s no está en la allowlist ni en una excepción del paquete", name, id))
		}
	}
	for dep := range exc {
		if !used[dep] {
			bad = append(bad, fmt.Sprintf("license_exceptions: la dependencia %q no está entre las resueltas", dep))
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		bad = compactStrings(bad)
		return fmt.Errorf("licencias de las dependencias: %s", strings.Join(bad, "; "))
	}
	return nil
}

// depLicenseTexts devuelve los textos de licencia de una dependencia: el que
// indica la excepción (file dentro de la dependencia o repo_file en
// packages/<p>/) o, si no, los archivos de licencia de su raíz y de LICENSES/.
func (e *Env) depLicenseTexts(m *manifest.Manifest, dir string, x manifest.LicenseException) ([][]byte, error) {
	switch {
	case x.File != "":
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(x.File)))
		if err != nil {
			return nil, fmt.Errorf("license_exceptions.file: %v", err)
		}
		return [][]byte{b}, nil
	case x.RepoFile != "":
		b, err := os.ReadFile(filepath.Join(e.Root, "packages", m.Name, filepath.FromSlash(x.RepoFile)))
		if err != nil {
			return nil, fmt.Errorf("license_exceptions.repo_file: %v", err)
		}
		return [][]byte{b}, nil
	}
	var out [][]byte
	read := func(d string, all bool) {
		ents, _ := os.ReadDir(d)
		for _, en := range ents {
			n := strings.ToLower(en.Name())
			if !en.Type().IsRegular() || strings.HasPrefix(n, "._") {
				continue
			}
			if !all && !hasAnyPrefix(n, licensePrefixes) {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(d, en.Name())); err == nil {
				out = append(out, b)
			}
		}
	}
	read(dir, false)
	read(filepath.Join(dir, "LICENSES"), true)
	return out, nil
}

func hasAnyPrefix(s string, ps []string) bool {
	for _, p := range ps {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func compactStrings(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}
