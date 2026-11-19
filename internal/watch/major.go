package watch

import (
	"regexp"
	"strings"
)

var leading = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]+))?`)

// IsMajor indica si pasar de from a to es un cambio de versión mayor
// (FR-015a): cambia el primer componente numérico; si ese componente es 0,
// también cuenta el segundo (en las 0.x el segundo marca las rupturas). Una
// versión que no empieza por un componente numérico cuenta siempre como mayor.
func IsMajor(from, to string) bool {
	f, t := leading.FindStringSubmatch(from), leading.FindStringSubmatch(to)
	if f == nil || t == nil {
		return true
	}
	if num(f[1]) != num(t[1]) {
		return true
	}
	return num(f[1]) == "0" && num(f[2]) != num(t[2])
}

// num quita los ceros a la izquierda ("007" y "7" son el mismo componente).
func num(s string) string {
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0"
	}
	return s
}
