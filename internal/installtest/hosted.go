package installtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/pkg"
	"debfactory/internal/report"
)

// TestDeb prueba un .deb en una suite con la arquitectura nativa (modo job
// hospedado, specs/009 T027): instalación, orden de comprobación y, en nivel
// 2, ciclo de vida. Deja out/test.json con el sha256 del .deb probado. La
// orden sale del build.json de nivel 1 junto al .deb si existe; si no, del
// manifiesto.
func TestDeb(ctx context.Context, cfg *config.Config, m *manifest.Manifest, suite, arch, deb, out string) (*Report, error) {
	fail := func(stage report.Stage, err error) error {
		return report.Wrap(err, stage, m.Name, m.Version, suite+"/"+arch)
	}
	i := slices.IndexFunc(cfg.Suites, func(s config.Suite) bool { return s.Name == suite })
	if i < 0 {
		return nil, fail(report.Validate, fmt.Errorf("suite %q no está en la configuración", suite))
	}
	if _, excluded := m.Exclude.Suites[suite]; excluded {
		return nil, fail(report.Validate, fmt.Errorf("el manifiesto excluye la suite %s", suite))
	}
	if !slices.Contains(m.Arches(cfg.Arches), arch) {
		return nil, fail(report.Validate, fmt.Errorf("el manifiesto excluye la arquitectura %s", arch))
	}
	vars, err := m.Vars(arch)
	if err != nil {
		return nil, fail(report.Validate, err)
	}
	expect, err := manifest.Render(m.Test.Expect, vars)
	if err != nil {
		return nil, fail(report.Validate, fmt.Errorf("test.expect: %v", err))
	}
	command := m.Test.Command
	if info, err := pkg.ReadBuildInfo(strings.TrimSuffix(deb, ".deb") + ".build.json"); err == nil && info.TestCommand != "" {
		command = info.TestCommand
	}
	if command == "" {
		command = m.Name + " --version"
	}
	sha, err := fetch.SHA256File(deb)
	if err != nil {
		return nil, fail(report.Inputs, err)
	}
	d := Deb{Path: deb, Package: m.Name, Arch: arch, TestCommand: command, Expect: expect, Lifecycle: m.Level2()}
	rep := Run(ctx, d, m.Version, m.Revision, []config.Suite{cfg.Suites[i]}, nil, nil)
	rep.DebSHA256, rep.CheckKey = sha, checkKey(d)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, fail(report.Test, err)
	}
	if err := rep.Write(filepath.Join(out, "test.json")); err != nil {
		return nil, fail(report.Test, err)
	}
	if rep.Overall != "pass" {
		return rep, fail(report.Test, fmt.Errorf("la prueba de instalación falló (%s)", filepath.Join(out, "test.json")))
	}
	return rep, nil
}
