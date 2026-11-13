package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
)

// Input es una versión lista para publicar: su entrada de estado (sin
// fecha) y la ruta de cada .deb en dist/.
type Input struct {
	Package   string
	Published Published
	Paths     map[string]string // arch -> dist/….deb
}

// ManifestVersion devuelve la versión Debian completa del manifiesto.
func ManifestVersion(root string, cfg *config.Config, name string) (string, error) {
	m, err := manifest.Validate(root, cfg, name)
	if err != nil {
		return "", err
	}
	return m.DebVersion(), nil
}

// Inputs comprueba que cada arquitectura de la versión tenga un .deb con
// lintian y prueba de instalación aprobados sobre ese mismo archivo, y que la
// prueba cubra todas las suites (FR-007, constitución VII; research D8).
// Devuelve todos los errores, no solo el primero.
func Inputs(root string, cfg *config.Config, name, version string) (*Input, error) {
	var errs report.Errors
	fail := func(arch, format string, args ...any) {
		errs = append(errs, &report.Error{Package: name, Version: version, Arch: arch,
			Stage: report.Inputs, Cause: fmt.Sprintf(format, args...)})
	}
	in := &Input{Package: name, Published: Published{Version: version, Debs: map[string]string{}},
		Paths: map[string]string{}}
	suitesByArch := map[string][]string{}
	for _, arch := range cfg.Arches {
		base := fmt.Sprintf("%s_%s_%s", name, version, arch)
		deb := filepath.Join(root, "dist", base+".deb")
		if _, err := os.Stat(deb); err != nil {
			fail(arch, "falta dist/%s.deb", base)
			continue
		}
		sha, err := fetch.SHA256File(deb)
		if err != nil {
			fail(arch, "%v", err)
			continue
		}
		bi, err := pkg.ReadBuildInfo(filepath.Join(root, "dist", base+".build.json"))
		switch {
		case err != nil:
			fail(arch, "falta dist/%s.build.json; ejecuta `debfactory build %s`", base, name)
			continue
		case bi.Lintian != "pass":
			fail(arch, "build.json no aprobado (lintian: %s)", bi.Lintian)
			continue
		case bi.SHA256 != sha:
			fail(arch, "sha256 del .deb no coincide con build.json; vuelve a ejecutar `debfactory build %s`", name)
			continue
		}
		tr, err := readTestReport(filepath.Join(root, "dist", base+".test.json"))
		switch {
		case err != nil:
			fail(arch, "falta dist/%s.test.json; ejecuta `debfactory test %s`", base, name)
			continue
		case tr.Overall != "pass":
			fail(arch, "test.json no aprobado (overall: %s)", tr.Overall)
			continue
		case tr.DebSHA256 == "":
			fail(arch, "test.json sin deb_sha256; vuelve a ejecutar `debfactory test %s`", name)
			continue
		case tr.DebSHA256 != sha:
			fail(arch, "sha256 del .deb no coincide con test.json; vuelve a ejecutar `debfactory test %s`", name)
			continue
		}
		status := map[string]string{}
		for _, r := range tr.Results {
			status[r.Suite] = r.Status
		}
		var suites []string
		complete := true
		for _, su := range cfg.Suites {
			switch status[su.Name] {
			case "pass":
				suites = append(suites, su.Name)
			case "excluded":
			default:
				fail(arch, "test.json no cubre %s; ejecuta `debfactory test %s` sin --suite", su.Name, name)
				complete = false
			}
		}
		if !complete {
			continue
		}
		if len(suites) == 0 {
			fail(arch, "test.json no tiene ninguna suite aprobada")
			continue
		}
		suitesByArch[arch] = suites
		in.Published.Debs[arch] = sha
		in.Paths[arch] = deb
	}
	if len(errs) == 0 {
		first := cfg.Arches[0]
		for _, arch := range cfg.Arches[1:] {
			if !slices.Equal(suitesByArch[first], suitesByArch[arch]) {
				fail("", "suites distintas entre %s [%s] y %s [%s]", first,
					strings.Join(suitesByArch[first], " "), arch, strings.Join(suitesByArch[arch], " "))
			}
		}
		in.Published.Suites = suitesByArch[first]
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return in, nil
}

func readTestReport(path string) (*installtest.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r installtest.Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}
