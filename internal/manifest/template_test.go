package manifest

import "testing"

func TestRender(t *testing.T) {
	vars := Vars{"version": "1.39.0", "tag": "v1.39.0", "arch": "linux"}
	cases := []struct{ in, want, err string }{
		{"https://x/{{tag}}/fnm-{{arch}}.zip", "https://x/v1.39.0/fnm-linux.zip", ""},
		{"v{{version}}", "v1.39.0", ""},
		{"sin plantilla", "sin plantilla", ""},
		{"{{version}}{{version}}", "1.39.01.39.0", ""},
		{"fnm-{{arc}}.zip", "", "variable desconocida {{arc}}"},
		{"{{name}}", "", "variable desconocida {{name}}"},
		{"{{ version }}", "", "variable desconocida {{ version }}"},
		{"a{{version", "", `plantilla sin cerrar en "{{version"`},
	}
	for _, c := range cases {
		got, err := Render(c.in, vars)
		if c.err != "" {
			if err == nil || err.Error() != c.err {
				t.Errorf("Render(%q) error = %v; se esperaba %q", c.in, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("Render(%q) = %q, %v; se esperaba %q", c.in, got, err, c.want)
		}
	}
}

func TestSrcVarsName(t *testing.T) {
	m := &Manifest{Name: "tool", Version: "1.0.0", Tag: "v{{version}}",
		ArchMap: map[string]string{"amd64": "x86_64"}, Artifact: Artifact{Format: "binary"}}
	v, err := m.SrcVars("amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Render("{{name}}", v); got != "tool" {
		t.Errorf("{{name}} = %q", got)
	}
	m.Artifact.Format = "zip"
	v, _ = m.SrcVars("amd64")
	if _, err := Render("{{name}}", v); err == nil {
		t.Error("{{name}} debería ser desconocida fuera de format: binary")
	}
}
