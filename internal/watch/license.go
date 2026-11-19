package watch

import (
	"fmt"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/license"
)

// LicenseKind clasifica la licencia de la versión nueva frente a la declarada
// en el manifiesto (research D8).
type LicenseKind string

const (
	// LicenseSame: el texto nuevo es compatible con license.spdx (aunque el
	// texto cambie, p. ej. el año).
	LicenseSame LicenseKind = "igual"
	// LicenseChanged: otra licencia, única e incluida en la allowlist; el PR
	// cambia license.spdx y nunca se integra solo (FR-015).
	LicenseChanged LicenseKind = "cambiada"
	// LicenseBlocked: fuera de la allowlist, no identificable o ambigua; se
	// notifica y no se abre PR (FR-016 a).
	LicenseBlocked LicenseKind = "bloqueada"
)

// LicenseResult es la clasificación con los datos de la notificación
// (FR-017: declarada, detectada y cobertura).
type LicenseResult struct {
	Kind     LicenseKind
	Declared string  // license.spdx del manifiesto
	SPDX     string  // licencia nueva (solo con LicenseChanged)
	Detected string  // identificadores detectados, separados por comas
	Coverage float64 // porcentaje del texto identificado
	Reason   string  // por qué está bloqueada
}

// LicenseChange aplica el gate de licencias existente al texto de la versión
// nueva (constitución IV) y, si no pasa con la licencia declarada, decide si
// es un cambio permitido o hay que bloquearlo. Nunca toca la allowlist.
func LicenseChange(text []byte, declared string, cfg *config.Config) LicenseResult {
	d := license.Detect(text)
	res := LicenseResult{Declared: declared, Detected: strings.Join(d.IDs, ", "), Coverage: d.Coverage}
	if license.Gate(text, declared, cfg) == nil {
		res.Kind = LicenseSame
		return res
	}
	res.Kind = LicenseBlocked
	switch {
	case len(d.IDs) == 0 || d.Coverage < license.MinCoverage:
		res.Reason = fmt.Sprintf("texto de licencia no identificable (cobertura %.0f %% < %.0f %%)", d.Coverage, license.MinCoverage)
	case len(d.IDs) > 1:
		res.Reason = "el texto tiene varias licencias (" + res.Detected + "); hay que declarar la expresión a mano"
	case !cfg.LicenseAllowed(d.IDs[0]):
		res.Reason = fmt.Sprintf("licencia %s fuera de %s", d.IDs[0], config.LicensesFile)
		if cfg.LicenseAllowed(d.IDs[0]+"-only") || cfg.LicenseAllowed(d.IDs[0]+"-or-later") {
			res.Reason = fmt.Sprintf("licencia %s: el texto no distingue -only de -or-later; hay que elegir a mano", d.IDs[0])
		}
	case license.Gate(text, d.IDs[0], cfg) != nil:
		res.Reason = fmt.Sprintf("licencia %s detectada, pero el gate no la acepta: %v", d.IDs[0], license.Gate(text, d.IDs[0], cfg))
	default:
		res.Kind, res.SPDX, res.Reason = LicenseChanged, d.IDs[0], ""
	}
	return res
}
