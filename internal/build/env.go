// Package build orquesta las órdenes lock y build siguiendo el Flujo de la
// constitución: validar → descargar y verificar → licencias → construir →
// lintian.
package build

import (
	"context"
	"errors"
	"fmt"
	"path"

	"debfactory/internal/assemble"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/lint"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/srcbuild"
	"debfactory/internal/srcpkg"
	"debfactory/internal/supply"
)

// Env reúne lo que comparten lock y build.
type Env struct {
	Root    string // raíz del repositorio (config/, packages/, dist/)
	Workdir string // directorio de trabajo (tmpfs)
	Cfg     *config.Config
	Fetcher *fetch.Fetcher
	GitHub  *fetch.GitHub
	Out     *report.Printer

	// Lint pasa lintian a un .deb (por defecto lint.Run, en contenedor).
	Lint func(ctx context.Context, cfg *config.Config, deb string) (*lint.Result, error)
	// Generate ejecuta las órdenes de completions (por defecto en contenedor).
	Generate assemble.Generator
	// LicenseCheck es el gate de licencias que aplica Lock (por defecto
	// license.Gate). El detector de versiones (fase 005) lo sustituye para
	// clasificar un cambio de licencia en lugar de solo rechazarlo.
	LicenseCheck func(text []byte, declared string, cfg *config.Config) error
	// SourceGen y BinaryBuild generan el paquete fuente y compilan un
	// paquete de nivel 2 (por defecto en contenedores de la suite,
	// internal/srcbuild); las pruebas sin Docker los sustituyen.
	SourceGen   func(ctx context.Context, s config.Suite, in srcpkg.Input, work string) (*srcbuild.Source, error)
	BinaryBuild func(ctx context.Context, spec srcbuild.Spec, out string) (*srcbuild.Binary, error)
	// Sigstore verifica bundles de sigstore (por defecto cosign en un
	// contenedor; las pruebas lo sustituyen).
	Sigstore func(ctx context.Context, s *manifest.SigstoreSig, o supply.Origin, file, bundle string) error
	// Provenance comprueba la procedencia de upstream de un archivo (por
	// defecto, atestaciones de GitHub verificadas con cosign; specs/009).
	Provenance func(ctx context.Context, repo, tag, file, sha256 string) (*supply.Provenance, error)
	// ZigFetcher crea el ZigFetcher (por defecto zig fetch en un contenedor).
	ZigFetcher ZigFetcherFactory
}

// Defaults completa Lint y Generate con las implementaciones en contenedor.
func (e *Env) Defaults() {
	if e.Lint == nil {
		e.Lint = lint.Run
	}
	if e.Generate == nil {
		e.Generate = assemble.ContainerGenerator(e.Cfg.CompletionsImage)
	}
	if e.SourceGen == nil {
		e.SourceGen = srcbuild.GenerateSource
	}
	if e.BinaryBuild == nil {
		e.BinaryBuild = srcbuild.Build
	}
}

// downloadErr traduce un error de descarga: un hash distinto es de la etapa
// verify (con esperado y obtenido alineados); lo demás, de fetch.
func downloadErr(err error, m *manifest.Manifest, arch, origin string) error {
	verify := func(cause, from, want, got string) error {
		label := "esperado (" + from + "):"
		w := len(label) + 2
		return &report.Error{Package: m.Name, Version: m.Version, Arch: arch, Stage: report.Verify, Cause: cause,
			Detail: []string{fmt.Sprintf("%-*s%s", w, label, want), fmt.Sprintf("%-*s%s", w, "obtenido:", got)}}
	}
	var hm *fetch.HashMismatch
	if errors.As(err, &hm) {
		return verify("sha256 no coincide para "+path.Base(hm.URL), origin, hm.Expected, hm.Got)
	}
	var cm *fetch.ChecksumMismatch
	if errors.As(err, &cm) {
		return verify(cm.Error(), cm.File, cm.Expected, cm.Got)
	}
	return report.Wrap(err, report.Fetch, m.Name, m.Version, arch)
}
