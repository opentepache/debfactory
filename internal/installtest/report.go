package installtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"debfactory/internal/config"
	"debfactory/internal/report"
)

// Result es el resultado de una suite.
type Result struct {
	Suite   string  `json:"suite"`
	Status  string  `json:"status"`          // pass | fail | excluded
	Stage   string  `json:"stage,omitempty"` // install | run | version | purge | leftovers
	Reason  string  `json:"reason,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
}

// Report es dist/<name>_<version>_<arch>.test.json (data-model.md).
type Report struct {
	Package        string   `json:"package"`
	Version        string   `json:"version"`
	Arch           string   `json:"arch"`
	Versions       string   `json:"versions"` // pass | fail (FR-024)
	VersionsReason string   `json:"versions_reason,omitempty"`
	Results        []Result `json:"results"`
	Overall        string   `json:"overall"` // pass si versions es pass y ningún resultado es fail
}

// Run prueba d en cada suite de suites, salvo las excluidas, que se informan
// con su motivo. Primero comprueba el orden de versiones: si falla, no
// arranca contenedores.
func Run(ctx context.Context, d Deb, upstream string, pav int, suites []config.Suite, excluded map[string]string) *Report {
	rep := &Report{Package: d.Package, Version: fmt.Sprintf("%s-0~pav%d", upstream, pav), Arch: d.Arch,
		Versions: "pass", Results: []Result{}}
	if err := CheckVersions(upstream, pav); err != nil {
		rep.Versions, rep.VersionsReason, rep.Overall = "fail", err.Error(), "fail"
		return rep
	}
	for _, s := range suites {
		if reason, ok := excluded[s.Name]; ok {
			rep.Results = append(rep.Results, Result{Suite: s.Name, Status: "excluded", Reason: reason})
			continue
		}
		rep.Results = append(rep.Results, RunSuite(ctx, s, d))
		if ctx.Err() != nil {
			break
		}
	}
	rep.Overall = "pass"
	for _, r := range rep.Results {
		if r.Status == "fail" {
			rep.Overall = "fail"
		}
	}
	return rep
}

// Write guarda el informe en path de forma atómica.
func (r *Report) Write(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), ".tmp-"+filepath.Base(path))
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Print muestra el informe como tabla legible (o un evento JSON por suite).
func (r *Report) Print(p *report.Printer) {
	p.Line("%s %s %s", r.Package, r.Version, r.Arch)
	p.Line("  %-10s %s %s", "versions", r.Versions, r.VersionsReason)
	for _, res := range r.Results {
		detail := res.Reason
		if res.Stage != "" {
			detail = res.Stage + ": " + res.Reason
		}
		if res.Seconds > 0 {
			detail = fmt.Sprintf("%5.1f s  %s", res.Seconds, detail)
		}
		p.Line("  %-10s %-8s %s", res.Suite, res.Status, detail)
		if p.JSON {
			p.Result(r.Package, r.Version, r.Arch, "test", res.Suite+": "+res.Status)
		}
	}
	p.Line("  %-10s %s", "overall", r.Overall)
	if p.JSON {
		p.Result(r.Package, r.Version, r.Arch, "test-overall", r.Overall)
	}
}
