package license

import (
	"slices"

	"github.com/google/licensecheck"
)

// Detection es el resultado de identificar un texto de licencia.
type Detection struct {
	IDs      []string // identificadores detectados, sin repetir, en orden de aparición
	Coverage float64  // porcentaje del texto cubierto por licencias conocidas
}

// Detect identifica el texto con google/licensecheck, localmente.
func Detect(text []byte) Detection {
	cov := licensecheck.Scan(text)
	d := Detection{Coverage: cov.Percent}
	for _, m := range cov.Match {
		if m.IsURL {
			continue
		}
		if !slices.Contains(d.IDs, m.ID) {
			d.IDs = append(d.IDs, m.ID)
		}
	}
	return d
}
