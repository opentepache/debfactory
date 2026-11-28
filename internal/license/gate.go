package license

import (
	"fmt"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/report"
)

// MinCoverage es la cobertura mínima para dar un texto por identificado.
const MinCoverage = 90.0

// versioned son los identificadores que licensecheck devuelve sin -only ni
// -or-later: el texto no distingue las dos variantes y decide el manifiesto.
var versioned = map[string]bool{
	"GPL-2.0": true, "GPL-3.0": true, "LGPL-2.0": true, "LGPL-2.1": true,
	"LGPL-3.0": true, "AGPL-3.0": true,
}

// Gate aplica el gate de licencias sobre un texto ya verificado (constitución
// IV), en este orden, e informa el primer fallo:
//  1. el texto se identifica con cobertura ≥ MinCoverage;
//  2. lo detectado es compatible con la expresión declarada;
//  3. la expresión declarada cumple la allowlist (OR: una; AND: todas).
func Gate(text []byte, declared string, cfg *config.Config) error {
	d := Detect(text)
	if len(d.IDs) == 0 || d.Coverage < MinCoverage {
		return report.Newf(report.License, "texto de licencia no identificable (cobertura %.0f %% < %.0f %%; declarada %q)",
			d.Coverage, MinCoverage, declared)
	}
	expr, err := Parse(declared)
	if err != nil {
		return report.Newf(report.License, "licencia declarada %q no es SPDX: %v", declared, err)
	}
	terms := Terms(expr)
	for _, id := range d.IDs {
		if !compatible(id, terms) {
			return report.Newf(report.License, "licencia detectada %q no coincide con la declarada %q", id, declared)
		}
	}
	if ok, bad := Allowed(expr, cfg.LicenseAllowed); !ok {
		return report.Newf(report.License, "licencia declarada %q no está en %s (detectada %q)",
			bad, config.LicensesFile, strings.Join(d.IDs, ", "))
	}
	return nil
}

// compatible indica si el identificador detectado corresponde a algún término
// declarado. GPL-3.0 detectado vale para GPL-3.0-only y GPL-3.0-or-later (y lo
// mismo para las demás licencias GNU versionadas).
func compatible(id string, terms []Term) bool {
	for _, t := range terms {
		if t.ID == id {
			return true
		}
		if versioned[id] && (t.ID == id+"-only" || t.ID == id+"-or-later") {
			return true
		}
	}
	return false
}

// String para depuración de la detección.
func (d Detection) String() string {
	return fmt.Sprintf("%v (%.1f %%)", d.IDs, d.Coverage)
}

// Matches indica si el identificador detectado corresponde a alguno de ids,
// con la misma equivalencia de las licencias GNU versionadas que el gate.
func Matches(detected string, ids []string) bool {
	terms := make([]Term, len(ids))
	for i, id := range ids {
		terms[i] = Term{ID: id}
	}
	return compatible(detected, terms)
}
