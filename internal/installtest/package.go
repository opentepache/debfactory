package installtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"debfactory/internal/config"
	"debfactory/internal/container"
	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
)

// Options ajusta TestPackage.
type Options struct {
	Only    []string // suites a probar (vacío = todas)
	Limiter Limiter  // pruebas simultáneas entre paquetes (nil = una)
	Force   bool     // ignora un .test.json aprobado que ya cubre este .deb
}

// TestPackage prueba dist/<name>_<version>_<arch>.deb (ya construido) en las
// suites indicadas, escribe su .test.json y devuelve un error de etapa test si
// el resultado global es fail. Si el .test.json existente ya aprobó el mismo
// .deb en esas suites con las mismas imágenes de prueba, lo reutiliza.
func TestPackage(ctx context.Context, root string, cfg *config.Config, m *manifest.Manifest, arch string,
	o Options, out *report.Printer) (*Report, error) {
	if m.Level2() {
		return testLevel2(ctx, root, cfg, m, arch, o, out)
	}
	only := o.Only
	fail := func(format string, args ...any) error {
		return &report.Error{Package: m.Name, Version: m.Version, Arch: arch, Stage: report.Test,
			Cause: fmt.Sprintf(format, args...)}
	}
	base := filepath.Join(root, "dist", m.DebBase(arch))
	if _, err := os.Stat(base + ".deb"); err != nil {
		return nil, fail("no existe dist/%s.deb; ejecuta `debfactory build %s`", m.DebBase(arch), m.Name)
	}
	info, err := pkg.ReadBuildInfo(base + ".build.json")
	if err != nil {
		return nil, fail("%v", err)
	}
	vars, err := m.Vars(arch)
	if err != nil {
		return nil, fail("%v", err)
	}
	expect, err := manifest.Render(m.Test.Expect, vars)
	if err != nil {
		return nil, fail("test.expect: %v", err)
	}
	suites := cfg.Suites
	if len(only) > 0 {
		suites = nil
		for _, name := range only {
			s, ok := cfg.Suite(name)
			if !ok {
				return nil, fail("suite desconocida %q", name)
			}
			suites = append(suites, s)
		}
	}
	sha, err := fetch.SHA256File(base + ".deb")
	if err != nil {
		return nil, fail("%v", err)
	}
	d := Deb{Path: base + ".deb", Package: m.Name, Arch: arch, TestCommand: info.TestCommand, Expect: expect}
	if !o.Force {
		if prev, ok := cached(ctx, base+".test.json", m, sha, checkKey(d), arch, suites); ok {
			out.Line("%s %s %s: sin cambios desde la última prueba aprobada (mismo .deb e imágenes; --force la repite)",
				m.Name, m.DebVersion(), arch)
			prev.Print(out)
			return prev, nil
		}
	}
	rep := Run(ctx, d, m.Version, m.Revision, suites, m.Exclude.Suites, o.Limiter)
	rep.DebSHA256, rep.CheckKey = sha, checkKey(d)
	if err := rep.Write(base + ".test.json"); err != nil {
		return rep, fail("%v", err)
	}
	rep.Print(out)
	if rep.Overall != "pass" {
		return rep, fail("la prueba de instalación falló (dist/%s.test.json)", m.DebBase(arch))
	}
	return rep, nil
}

// cached devuelve el informe anterior si aprobó este mismo .deb (sha), con la
// misma comprobación (key), en cada suite pedida, con las exclusiones actuales del manifiesto y con la imagen de
// prueba que hay ahora. Las imágenes se renuevan cada día (imageMaxAge), así
// que la caché nunca dura más que los índices de APT de la imagen.
func cached(ctx context.Context, path string, m *manifest.Manifest, sha, key, arch string,
	suites []config.Suite) (*Report, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var prev Report
	if json.Unmarshal(data, &prev) != nil || prev.Overall != "pass" || prev.Versions != "pass" ||
		prev.DebSHA256 != sha || prev.CheckKey != key || prev.Version != m.DebVersion() || prev.Arch != arch {
		return nil, false
	}
	bySuite := map[string]Result{}
	for _, r := range prev.Results {
		bySuite[r.Suite] = r
	}
	for _, s := range suites {
		r, ok := bySuite[s.Name]
		if _, excluded := m.Exclude.Suites[s.Name]; excluded {
			if !ok || r.Status != "excluded" {
				return nil, false
			}
			continue
		}
		if !ok || r.Status != "pass" || r.Image == "" {
			return nil, false
		}
		if EnsureImage(ctx, s, arch) != nil {
			return nil, false
		}
		if id, err := container.ImageID(ctx, ImageTag(s.Name, arch)); err != nil || id != r.Image {
			return nil, false
		}
	}
	return &prev, true
}

