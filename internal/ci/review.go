package ci

import (
	"context"
	"fmt"
	"strings"
)

// Review escribe en la salida el comentario de aviso de un PR (specs/009,
// FR-017, FR-018): por cada paquete que cambia de versión respecto a lo
// publicado en la base, el resumen del cambio de su fuente y Scorecard. Corre
// en un job sin secretos ni escritura (models: read); otro job, sin IA, lo
// publica. Nunca falla por la IA o Scorecard.
func (r *Runner) Review(ctx context.Context, o Options) error {
	if o.Base == "" {
		return &InputError{"ci review necesita --base REF"}
	}
	st, err := stateAt(ctx, r.Root, o.Base)
	if err != nil {
		return err
	}
	p, err := PlanPR(ctx, r.Root, r.Cfg, o.Base, st)
	if err != nil {
		return err
	}
	var parts []string
	for _, ap := range append(append([]AffectedPackage{}, p.Publish...), p.Test...) {
		pubs := st.Packages[ap.Package]
		if len(pubs) == 0 {
			parts = append(parts, fmt.Sprintf("### %s\n\nPaquete nuevo: revisa el manifiesto completo.\n", ap.Package))
			continue
		}
		from, to := upstream(pubs[0].Version), upstream(ap.Version)
		if from == to {
			continue
		}
		b := r.Build()
		b.Root = r.Root
		md, err := b.SupplyReview(ctx, ap.Package, from, to, r.Env.Token)
		if err != nil {
			parts = append(parts, fmt.Sprintf("### %s %s → %s\n\nResumen no disponible: %v\n", ap.Package, from, to, err))
			continue
		}
		parts = append(parts, md)
	}
	if len(parts) == 0 {
		return nil
	}
	_, err = fmt.Fprintln(r.Out.Out, strings.Join(parts, "\n---\n\n"))
	return err
}
