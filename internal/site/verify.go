package site

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"debfactory/internal/report"
)

// Verify comprueba que el modelo coincide con los índices Packages de la
// generación genDir, en los dos sentidos (data-model §1, invariante 1;
// FR-009): cada (paquete, versión, suite, arquitectura) del modelo está en el
// Packages de esa suite y arquitectura, y cada entrada de los índices está en
// el modelo.
func Verify(m *Model, genDir string) error {
	want := map[string]bool{} // "suite arch paquete versión"
	for _, p := range m.Packages {
		for _, v := range p.Versions {
			for _, s := range v.Suites {
				for _, a := range v.Architectures {
					want[strings.Join([]string{s, a, p.Name, v.SuiteVersion(s)}, " ")] = true
				}
			}
		}
	}
	var errs report.Errors
	for _, s := range m.Repo.Suites {
		for _, a := range m.Repo.Architectures {
			rel := filepath.Join("dists", s.Name, m.Repo.Component, "binary-"+a, "Packages")
			data, err := os.ReadFile(filepath.Join(genDir, rel))
			if err != nil {
				errs = append(errs, siteErr("falta %s en la generación: %v", rel, err))
				continue
			}
			got := map[string]bool{}
			for _, e := range packagesEntries(data) {
				key := strings.Join([]string{s.Name, a, e[0], e[1]}, " ")
				got[key] = true
				if !want[key] {
					errs = append(errs, &report.Error{Package: e[0], Version: e[1], Stage: report.Site,
						Cause: fmt.Sprintf("está en %s pero el estado no la publica en %s/%s", rel, s.Name, a)})
				}
			}
			for key := range want {
				f := strings.Fields(key)
				if f[0] == s.Name && f[1] == a && !got[key] {
					errs = append(errs, &report.Error{Package: f[2], Version: f[3], Stage: report.Site,
						Cause: fmt.Sprintf("el estado la publica en %s/%s pero no está en %s", s.Name, a, rel)})
				}
			}
		}
	}
	slices.SortFunc(errs, func(a, b *report.Error) int { return strings.Compare(a.Error(), b.Error()) })
	return errs.Err()
}

// packagesEntries devuelve los pares (Package, Version) de un índice Packages.
func packagesEntries(data []byte) [][2]string {
	var out [][2]string
	var pkg, ver string
	flush := func() {
		if pkg != "" {
			out = append(out, [2]string{pkg, ver})
		}
		pkg, ver = "", ""
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "Package: "):
			pkg = strings.TrimSpace(strings.TrimPrefix(line, "Package: "))
		case strings.HasPrefix(line, "Version: "):
			ver = strings.TrimSpace(strings.TrimPrefix(line, "Version: "))
		}
	}
	flush()
	return out
}