// testLevel2 prueba cada binario de nivel 2 solo en su suite
// (specs/008-source-builds, FR-006): dist/<name>_<versión+sufijo>_<arch>.deb
// en un contenedor limpio de esa suite, con la prueba de ciclo de vida, y
// escribe un .test.json por binario. Devuelve el informe de la última suite
// y un error si alguna falla (constitución VII: falla en una, no se publica
// en ninguna).
func testLevel2(ctx context.Context, root string, cfg *config.Config, m *manifest.Manifest, arch string,
	o Options, out *report.Printer) (*Report, error) {
	var errs report.Errors
	var last *Report
	for _, s := range cfg.Suites {
		if _, excluded := m.Exclude.Suites[s.Name]; excluded {
			continue
		}
		if len(o.Only) > 0 && !slices.Contains(o.Only, s.Name) {
			continue
		}
		version := m.SuiteVersion(s.Level2Suffix)
		base := fmt.Sprintf("%s_%s_%s", m.Name, version, arch)
		fail := func(format string, args ...any) {
			errs = append(errs, &report.Error{Package: m.Name, Version: m.Version, Arch: s.Name + "/" + arch,
				Stage: report.Test, Cause: fmt.Sprintf(format, args...)})
		}
		path := filepath.Join(root, "dist", base)
		if _, err := os.Stat(path + ".deb"); err != nil {
			fail("no existe dist/%s.deb; ejecuta `debfactory build %s`", base, m.Name)
			continue
		}
		info, err := pkg.ReadBuildInfo(path + ".build.json")
		if err != nil {
			fail("%v", err)
			continue
		}
		vars, err := m.Vars("")
		if err != nil {
			fail("%v", err)
			continue
		}
		expect, err := manifest.Render(m.Test.Expect, vars)
		if err != nil {
			fail("test.expect: %v", err)
			continue
		}
		command := info.TestCommand
		if command == "" {
			command = m.Name + " --version"
		}
		sha, err := fetch.SHA256File(path + ".deb")
		if err != nil {
			fail("%v", err)
			continue
		}
		d := Deb{Path: path + ".deb", Package: m.Name, Arch: arch, TestCommand: command, Expect: expect, Lifecycle: true}
		suites := []config.Suite{s}
		if !o.Force {
			if prev, ok := cachedLevel2(ctx, path+".test.json", version, sha, checkKey(d), arch, s); ok {
				out.Line("%s %s %s: sin cambios desde la última prueba aprobada (--force la repite)", m.Name, version, arch)
				prev.Print(out)
				last = prev
				continue
			}
		}
		rep := Run(ctx, d, m.Version, m.Revision, suites, nil, o.Limiter)
		rep.Version, rep.DebSHA256, rep.CheckKey = version, sha, checkKey(d)
		if err := rep.Write(path + ".test.json"); err != nil {
			fail("%v", err)
			continue
		}
		rep.Print(out)
		last = rep
		if rep.Overall != "pass" {
			fail("la prueba de instalación falló (dist/%s.test.json)", base)
		}
	}
	return last, errs.Err()
}

// cachedLevel2 es cached para un binario de nivel 2 y su única suite.
func cachedLevel2(ctx context.Context, path, version, sha, key, arch string, s config.Suite) (*Report, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var prev Report
	if json.Unmarshal(data, &prev) != nil || prev.Overall != "pass" || prev.DebSHA256 != sha || prev.CheckKey != key ||
		prev.Version != version || prev.Arch != arch || len(prev.Results) != 1 || prev.Results[0].Suite != s.Name {
		return nil, false
	}
	if EnsureImage(ctx, s, arch) != nil {
		return nil, false
	}
	if id, err := container.ImageID(ctx, ImageTag(s.Name, arch)); err != nil || id != prev.Results[0].Image {
		return nil, false
	}
	return &prev, true
}
