package license

import (
	"os"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/testutil"
)

func TestGateRealTexts(t *testing.T) {
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ file, declared, err string }{
		{"fnm-v1.39.0-LICENSE", "GPL-3.0-only", ""},
		{"fnm-v1.39.0-LICENSE", "GPL-3.0-or-later", ""},
		{"bob-v4.3.0-LICENSE", "MIT", ""},
		{"buf-v1.75.0-LICENSE", "Apache-2.0", ""},
		{"aqua-v2.66.1-LICENSE", "MIT", ""},
		{"aqua-v2.66.1-LICENSE", "MIT OR Apache-2.0", ""},
		{"fnm-v1.39.0-LICENSE", "GPL-2.0-only", `license: licencia detectada "GPL-3.0" no coincide con la declarada "GPL-2.0-only"`},
		{"bob-v4.3.0-LICENSE", "Apache-2.0", `license: licencia detectada "MIT" no coincide con la declarada "Apache-2.0"`},
		{"fnm-v1.39.0-LICENSE", "GPL-3.0", `license: licencia declarada "GPL-3.0" no está en config/licenses.yaml (detectada "GPL-3.0")`},
	}
	for _, c := range cases {
		text, err := os.ReadFile("../../testdata/licenses/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		err = Gate(text, c.declared, cfg)
		switch {
		case c.err == "" && err != nil:
			t.Errorf("%s como %s: %v", c.file, c.declared, err)
		case c.err != "" && (err == nil || !strings.HasSuffix(err.Error(), c.err)):
			t.Errorf("%s como %s: error = %v; se esperaba %q", c.file, c.declared, err, c.err)
		}
	}
	err = Gate([]byte("Este texto lo escribí yo y no es ninguna licencia conocida."), "MIT", cfg)
	if err == nil || !strings.Contains(err.Error(), `license: texto de licencia no identificable (cobertura 0 % < 90 %; declarada "MIT")`) {
		t.Errorf("error = %v", err)
	}
}
