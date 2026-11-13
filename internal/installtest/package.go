package installtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
