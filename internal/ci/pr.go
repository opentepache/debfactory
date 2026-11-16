package ci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"debfactory/internal/installtest"
	"debfactory/internal/report"
)

// PR es `ci pr --base REF` (FR-008 a FR-011): comprueba el código si cambió,
// aplica los gates 1–6 a los paquetes cambiados y reconstruye y compara las
// versiones publicadas afectadas por el código (FR-010). Nunca firma,
// publica, sincroniza ni escribe en la forja (FR-009): no abre <data> ni
// necesita token.
func (r *Runner) PR(ctx context.Context, o Options) error {
	if r.Env.Trust == "pub" {
		return &report.EnvError{Cause: "ci pr no corre en el runner confiable (nivel pub): un PR nunca llega a los secretos"}
	}
	if o.Base == "" {
		return &InputError{"ci pr necesita --base REF"}
	}
	baseState, err := stateAt(ctx, r.Root, o.Base)
	if err != nil {
		return err
	}
	p, err := PlanPR(ctx, r.Root, r.Cfg, o.Base, baseState)
	if err != nil {
		return err
	}
	r.Out.Event("plan", map[string]any{"plan": p}, "plan: "+p.Summary())
	var errs report.Errors
	fail := func(e *report.Error) {
		r.Out.Fail(e)
		errs = append(errs, e)
	}
	for _, e := range p.Err() {
		fail(e)
	}
	if len(p.CodeDiff) > 0 {
		r.Out.Line("cambian %s: se validan todos los manifiestos y se compara lo publicado", strings.Join(p.CodeDiff, " "))
		if err := r.validateAll(r.Root); err != nil {
			var es report.Errors
			if errors.As(err, &es) {
				for _, e := range es {
					fail(e)
				}
			} else {
				fail(report.Wrap(err, report.Validate, "", "", ""))
			}
		}
	}
	var results []gateResult
	if len(p.Test) > 0 || len(p.Compare) > 0 {
		if err := r.preflight(ctx); err != nil {
			return err
		}
	}
	if len(p.Test) > 0 {
		results = r.runGates(ctx, r.Root, p.Test, o.Jobs)
		for _, g := range results {
			if g.err == nil {
				continue
			}
			var es report.Errors
			if errors.As(g.err, &es) {
				for _, e := range es {
					fail(report.Wrap(e, report.Test, g.ap.Package, g.ap.Version, ""))
				}
			} else {
				fail(report.Wrap(g.err, report.Test, g.ap.Package, g.ap.Version, ""))
			}
		}
	}
	for _, e := range r.compare(ctx, r.Root, baseState, p.Compare) {
		fail(e)
	}
	head := r.toolBump(ctx, o.Base)
	r.summary(head + r.prSummary(p, results, errs))
	if len(errs) == 0 {
		r.Out.Event("pr", map[string]any{"result": "pass"}, "ci pr: todo en verde; nada se publica desde un PR")
	}
	return errs.Err()
}

// toolBump devuelve la cabecera del resumen si el PR cambia la herramienta
// fijada (004 FR-009), o "".
func (r *Runner) toolBump(ctx context.Context, base string) string {
	data, err := (&StatePR{Root: r.Root}).show(ctx, base, ToolFile)
	if err != nil {
		return ""
	}
	head, _ := ReadTool(r.Root)
	text := toolDiffHeader(strings.TrimSpace(string(data)), head, r.Cfg.CI.ToolRepository)
	if text != "" {
		r.Out.Line("%s", strings.TrimSpace(strings.ReplaceAll(text, "\n\n", " · ")))
	}
	return text
}

// toolDiffHeader es la cabecera de contracts/herramienta.md § Enlace de
// diferencias: vacía si la versión no cambia o falta alguna de las dos.
func toolDiffHeader(old, new, toolRepo string) string {
	if !commitRe.MatchString(old) || !commitRe.MatchString(new) || old == new {
		return ""
	}
	s := fmt.Sprintf("Herramienta: %s → %s\n\n", old[:12], new[:12])
	if toolRepo != "" {
		s += fmt.Sprintf("Diferencias: https://github.com/%s/compare/%s...%s\n\n", toolRepo, old, new)
	}
	return s
}

// prSummary es la tabla paquete × suite × arquitectura del check (FR-008).
func (r *Runner) prSummary(p *Plan, results []gateResult, errs report.Errors) string {
	var b strings.Builder
	b.WriteString("### debfactory ci pr\n\n")
	fmt.Fprintf(&b, "Plan: %s\n\n", mdEscape(p.Summary()))
	if len(results) > 0 {
		b.WriteString("| paquete | versión | suite |")
		for _, a := range r.Cfg.Arches {
			b.WriteString(" " + a + " |")
		}
		b.WriteString("\n|---|---|---|")
		for range r.Cfg.Arches {
			b.WriteString("---|")
		}
		b.WriteString("\n")
		for _, g := range results {
			reports := map[string]*installtest.Report{}
			for _, a := range r.Cfg.Arches {
				reports[a] = readReport(filepath.Join(r.Root, "dist", fmt.Sprintf("%s_%s_%s.test.json", g.ap.Package, g.ap.Version, a)))
			}
			for _, su := range r.Cfg.Suites {
				fmt.Fprintf(&b, "| %s | %s | %s |", g.ap.Package, g.ap.Version, su.Name)
				for _, a := range r.Cfg.Arches {
					b.WriteString(" " + cell(reports[a], su.Name) + " |")
				}
				b.WriteString("\n")
			}
		}
	}
	if len(p.Compare) > 0 {
		b.WriteString("\nComparadas con lo publicado (FR-010): ")
		var s []string
		for _, c := range p.Compare {
			s = append(s, c.Package+" "+c.Version)
		}
		b.WriteString(strings.Join(s, ", ") + "\n")
	}
	if len(errs) > 0 {
		b.WriteString("\n| paquete | versión | arq. | suite | etapa | motivo |\n|---|---|---|---|---|---|\n")
		for _, e := range errs {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", e.Package, e.Version, e.Arch, e.Suite, e.Stage, mdEscape(e.Cause))
		}
	} else {
		b.WriteString("\n✅ Todo en verde. Un PR nunca firma ni publica.\n")
	}
	return b.String()
}

func readReport(path string) *installtest.Report {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rep installtest.Report
	if json.Unmarshal(data, &rep) != nil {
		return nil
	}
	return &rep
}

func cell(rep *installtest.Report, suite string) string {
	if rep == nil {
		return "—"
	}
	for _, res := range rep.Results {
		if res.Suite == suite {
			switch res.Status {
			case "pass":
				return "✅"
			case "excluded":
				return "excluida"
			default:
				return "❌ " + mdEscape(res.Stage)
			}
		}
	}
	return "—"
}
