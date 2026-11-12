package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExamplesValidate: los manifiestos de ejemplo del contrato y los de
// packages/ pasan el schema y las reglas semánticas, y el ejemplo mínimo
// tiene como mucho 25 líneas (SC-003).
func TestExamplesValidate(t *testing.T) {
	cfg := repoConfig(t)
	var files []string
	for _, pattern := range []string{examples + "*.yaml", "../../packages/*.yaml"} {
		matches, _ := filepath.Glob(pattern)
		for _, f := range matches {
			if !strings.HasSuffix(f, ".lock.yaml") {
				files = append(files, f)
			}
		}
	}
	if len(files) < 5 {
		t.Fatalf("faltan ejemplos: %v", files)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m, err := Parse(data, filepath.Join("packages", filepath.Base(f)))
		if err == nil {
			err = Check(m, cfg, nil)
		}
		if err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	data, err := os.ReadFile(examples + "example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(data, []byte("\n")); n > 25 {
		t.Errorf("example.yaml tiene %d líneas (> 25)", n)
	}
}
