package installtest

import (
	"context"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/manifest"
	"debfactory/internal/testutil"
)

// TestTestDebRejects (T027): suite desconocida, suite o arquitectura
// excluidas y .deb inexistente fallan antes de arrancar contenedores.
func TestTestDebRejects(t *testing.T) {
	root := testutil.ProjectRoot()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Validate(root, cfg, "buf")
	if err != nil {
		t.Fatal(err)
	}
	m.Exclude.Suites = map[string]string{"sid": "rompe en sid"}
	m.Exclude.Arches = map[string]string{"arm64": "no cabe en el runner"}
	for _, c := range []struct{ suite, arch, deb, want string }{
		{"nada", "amd64", "x.deb", "no está en la configuración"},
		{"sid", "amd64", "x.deb", "excluye la suite"},
		{"trixie", "arm64", "x.deb", "excluye la arquitectura"},
		{"trixie", "amd64", "/no/existe.deb", "inputs"},
	} {
		_, err := TestDeb(context.Background(), cfg, m, c.suite, c.arch, c.deb, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c, err)
		}
	}
}
