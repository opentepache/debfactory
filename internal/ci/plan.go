package ci

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/installtest"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
)

// Reasons de AffectedPackage (data-model.md §2).
const (
	NewPackage  = "new-package"
	NewVersion  = "new-version"
	NewRevision = "new-revision"
	Rebuild     = "rebuild"
)

// AffectedPackage es un paquete que esta ejecución construye.
type AffectedPackage struct {
	Package string `json:"package"`
	Version string `json:"version"` // versión Debian completa
	Commit  string `json:"commit"`  // commit cuyo manifiesto y lockfile se construyen
	Reason  string `json:"reason"`
}

// PkgVersion es un paquete y una versión.
type PkgVersion struct {
	Package string `json:"package"`
	Version string `json:"version"`
}

// Skip es un paquete que no se procesa, con el motivo.
type Skip struct {
	Package string `json:"package"`
	Reason  string `json:"reason"`
}

// PlanError es un paquete que no se puede procesar.
type PlanError struct {
	Package string       `json:"package"`
	Version string       `json:"version,omitempty"`
	Stage   report.Stage `json:"stage"`
	Reason  string       `json:"reason"`
}

// Reference es el estado del que parte la ejecución (D6, D8).
type Reference struct {
	Source string `json:"source"` // main | state-pr
	SHA256 string `json:"sha256"`
}

// Plan dice qué hace una ejecución (data-model.md §2).
type Plan struct {
	Mode      string            `json:"mode"`
	Reference Reference         `json:"reference"`
	Publish   []AffectedPackage `json:"publish"`
	Test      []AffectedPackage `json:"test"`
	Compare   []PkgVersion      `json:"compare"`
	Skipped   []Skip            `json:"skipped"`
	Errors    []PlanError       `json:"errors"`
	CodeDiff  []string          `json:"code_changes,omitempty"` // archivos de ci.code_paths cambiados
}

// Err convierte los errores del plan en report.Errors.
func (p *Plan) Err() report.Errors {
	var errs report.Errors
	for _, e := range p.Errors {
		errs = append(errs, &report.Error{Package: e.Package, Version: e.Version, Stage: e.Stage, Cause: e.Reason})
	}
	return errs
}

// Summary es una línea legible del plan.
func (p *Plan) Summary() string {
	var parts []string
	list := func(verb string, aps []AffectedPackage) {
		if len(aps) == 0 {
			return
		}
		var s []string
		for _, a := range aps {
			s = append(s, fmt.Sprintf("%s %s (%s)", a.Package, a.Version, a.Reason))
		}
		parts = append(parts, verb+" "+strings.Join(s, ", "))
	}
	list("publicar", p.Publish)
	list("probar", p.Test)
	if len(p.Compare) > 0 {
		var s []string
		for _, c := range p.Compare {
			s = append(s, c.Package+" "+c.Version)
		}
		parts = append(parts, "comparar con lo publicado "+strings.Join(s, ", "))
	}
	if len(p.Skipped) > 0 {
		var s []string
		for _, k := range p.Skipped {
			s = append(s, k.Package)
		}
		parts = append(parts, "sin cambios: "+strings.Join(s, " "))
	}
	if len(p.Errors) > 0 {
		var s []string
		for _, e := range p.Errors {
			s = append(s, e.Package)
		}
		parts = append(parts, "con errores: "+strings.Join(s, " "))
	}
	if len(parts) == 0 {
		return "nada que hacer"
	}
	return strings.Join(parts, "; ")
}

// upstream quita el sufijo -0~tepache<N> de una versión Debian.
func upstream(v string) string {
	if i := strings.LastIndex(v, "-0~tepache"); i >= 0 {
		return v[:i]
	}
	return v
}

// manifestEntry valida el manifiesto de name en root y comprueba que su
// lockfile tenga la versión upstream.
func manifestEntry(root string, cfg *config.Config, name string) (string, *PlanError) {
	m, err := manifest.Validate(root, cfg, name)
	if err != nil {
		return "", &PlanError{Package: name, Stage: report.Validate, Reason: firstLine(err.Error())}
	}
	lk, err := lock.Read(root, name)
	if err != nil {
		return "", &PlanError{Package: name, Version: m.DebVersion(), Stage: report.Lock, Reason: err.Error()}
	}
	if _, ok := lk.Versions[m.Version]; !ok {
		return "", &PlanError{Package: name, Version: m.DebVersion(), Stage: report.Lock,
			Reason: "el lockfile no tiene " + m.Version + "; regenera el lockfile con `debfactory lock " + name + "`"}
	}
	return m.DebVersion(), nil
}

// Classify aplica la tabla de conciliación de data-model.md §2 a una versión
// del manifiesto frente al estado de referencia. Devuelve reason (si hay que
// publicar), skip (si ya está) o un error.
func Classify(ref *repo.State, name, version string) (reason, skip string, perr *PlanError) {
	if _, ok := ref.FindRetired(name, version); ok {
		return "", "", &PlanError{Package: name, Version: version, Stage: report.State,
			Reason: "versión retirada; sube revision"}
	}
	if _, ok := ref.Find(name, version); ok {
		return "", "publicado", nil
	}
	pubs := ref.Packages[name]
	if len(pubs) == 0 {
		return NewPackage, "", nil
	}
	highest := pubs[0].Version // state.yaml va de mayor a menor
	newer, err := installtest.CompareVersions(version, "gt", highest)
	if err != nil {
		return "", "", &PlanError{Package: name, Version: version, Stage: report.State, Reason: err.Error()}
	}
	if !newer {
		return "", "", &PlanError{Package: name, Version: version, Stage: report.State,
			Reason: "versión anterior a la publicada (" + highest + ")"}
	}
	if upstream(version) == upstream(highest) {
		return NewRevision, "", nil
	}
	return NewVersion, "", nil
}

