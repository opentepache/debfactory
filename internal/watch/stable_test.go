package watch

import (
	"strings"
	"testing"
)

// TestStable: research D5. El filtro de marcadores preliminares en la versión
// se aplica siempre, también con watch.prerelease_flag: ignore (FR-003a).
func TestStable(t *testing.T) {
	cases := []struct {
		name       string
		r          Release
		tmpl       string
		ignoreFlag bool
		want       string // versión, o "" si se descarta
		reason     string // subcadena del motivo
	}{
		{"estable", Release{Tag: "v4.4.0"}, "v{{version}}", false, "4.4.0", ""},
		{"calendario", Release{Tag: "v2026.11.9"}, "v{{version}}", false, "2026.11.9", ""},
		{"borrador", Release{Tag: "v4.4.0", Draft: true}, "v{{version}}", true, "", ReasonDraft},
		{"marca pre-release", Release{Tag: "v4.4.0", Prerelease: true}, "v{{version}}", false, "", ReasonFlag},
		{"marca ignorada", Release{Tag: "v4.4.0", Prerelease: true}, "v{{version}}", true, "4.4.0", ""},
		{"rc sin marca", Release{Tag: "v4.4.0-rc1"}, "v{{version}}", false, "", "(rc)"},
		{"rc con marca ignorada", Release{Tag: "v4.4.0-rc1", Prerelease: true}, "v{{version}}", true, "", "(rc)"},
		{"rc pegado", Release{Tag: "v1.2.0rc1"}, "v{{version}}", true, "", "(rc)"},
		{"Beta en mayúsculas", Release{Tag: "v2.0.0-Beta.2"}, "v{{version}}", true, "", "(beta)"},
		{"nightly", Release{Tag: "v1.0.0+nightly"}, "v{{version}}", true, "", "(nightly)"},
		{"alpha", Release{Tag: "1.0.0_alpha"}, "{{version}}", false, "", "(alpha)"},
		{"preview", Release{Tag: "v3.0.0.preview1"}, "v{{version}}", false, "", "(preview)"},
		{"tip", Release{Tag: "vtip"}, "v{{version}}", false, "", "(tip)"},
		{"canary", Release{Tag: "v5.0.0-canary.3"}, "v{{version}}", false, "", "(canary)"},
		{"dev", Release{Tag: "v1.1.0.dev2"}, "v{{version}}", false, "", "(dev)"},
		{"pre", Release{Tag: "v1.0.0-pre"}, "v{{version}}", false, "", "(pre)"},
		{"snapshot", Release{Tag: "v1.0.0-SNAPSHOT"}, "v{{version}}", false, "", "(snapshot)"},
		{"plantilla propia", Release{Tag: "release-1.3.0"}, "release-{{version}}", false, "1.3.0", ""},
		{"plantilla propia rc", Release{Tag: "release-1.3.0-rc1"}, "release-{{version}}", false, "", "(rc)"},
		{"otra etiqueta", Release{Tag: "sdk-v1.0.0"}, "v{{version}}", false, "", ReasonOtherTag},
		{"no empieza por dígito", Release{Tag: "vlatest"}, "v{{version}}", false, "", ReasonInvalid},
		{"guion sin marcador", Release{Tag: "v1.0.0-hotfix"}, "v{{version}}", false, "", ReasonInvalid},
		// Palabras que contienen un marcador sin serlo.
		{"devel no es dev", Release{Tag: "v1.0.0+devel"}, "v{{version}}", false, "1.0.0+devel", ""},
		{"precise no es pre", Release{Tag: "v1.0.0+precise"}, "v{{version}}", false, "1.0.0+precise", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := Stable(c.r, c.tmpl, c.ignoreFlag)
			if got != c.want {
				t.Fatalf("versión = %q (%s); se esperaba %q", got, reason, c.want)
			}
			if c.reason == "" && reason != "" || !strings.Contains(reason, c.reason) {
				t.Fatalf("motivo = %q; se esperaba %q", reason, c.reason)
			}
		})
	}
}
