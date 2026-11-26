package site

import (
	"os"
	"testing"
)

// TestPreview genera el sitio de testdata/project en $DEBFACTORY_SITE_OUT
// para revisarlo en local sin llave ni repositorio publicado (`just site`,
// contracts/cli.md § just). Sin la variable, no hace nada.
func TestPreview(t *testing.T) {
	out := os.Getenv("DEBFACTORY_SITE_OUT")
	if out == "" {
		t.Skip("DEBFACTORY_SITE_OUT no está definida")
	}
	m := newFixture(t).load(t)
	if err := Verify(m, genDir(t, m)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(out); err != nil {
		t.Fatal(err)
	}
	if err := Build(m, out); err != nil {
		t.Fatal(err)
	}
	t.Logf("sitio en %s", out)
}