// PlanMain concilia los manifiestos de root (HEAD) con el estado de
// referencia (modo main, research D6). Los paquetes son independientes: un
// error en uno no quita a los demás (FR-004).
func PlanMain(root string, cfg *config.Config, ref *repo.State, refInfo Reference, head string) (*Plan, error) {
	p := &Plan{Mode: "main", Reference: refInfo}
	names, err := build.ListPackages(root)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		version, perr := manifestEntry(root, cfg, name)
		if perr != nil {
			p.Errors = append(p.Errors, *perr)
			continue
		}
		reason, skip, perr := Classify(ref, name, version)
		switch {
		case perr != nil:
			p.Errors = append(p.Errors, *perr)
		case skip != "":
			p.Skipped = append(p.Skipped, Skip{Package: name, Reason: skip})
		default:
			p.Publish = append(p.Publish, AffectedPackage{Package: name, Version: version, Commit: head, Reason: reason})
		}
	}
	return p, nil
}

// PlanPR calcula el plan de un pull request (modo pr): los paquetes con
// manifiesto o lockfile cambiados respecto de base se prueban; si cambia
// algún ci.code_paths, las versiones del manifiesto ya publicadas en el
// estado de la base se reconstruyen y comparan (FR-010).
func PlanPR(ctx context.Context, root string, cfg *config.Config, base string, baseState *repo.State) (*Plan, error) {
	p := &Plan{Mode: "pr", Reference: Reference{Source: "base"}}
	changed, err := changedFiles(ctx, root, base+"...HEAD")
	if err != nil {
		return nil, err
	}
	pkgs := map[string]bool{}
	for _, f := range changed {
		if name, ok := packageOf(f); ok {
			pkgs[name] = true
		}
		if touchesCode(cfg, f) {
			p.CodeDiff = append(p.CodeDiff, f)
		}
	}
	names, err := build.ListPackages(root)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if !pkgs[name] {
			continue
		}
		version, perr := manifestEntry(root, cfg, name)
		if perr != nil {
			p.Errors = append(p.Errors, *perr)
			continue
		}
		if pub, ok := baseState.Find(name, version); ok {
			// El PR cambia el manifiesto o el lockfile de una versión ya
			// publicada sin subir revision: el .deb no puede cambiar.
			_ = pub
			p.Compare = append(p.Compare, PkgVersion{Package: name, Version: version})
			continue
		}
		reason, _, perr := Classify(baseState, name, version)
		if perr != nil {
			p.Errors = append(p.Errors, *perr)
			continue
		}
		p.Test = append(p.Test, AffectedPackage{Package: name, Version: version, Commit: "HEAD", Reason: reason})
	}
	if len(p.CodeDiff) > 0 {
		for _, name := range names {
			if pkgs[name] {
				continue
			}
			version, perr := manifestEntry(root, cfg, name)
			if perr != nil {
				p.Errors = append(p.Errors, *perr)
				continue
			}
			if _, ok := baseState.Find(name, version); ok && !slices.ContainsFunc(p.Compare, func(c PkgVersion) bool { return c.Package == name }) {
				p.Compare = append(p.Compare, PkgVersion{Package: name, Version: version})
			}
		}
	}
	slices.SortFunc(p.Compare, func(a, b PkgVersion) int { return strings.Compare(a.Package, b.Package) })
	return p, nil
}

// CodeChanges devuelve los archivos de ci.code_paths cambiados en rng
// (por ejemplo "<last_main_sha>..HEAD"), para el paso 3b de ci main.
func CodeChanges(ctx context.Context, root string, cfg *config.Config, rng string) ([]string, error) {
	changed, err := changedFiles(ctx, root, rng)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range changed {
		if touchesCode(cfg, f) {
			out = append(out, f)
		}
	}
	return out, nil
}

func changedFiles(ctx context.Context, root, rng string) ([]string, error) {
	out, err := gitOut(ctx, root, "diff", "--name-only", rng)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// packageOf reconoce packages/<p>.yaml y packages/<p>.lock.yaml.
func packageOf(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "packages/")
	if !ok || strings.Contains(rest, "/") {
		return "", false
	}
	if n, ok := strings.CutSuffix(rest, ".lock.yaml"); ok {
		return n, true
	}
	if n, ok := strings.CutSuffix(rest, ".yaml"); ok {
		return n, true
	}
	return "", false
}

func touchesCode(cfg *config.Config, path string) bool {
	for _, p := range cfg.CI.CodePaths {
		if strings.HasSuffix(p, "/") && strings.HasPrefix(path, p) || path == p {
			return true
		}
	}
	return false
}

// gitOut ejecuta git en root y devuelve la salida (sin espacios finales).
func gitOut(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = gitEnv()
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), firstLine(msg))
	}
	return strings.TrimSpace(out.String()), nil
}

// gitEnv es el entorno de git con los mensajes en inglés: show distingue
// "no existe" por el texto del error, que se traduce según el locale.
func gitEnv() []string {
	return append(os.Environ(), "LC_ALL=C", "LANGUAGE=")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
