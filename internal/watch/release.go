// Package watch es la lógica pura del detector de versiones upstream
// (specs/005-upstream-release-watch): qué release es estable, cuál es la
// candidata, si un salto es de versión mayor y si cambió la licencia. No hace
// E/S; la orquestación con GitHub y git vive en internal/ci.
package watch

import (
	"strings"
	"time"
)

// Release es lo que el detector necesita de un release upstream
// (data-model.md §3).
type Release struct {
	Tag         string
	Draft       bool
	Prerelease  bool
	PublishedAt time.Time
	URL         string   // página del release (notas)
	Assets      []string // nombres de los archivos adjuntos
	// Digests es el sha256 de cada asset según la API (campo digest), si lo
	// publica; permite detectar un asset resubido sin descargarlo.
	Digests map[string]string
}

const versionVar = "{{version}}"

// VersionFromTag invierte la plantilla tag del manifiesto (v{{version}},
// release-{{version}}, {{version}}…): devuelve la versión si la etiqueta
// encaja. Una etiqueta que no encaja es de otro producto del mismo
// repositorio y no se considera.
func VersionFromTag(tmpl, tag string) (string, bool) {
	if strings.Count(tmpl, versionVar) != 1 {
		return "", false
	}
	prefix, suffix, _ := strings.Cut(tmpl, versionVar)
	if len(tag) <= len(prefix)+len(suffix) || !strings.HasPrefix(tag, prefix) || !strings.HasSuffix(tag, suffix) {
		return "", false
	}
	return tag[len(prefix) : len(tag)-len(suffix)], true
}

// Compare compara dos versiones upstream con el algoritmo de dpkg
// (verrevcmp de lib/dpkg/version.c; constitución VI): negativo si a < b, 0 si
// son iguales y positivo si a > b. No interpreta épocas ni revisiones: las
// versiones upstream de los manifiestos no las tienen.
func Compare(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		firstDiff := 0
		for (at(a, i) != 0 && !isDigit(at(a, i))) || (at(b, j) != 0 && !isDigit(at(b, j))) {
			ac, bc := order(at(a, i)), order(at(b, j))
			if ac != bc {
				return sign(ac - bc)
			}
			i++
			j++
		}
		for at(a, i) == '0' {
			i++
		}
		for at(b, j) == '0' {
			j++
		}
		for isDigit(at(a, i)) && isDigit(at(b, j)) {
			if firstDiff == 0 {
				firstDiff = int(at(a, i)) - int(at(b, j))
			}
			i++
			j++
		}
		if isDigit(at(a, i)) {
			return 1
		}
		if isDigit(at(b, j)) {
			return -1
		}
		if firstDiff != 0 {
			return sign(firstDiff)
		}
	}
	return 0
}

func at(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// order es el peso de un carácter no numérico en dpkg: ~ antes que el fin de
// cadena, las letras antes que los demás símbolos.
func order(c byte) int {
	switch {
	case isDigit(c):
		return 0
	case isAlpha(c):
		return int(c)
	case c == '~':
		return -1
	case c != 0:
		return int(c) + 256
	}
	return 0
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
