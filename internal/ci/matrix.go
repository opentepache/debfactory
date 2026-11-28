package ci

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"debfactory/internal/config"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
)

// Runners estándar gratuitos de GitHub por arquitectura (constitución IX,
// specs/009). Ningún job hospedado usa runners "larger" ni de pago.
var hostedRunner = map[string]string{"amd64": "ubuntu-24.04", "arm64": "ubuntu-24.04-arm"}

// MatrixEntry es un elemento de strategy.matrix.include.
type MatrixEntry struct {
	Package string `json:"package"`
	Version string `json:"version"`
	Suite   string `json:"suite,omitempty"`
	Arch    string `json:"arch,omitempty"`
	Variant *int   `json:"variant,omitempty"`
	Runner  string `json:"runner"`
}

// Matrix es la matriz de los jobs hospedados (specs/009, contracts/pipeline.md).
// Las listas vacías salen como [] para que el workflow omita el job.
type Matrix struct {
	Packages []string `json:"packages"`
	// Compare son los paquetes ya publicados que el PR reconstruye para
	// exigir el mismo sha256 (cambio de herramienta o de manifiesto sin subir
	// revision; ci compare).
	Compare  []string      `json:"compare"`
	L1Build  []MatrixEntry `json:"l1_build"`
	L2Source []MatrixEntry `json:"l2_source"`
	L2Build  []MatrixEntry `json:"l2_build"`
	Test     []MatrixEntry `json:"test"`
}

// BuildMatrix convierte un plan en la matriz: nivel 1 por arquitectura;
// nivel 2 por suite × arquitectura × variante (las dos compilaciones van en
// jobs distintos); pruebas por suite × arquitectura. Respeta exclude.suites y
// exclude.arches; en una ampliación solo entran las suites nuevas.
func BuildMatrix(root string, cfg *config.Config, p *Plan) (*Matrix, error) {
	mx := &Matrix{Packages: []string{}, Compare: []string{}, L1Build: []MatrixEntry{}, L2Source: []MatrixEntry{},
		L2Build: []MatrixEntry{}, Test: []MatrixEntry{}}
	// Las ampliaciones (suite o arquitectura nueva) se validan en la PC con
	// el flujo local (gatesFor): no entran en la matriz.
	affected := append(append([]AffectedPackage{}, p.Publish...), p.Test...)
	// Las comparaciones se compilan igual pero no se prueban (como en la
	// fase 003): solo cuenta que den el sha256 publicado.
	for _, c := range p.Compare {
		if !slices.ContainsFunc(affected, func(a AffectedPackage) bool { return a.Package == c.Package }) {
			affected = append(affected, AffectedPackage{Package: c.Package, Version: c.Version, Reason: "compare"})
		}
		mx.Compare = append(mx.Compare, c.Package)
	}
	for _, a := range affected {
		if slices.Contains(mx.Packages, a.Package) {
			continue
		}
		m, err := manifest.Validate(root, cfg, a.Package)
		if err != nil {
			return nil, err
		}
		mx.Packages = append(mx.Packages, a.Package)
		arches := m.Arches(cfg.Arches)
		var suites []string
		for _, s := range cfg.Suites {
			if _, excluded := m.Exclude.Suites[s.Name]; excluded {
				continue
			}
			if a.Reason == Extend && !slices.Contains(a.Suites, s.Name) {
				continue
			}
			suites = append(suites, s.Name)
		}
		for _, arch := range arches {
			runner, ok := hostedRunner[arch]
			if !ok {
				return nil, fmt.Errorf("%s: no hay runner hospedado para %s", a.Package, arch)
			}
			if m.Level2() {
				for _, s := range suites {
					for v := range 2 {
						mx.L2Build = append(mx.L2Build, MatrixEntry{Package: a.Package, Version: a.Version,
							Suite: s, Arch: arch, Variant: &v, Runner: runner})
					}
				}
			} else if a.Reason != Extend { // el .deb de nivel 1 sirve a todas las suites
				mx.L1Build = append(mx.L1Build, MatrixEntry{Package: a.Package, Version: a.Version, Arch: arch, Runner: runner})
			}
			if a.Reason == "compare" {
				continue
			}
			for _, s := range suites {
				mx.Test = append(mx.Test, MatrixEntry{Package: a.Package, Version: a.Version, Suite: s, Arch: arch, Runner: runner})
			}
		}
		if m.Level2() && len(suites) > 0 {
			mx.L2Source = append(mx.L2Source, MatrixEntry{Package: a.Package, Version: a.Version, Runner: hostedRunner["amd64"]})
		}
	}
	return mx, nil
}

// Matrix imprime la matriz de los jobs hospedados en JSON compacto (una línea
// por lista, "clave=valor", para $GITHUB_OUTPUT).
func (r *Runner) Matrix(ctx context.Context, o Options) error {
	var p *Plan
	switch o.Mode {
	case "", "main":
		st, err := repo.ReadState(r.Root)
		if err != nil {
			return err
		}
		head, _ := gitOut(ctx, r.Root, "rev-parse", "HEAD")
		if p, err = PlanMain(r.Root, r.Cfg, st, Reference{Source: "checkout"}, head); err != nil {
			return err
		}
	case "pr":
		if o.Base == "" {
			return &InputError{"ci matrix --mode pr necesita --base REF"}
		}
		st, err := stateAt(ctx, r.Root, o.Base)
		if err != nil {
			return err
		}
		if p, err = PlanPR(ctx, r.Root, r.Cfg, o.Base, st); err != nil {
			return err
		}
	default:
		return &InputError{fmt.Sprintf("ci matrix: modo %q (main o pr)", o.Mode)}
	}
	if err := p.Err().Err(); err != nil {
		return err
	}
	mx, err := BuildMatrix(r.Root, r.Cfg, p)
	if err != nil {
		return err
	}
	return writeMatrix(r.Out.Out, mx)
}

func writeMatrix(w interface{ Write([]byte) (int, error) }, mx *Matrix) error {
	for _, kv := range []struct {
		k string
		v any
	}{{"packages", mx.Packages}, {"compare", mx.Compare}, {"l1_build", mx.L1Build}, {"l2_source", mx.L2Source},
		{"l2_build", mx.L2Build}, {"test", mx.Test}} {
		b, err := json.Marshal(kv.v)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s=%s\n", kv.k, b); err != nil {
			return err
		}
	}
	return nil
}
