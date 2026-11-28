package build

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/supply"
)

// ProvenanceVerified es el valor del lockfile para un archivo con procedencia
// SLSA de upstream verificada.
const ProvenanceVerified = "verified"

// provFile es un archivo de upstream ya descargado y verificado por sha256.
type provFile struct{ path, sha string }

// checkProvenance verifica la procedencia de upstream de cada archivo
// (FR-016, research D11). locked son los valores del lockfile (nil en lock):
// un archivo que el lockfile da por verificado MUST seguir teniendo
// procedencia válida (no se puede degradar quitando la atestación). Devuelve
// los valores para el lockfile y el resumen para supply.json.
func (e *Env) checkProvenance(ctx context.Context, m *manifest.Manifest, files map[string]provFile,
	locked map[string]string) (map[string]string, *supply.Provenance, error) {
	fail := func(err error) error { return report.Wrap(err, report.Provenance, m.Name, m.Version, "") }
	tag, err := m.RenderedTag()
	if err != nil {
		return nil, nil, fail(err)
	}
	check := e.Provenance
	if check == nil {
		gh := e.GitHub
		if gh == nil {
			gh = &fetch.GitHub{} // API pública, sin token
		}
		a := &supply.Attestor{List: gh.Attestations}
		check = a.Check
	}
	urls := make([]string, 0, len(files))
	for u := range files {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	out := map[string]string{}
	sum := &supply.Provenance{}
	for _, u := range urls {
		f := files[u]
		p, err := check(ctx, m.Upstream, tag, f.path, f.sha)
		if err != nil {
			return nil, nil, fail(err)
		}
		if p.Present {
			sum.Present = true
			sum.Count += p.Count
		}
		if p.OK {
			out[u] = ProvenanceVerified
		} else if locked[u] == ProvenanceVerified {
			return nil, nil, fail(fmt.Errorf("%s tenía procedencia verificada en el lockfile y ya no la tiene", u))
		}
	}
	sum.OK = sum.Present && len(out) > 0
	if len(out) == 0 {
		out = nil
	}
	return out, sum, nil
}

// newSupplyReport arranca el informe de cadena de suministro (data-model §7)
// con lo ya verificado: la firma fijada (si upstream firma) y la procedencia.
func newSupplyReport(m *manifest.Manifest, arch string, prov *supply.Provenance) *supply.Report {
	r := &supply.Report{Package: m.Name, Version: m.Version, Arch: arch, Signature: supply.SignatureReport{Type: "none"}}
	if m.Signing != nil {
		r.Signature = supply.SignatureReport{Type: m.Signing.Kind(), OK: true}
	}
	if prov != nil {
		r.Provenance = *prov
	}
	return r
}

// writeSupply escribe el informe en dist/<name> (vía work, como el resto de
// dist/: solo llega cuando todo pasó).
func (e *Env) writeSupply(r *supply.Report, name, work string) error {
	if r == nil {
		return nil
	}
	tmp := filepath.Join(work, name)
	if err := r.Write(tmp); err != nil {
		return err
	}
	return e.copyDist(tmp, name)
}
