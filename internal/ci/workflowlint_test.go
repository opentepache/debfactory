package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/testutil"
)

// TestWorkflowTemplates (T032): las plantillas de ci/workflows pasan ci-lint.
func TestWorkflowTemplates(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(testutil.RepoRoot(), "ci", "workflows", "*.yml"))
	if len(files) < 2 {
		t.Fatalf("faltan plantillas: %v", files)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if probs := LintWorkflow(filepath.Base(f), b); len(probs) > 0 {
			t.Errorf("%s:\n%s", f, strings.Join(probs, "\n"))
		}
	}
}

// TestWorkflowLintRejects (T032): cada workflow de testdata/workflows/bad-*
// tiene un problema; la primera línea del archivo dice cuál ("# espera: …").
func TestWorkflowLintRejects(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(testutil.RepoRoot(), "testdata", "workflows", "bad-*.yml"))
	if len(files) == 0 {
		t.Fatal("sin casos")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		first, _, _ := strings.Cut(string(b), "\n")
		want := strings.TrimSpace(strings.TrimPrefix(first, "# espera:"))
		probs := strings.Join(LintWorkflow(filepath.Base(f), b), "\n")
		if !strings.Contains(probs, want) {
			t.Errorf("%s: se esperaba %q en:\n%s", filepath.Base(f), want, probs)
		}
	}
}
