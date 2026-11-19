package watch

import (
	"regexp"
	"strings"
)

// Motivos por los que un release no es una versión estable de este paquete
// (research D5). Solo se muestran en el resumen con --dry-run.
const (
	ReasonDraft      = "borrador"
	ReasonFlag       = "marcado como pre-release por upstream"
	ReasonOtherTag   = "etiqueta de otro producto (no encaja con la plantilla tag)"
	ReasonPreliminar = "versión preliminar"
	ReasonInvalid    = "versión que no cabe en un manifiesto"
)

// preliminary son los marcadores de versión preliminar (constitución,
// Alcance: alpha, beta, rc, preview, nightly, tip, canary, dev…) como
// componente de la versión: separados por -, ., _, +, ~ o pegados a un
// número (1.2.0rc1), nunca dentro de una palabra más larga.
var preliminary = regexp.MustCompile(`(?i)(?:^|[^a-z])(alpha|beta|rc|pre|prerelease|preview|nightly|tip|canary|dev|snapshot)(?:[^a-z]|$)`)

// manifestVersion es el patrón de version en schema/manifest.v1.schema.json.
var manifestVersion = regexp.MustCompile(`^[0-9][A-Za-z0-9.+]*$`)

// Preliminary devuelve el marcador preliminar de una versión, o "".
func Preliminary(version string) string {
	if m := preliminary.FindStringSubmatch(version); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// Stable decide si un release es una versión estable del paquete (research
// D5, FR-003, FR-003a) y devuelve su versión. Si no lo es, reason dice por
// qué. ignoreFlag (watch.prerelease_flag: ignore) solo anula la marca
// prerelease de upstream: el filtro de marcadores en la versión se aplica
// siempre.
func Stable(r Release, tagTmpl string, ignoreFlag bool) (version, reason string) {
	if r.Draft {
		return "", ReasonDraft
	}
	if r.Prerelease && !ignoreFlag {
		return "", ReasonFlag
	}
	v, ok := VersionFromTag(tagTmpl, r.Tag)
	if !ok {
		return "", ReasonOtherTag
	}
	if m := Preliminary(v); m != "" {
		return "", ReasonPreliminar + " (" + m + ")"
	}
	if !manifestVersion.MatchString(v) {
		return "", ReasonInvalid
	}
	return v, ""
}
