package repo

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/report"
)

// GuideFile es la guía de usuario (contracts/usuario.md).
const GuideFile = "docs/usuario.md"

// PlaceholderURL es la URL de la guía mientras repo.url esté vacía.
const PlaceholderURL = "https://apt.example.org"

var (
	guideRow     = regexp.MustCompile("^\\| `([a-z0-9][a-z0-9+.-]+)` \\|[^|]*\\| ([^|]*)\\|")
	fingerprints = regexp.MustCompile(`(?m)^Fingerprint: (.+)$`)
)

// VerifyGuide comprueba que docs/usuario.md tiene la URL y los fingerprints
// de la configuración y que su tabla de paquetes por suite coincide con el
// estado (FR-030).
func VerifyGuide(root string, cfg *config.Config, st *State) error {
	data, err := os.ReadFile(filepath.Join(root, GuideFile))
	if err != nil {
		return report.Newf(report.VerifyRepo, "%s: %v", GuideFile, err)
	}
	guide := string(data)
	var errs report.Errors
	fail := func(format string, args ...any) {
		errs = append(errs, report.Newf(report.VerifyRepo, GuideFile+": "+format, args...))
	}
	url := cfg.Repo.URL
	if url == "" {
		url = PlaceholderURL
	}
	if !strings.Contains(guide, strings.TrimSuffix(url, "/")+"/") {
		fail("no contiene la URL del repositorio %s", url)
	}
	var shown []string
	for _, m := range fingerprints.FindAllStringSubmatch(guide, -1) {
		shown = append(shown, strings.ReplaceAll(m[1], " ", ""))
	}
	for _, f := range cfg.Repo.Signing.Masters {
		if !contains(shown, f) {
			fail("falta el fingerprint de la llave maestra %s (como `Fingerprint: %s`)", f, GroupFingerprint(f))
		}
	}
	if st.Serial > 0 {
		table := map[string][]string{} // paquete -> suites en las que falta
		for _, line := range strings.Split(guide, "\n") {
			m := guideRow.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			var missing []string
			for _, su := range cfg.Suites {
				if regexp.MustCompile(`\b` + su.Name + `\b`).MatchString(m[2]) {
					missing = append(missing, su.Name)
				}
			}
			table[m[1]] = missing
		}
		for _, name := range st.PackageNames() {
			newest := st.Packages[name][0]
			for _, p := range st.Packages[name][1:] {
				if gt, _ := cmpGT(p.Version, newest.Version); gt {
					newest = p
				}
			}
			var want []string
			for _, su := range cfg.Suites {
				if !contains(newest.Suites, su.Name) {
					want = append(want, su.Name)
				}
			}
			got, ok := table[name]
			if !ok {
				fail("la tabla de paquetes no incluye %s", name)
				continue
			}
			sort.Strings(want)
			sort.Strings(got)
			if strings.Join(want, " ") != strings.Join(got, " ") {
				fail("la tabla dice que %s falta en [%s], pero el estado dice [%s]", name,
					strings.Join(got, " "), strings.Join(want, " "))
			}
		}
	}
	return errs.Err()
}

// GroupFingerprint da el formato de gpg: 10 grupos de 4, con doble espacio
// en medio.
func GroupFingerprint(f string) string {
	var g []string
	for i := 0; i+4 <= len(f); i += 4 {
		g = append(g, f[i:i+4])
	}
	if len(g) != 10 {
		return f
	}
	return strings.Join(g[:5], " ") + "  " + strings.Join(g[5:], " ")
}

func cmpGT(a, b string) (bool, error) {
	return compareVersions(a, "gt", b)
}
