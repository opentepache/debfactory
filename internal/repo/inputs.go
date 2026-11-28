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
	Paths     map[string]string // arch -> dist/….deb (nivel 1)
	Files     map[string]string // nombre en el pool -> dist/… (nivel 2)
}

// Source devuelve la ruta en dist/ de un archivo de la versión.
func (in *Input) Source(a Artifact) string {
	if in.Published.Level2() {
		return in.Files[a.Name]
	}
	return in.Paths[a.Arch]
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
	m, merr := manifest.Load(root, name)
	if merr == nil && m.Level2() && m.DebVersion() == version {
		return inputsLevel2(root, cfg, m)
	}
	// Arquitecturas de la versión (specs/009, exclude.arches): las del
	// manifiesto si es esa versión; si no, todas las de la configuración.
	arches := cfg.Arches
	if merr == nil && m.DebVersion() == version {
		arches = m.Arches(cfg.Arches)
	}
	var errs report.Errors
	fail := func(arch, format string, args ...any) {
		errs = append(errs, &report.Error{Package: name, Version: version, Arch: arch,
			Stage: report.Inputs, Cause: fmt.Sprintf(format, args...)})
	}
	in := &Input{Package: name, Published: Published{Version: version, Debs: map[string]string{}},
		Paths: map[string]string{}}
	suitesByArch := map[string][]string{}
	for _, arch := range arches {
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
		first := arches[0]
		for _, arch := range arches[1:] {
			if !slices.Equal(suitesByArch[first], suitesByArch[arch]) {
				fail("", "suites distintas entre %s [%s] y %s [%s]", first,
					strings.Join(suitesByArch[first], " "), arch, strings.Join(suitesByArch[arch], " "))
			}
		}
		in.Published.Suites = suitesByArch[first]
		if !slices.Equal(arches, cfg.Arches) {
			in.Published.Arches = slices.Clone(arches)
		}
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

// inputsLevel2 es Inputs para un paquete de nivel 2 (specs/008-source-builds,
// research D10): por cada suite no excluida, el .dsc y el .debian.tar.xz y,
// por arquitectura, el .deb con su build.json (lintian y doble compilación
// aprobados, mismo .dsc) y su test.json (aprobado en esa suite sobre ese
// mismo .deb) y el .buildinfo; el .orig es común. Si falta algo en una suite,
// no se publica en ninguna (constitución VII).
func inputsLevel2(root string, cfg *config.Config, m *manifest.Manifest) (*Input, error) {
	var errs report.Errors
	fail := func(where, format string, args ...any) {
		errs = append(errs, &report.Error{Package: m.Name, Version: m.DebVersion(), Arch: where,
			Stage: report.Inputs, Cause: fmt.Sprintf(format, args...)})
	}
	dist := filepath.Join(root, "dist")
	in := &Input{Package: m.Name, Files: map[string]string{},
		Published: Published{Version: m.DebVersion(), Level: 2, Builds: map[string]Build{}}}
	sha := func(where, file string) string {
		p := filepath.Join(dist, file)
		s, err := fetch.SHA256File(p)
		if err != nil {
			fail(where, "falta dist/%s; ejecuta `debfactory build %s`", file, m.Name)
			return ""
		}
		in.Files[file] = p
		return s
	}
	in.Published.Orig = &Orig{File: m.OrigName(), SHA256: sha("", m.OrigName())}
	for _, su := range cfg.Suites {
		if _, excluded := m.Exclude.Suites[su.Name]; excluded {
			continue
		}
		v := m.SuiteVersion(su.Level2Suffix)
		b := Build{Version: v, Debs: map[string]string{}, Buildinfo: map[string]string{}}
		b.DSC = sha(su.Name, DscFile(m.Name, v))
		b.DebianTar = sha(su.Name, DebianTarFile(m.Name, v))
		for _, arch := range m.Arches(cfg.Arches) {
			where := su.Name + "/" + arch
			base := fmt.Sprintf("%s_%s_%s", m.Name, v, arch)
			deb := sha(where, base+".deb")
			binfo := sha(where, base+".buildinfo")
			if deb == "" || binfo == "" {
				continue
			}
			bi, err := pkg.ReadBuildInfo(filepath.Join(dist, base+".build.json"))
			switch {
			case err != nil:
				fail(where, "falta dist/%s.build.json; ejecuta `debfactory build %s`", base, m.Name)
				continue
			case bi.Lintian != "pass" || !bi.Reproducible:
				fail(where, "build.json no aprobado (lintian: %s, reproducible: %v)", bi.Lintian, bi.Reproducible)
				continue
			case bi.SHA256 != deb || bi.BuildinfoSHA256 != binfo || bi.DSCSHA256 != b.DSC || bi.Suite != su.Name:
				fail(where, "los archivos de dist/ no coinciden con build.json; vuelve a ejecutar `debfactory build %s`", m.Name)
				continue
			}
			tr, err := readTestReport(filepath.Join(dist, base+".test.json"))
			switch {
			case err != nil:
				fail(where, "falta dist/%s.test.json; ejecuta `debfactory test %s`", base, m.Name)
				continue
			case tr.Overall != "pass" || len(tr.Results) != 1 || tr.Results[0].Suite != su.Name || tr.Results[0].Status != "pass":
				fail(where, "test.json no aprobado en %s", su.Name)
				continue
			case tr.DebSHA256 != deb:
				fail(where, "sha256 del .deb no coincide con test.json; vuelve a ejecutar `debfactory test %s`", m.Name)
				continue
			}
			b.Debs[arch], b.Buildinfo[arch] = deb, binfo
		}
		in.Published.Builds[su.Name] = b
		in.Published.Suites = append(in.Published.Suites, su.Name)
	}
	if len(in.Published.Suites) == 0 {
		fail("", "el manifiesto excluye todas las suites")
	}
	if a := m.Arches(cfg.Arches); !slices.Equal(a, cfg.Arches) {
		in.Published.Arches = slices.Clone(a)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return in, nil
}
