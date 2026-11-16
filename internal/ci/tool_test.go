package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/version"
)

func TestCheckTool(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	old := version.Commit
	t.Cleanup(func() { version.Commit = old })
	ci := &Env{Actions: true}
	cases := []struct {
		name, file, commit string
		env                *Env
		want               string // "" = sin error
	}{
		{"igual", a + "\n", a, ci, ""},
		{"sin salto de línea", a, a, ci, ""},
		{"distinto", b + "\n", a, ci, "soy " + a + " y packages fija " + b},
		{"dev", a + "\n", "dev", ci, "soy dev y packages fija " + a},
		{"formato inválido", "v1.2.3\n", a, ci, "no es un commit"},
		{"mayúsculas", strings.ToUpper(a) + "\n", a, ci, "no es un commit"},
		{"dos líneas", a + "\n" + b + "\n", a, ci, "no es un commit"},
		{"sin archivo", "", a, ci, "falta"},
		{"fuera de CI", b + "\n", "dev", &Env{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.name != "sin archivo" {
				os.WriteFile(filepath.Join(root, ToolFile), []byte(c.file), 0o644)
			}
			version.Commit = c.commit
			err := CheckTool(root, c.env)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("error inesperado: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Errorf("error %v, se esperaba %q", err, c.want)
			}
			if err != nil {
				if _, ok := err.(*InputError); !ok {
					t.Errorf("no es InputError (código 2): %T", err)
				}
			}
		})
	}
}

func TestToolDiffHeader(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	if got := toolDiffHeader(a, b, "opentepache/debfactory"); got != "Herramienta: aaaaaaaaaaaa → bbbbbbbbbbbb\n\n"+
		"Diferencias: https://github.com/opentepache/debfactory/compare/"+a+"..."+b+"\n\n" {
		t.Errorf("cabecera = %q", got)
	}
	if got := toolDiffHeader(a, b, ""); strings.Contains(got, "Diferencias") || !strings.HasPrefix(got, "Herramienta:") {
		t.Errorf("sin tool_repository: %q", got)
	}
	for _, c := range [][2]string{{a, a}, {"", b}, {a, ""}, {"x", b}} {
		if got := toolDiffHeader(c[0], c[1], "o/r"); got != "" {
			t.Errorf("%q → %q: %q, se esperaba vacía", c[0], c[1], got)
		}
	}
}
