package installtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
)

// TestPackage prueba dist/<name>_<version>_<arch>.deb (ya construido) en las
// suites indicadas (vacío = todas), escribe su .test.json y devuelve un error
// de etapa test si el resultado global es fail.
func TestPackage(ctx context.Context, root string, cfg *config.Config, m *manifest.Manifest, arch string,
	only []string, out *report.Printer) (*Report, error) {
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
	rep := Run(ctx, d, m.Version, m.Revision, suites, m.Exclude.Suites)
	rep.DebSHA256 = sha
	if err := rep.Write(base + ".test.json"); err != nil {
		return rep, fail("%v", err)
	}
	rep.Print(out)
	if rep.Overall != "pass" {
		return rep, fail("la prueba de instalación falló (dist/%s.test.json)", m.DebBase(arch))
	}
	return rep, nil
}
