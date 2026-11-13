package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/lock"
)

func repoConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// parseFixture lee un fixture como si fuera packages/<archivo>.
func parseFixture(t *testing.T, path string) (*Manifest, string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	if first, _, ok := strings.Cut(string(data), "\n"); ok && strings.HasPrefix(first, "# want: ") {
		want = strings.TrimPrefix(first, "# want: ")
	}
	m, err := Parse(data, filepath.Join("packages", filepath.Base(path)))
	if err != nil {
		t.Fatalf("%s no pasa el schema: %v", path, err)
	}
	return m, want
}

// TestCheckInvalidFixtures: un fixture por regla en testdata/manifests/invalid/.
func TestCheckInvalidFixtures(t *testing.T) {
	cfg := repoConfig(t)
	files, _ := filepath.Glob("../../testdata/manifests/invalid/*.yaml")
	if len(files) < 10 {
		t.Fatalf("faltan fixtures: %v", files)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			m, want := parseFixture(t, f)
			if want == "" {
				t.Fatal("el fixture no tiene línea '# want: '")
			}
			err := Check(m, cfg, nil)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v\nse esperaba que contuviera %q", err, want)
			}
			if strings.Count(err.Error(), "debfactory: ") != 1 {
				t.Errorf("se esperaba un solo error:\n%v", err)
			}
		})
	}
}

func TestCheckValidFixtures(t *testing.T) {
	cfg := repoConfig(t)
	files, _ := filepath.Glob("../../testdata/manifests/valid/*.yaml")
	if len(files) == 0 {
		t.Fatal("faltan fixtures válidos")
	}
	for _, f := range files {
		m, _ := parseFixture(t, f)
		if err := Check(m, cfg, nil); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestCheckRevision(t *testing.T) {
	cfg := repoConfig(t)
	m, _ := parseFixture(t, "../../testdata/manifests/valid/tool.yaml")
	m.Revision = 2
	h := strings.Repeat("a", 64)
	entry := &lock.Entry{SourceDateEpoch: 1, LicenseSHA256: h, Artifacts: map[string]string{"amd64": h, "arm64": h}}

	empty := &lock.File{Schema: 1, Versions: map[string]*lock.Entry{}}
	if err := Check(m, cfg, empty); err != nil {
		t.Errorf("sin versiones previas no aplica la regla: %v", err)
	}
	same := &lock.File{Schema: 1, Versions: map[string]*lock.Entry{"1.0.0": entry}}
	if err := Check(m, cfg, same); err != nil {
		t.Errorf("misma versión con revision 2 es una corrección válida: %v", err)
	}
	other := &lock.File{Schema: 1, Versions: map[string]*lock.Entry{"0.9.0": entry}}
	err := Check(m, cfg, other)
	if err == nil || !strings.Contains(err.Error(), "revision: debe ser 1 para una versión upstream nueva") {
		t.Errorf("error = %v", err)
	}
}
