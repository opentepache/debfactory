package installtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	Image   string  `json:"image,omitempty"` // id de la imagen de prueba (caché de TestPackage)
}

// Limiter limita cuántas suites se prueban a la vez entre todos los paquetes.
type Limiter chan struct{}

// NewLimiter permite n pruebas simultáneas (mínimo 1).
func NewLimiter(n int) Limiter { return make(Limiter, max(n, 1)) }

// Report es dist/<name>_<version>_<arch>.test.json (data-model.md).
type Report struct {
	Package        string   `json:"package"`
	Version        string   `json:"version"`
	Arch           string   `json:"arch"`
	DebSHA256      string   `json:"deb_sha256"`          // .deb que se probó (fase 002, research D8)
	CheckKey       string   `json:"check_key,omitempty"` // hash de la orden y de lo esperado (caché de TestPackage)
	Versions       string   `json:"versions"`            // pass | fail (FR-024)
	VersionsReason string   `json:"versions_reason,omitempty"`
	Results        []Result `json:"results"`
	Overall        string   `json:"overall"` // pass si versions es pass y ningún resultado es fail
}

// Run prueba d en cada suite de suites, salvo las excluidas, que se informan
// con su motivo. Las suites corren en paralelo hasta lo que permita lim (nil
// = una a la vez). Primero comprueba el orden de versiones: si falla, no
// arranca contenedores.
func Run(ctx context.Context, d Deb, upstream string, revision int, suites []config.Suite, excluded map[string]string,
	lim Limiter) *Report {
	rep := &Report{Package: d.Package, Version: fmt.Sprintf("%s-0~tepache%d", upstream, revision), Arch: d.Arch,
		Versions: "pass", Results: make([]Result, len(suites))}
	if err := CheckVersions(upstream, revision); err != nil {
		rep.Versions, rep.VersionsReason, rep.Overall = "fail", err.Error(), "fail"
		rep.Results = []Result{}
		return rep
	}
	if lim == nil {
		lim = NewLimiter(1)
	}
	var wg sync.WaitGroup
	for i, s := range suites {
		if reason, ok := excluded[s.Name]; ok {
			rep.Results[i] = Result{Suite: s.Name, Status: "excluded", Reason: reason}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case lim <- struct{}{}:
				defer func() { <-lim }()
				rep.Results[i] = RunSuite(ctx, s, d)
			case <-ctx.Done():
				rep.Results[i] = Result{Suite: s.Name, Status: "fail", Stage: StageInstall, Reason: ctx.Err().Error()}
			}
		}()
	}
	wg.Wait()
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

// checkKey identifica la comprobación de d: si cambia la orden o la subcadena
// esperada, un informe anterior no sirve aunque el .deb sea el mismo.
func checkKey(d Deb) string {
	h := sha256.Sum256([]byte(d.TestCommand + "\x00" + d.Expect))
	return hex.EncodeToString(h[:8])
}
