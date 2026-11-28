package supply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/testutil"
)

func writeTarGz(t *testing.T, top string, files map[string]string) string {
	t.Helper()
	entries := []testutil.Entry{{Name: top + "/", Dir: true}}
	for n, b := range files {
		entries = append(entries, testutil.Entry{Name: top + "/" + n, Body: []byte(b)})
	}
	p := filepath.Join(t.TempDir(), top+".tar.gz")
	os.WriteFile(p, testutil.TarGz(entries), 0o644)
	return p
}

// TestCompareTag (FR-014): archivo extra declarado, sin declarar, distinto,
// faltante (no falla) y sha cambiado respecto del lockfile.
func TestCompareTag(t *testing.T) {
	tag := writeTarGz(t, "ghostty-1.4.0", map[string]string{"src/main.zig": "a", "build.zig": "b", ".gitattributes": "x"})
	rel := writeTarGz(t, "ghostty-1.4.0", map[string]string{"src/main.zig": "a", "build.zig": "b", "VERSION": "1.4.0"})
	d, err := CompareTag(rel, "tar.gz", tag)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Extra) != 1 || d.Extra["VERSION"] == "" || len(d.Missing) != 1 || d.Missing[0] != ".gitattributes" {
		t.Fatalf("diff = %+v", d)
	}
	sums, err := d.CheckExtras([]string{"VERSION"}, nil)
	if err != nil || sums["VERSION"] == "" {
		t.Fatalf("declarado: %v %v", sums, err)
	}
	if _, err := d.CheckExtras(nil, nil); err == nil || !strings.Contains(err.Error(), "VERSION") {
		t.Errorf("sin declarar: %v", err)
	}
	if _, err := d.CheckExtras([]string{"VERSION"}, map[string]string{"VERSION": strings.Repeat("0", 64)}); err == nil ||
		!strings.Contains(err.Error(), "cambió") {
		t.Errorf("sha cambiado: %v", err)
	}
	// Un archivo del tag modificado en el release (caso xz).
	evil := writeTarGz(t, "ghostty-1.4.0", map[string]string{"src/main.zig": "a", "build.zig": "puerta trasera"})
	d, _ = CompareTag(evil, "tar.gz", tag)
	var te *TagDiffError
	if _, err := d.CheckExtras(nil, nil); !errors.As(err, &te) || te.Undeclared[0] != "build.zig" {
		t.Errorf("modificado: %v", err)
	}
}
