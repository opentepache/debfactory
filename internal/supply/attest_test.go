package supply

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// slsaBundles devuelve la atestación SLSA grabada de aqua (sirve como bundle
// de forma válida; la verificación criptográfica la simula cosign).
func slsaBundles(t *testing.T) []json.RawMessage {
	t.Helper()
	var resp struct {
		Attestations []struct {
			Bundle json.RawMessage `json:"bundle"`
		} `json:"attestations"`
	}
	b, _ := os.ReadFile("../../testdata/supply/aqua/attestations-amd64.json")
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatal(err)
	}
	var out []json.RawMessage
	for _, a := range resp.Attestations {
		out = append(out, a.Bundle)
	}
	return out
}

// TestVerifyRun (T028): exige atestación, identidad exacta del workflow y
// ref, repositorio y commit; sin atestación o si ninguna verifica, falla.
func TestVerifyRun(t *testing.T) {
	run := Run{Repo: "opentepache/packages", Workflow: ".github/workflows/main.yml", Ref: "refs/heads/main", Commit: "abc123"}
	var args []string
	a := &Attestor{
		List: func(_ context.Context, repo, _ string) ([]json.RawMessage, error) {
			if repo != run.Repo {
				t.Errorf("repo = %s", repo)
			}
			return slsaBundles(t), nil
		},
		Cosign: func(_ context.Context, _, _ string, a []string) error { args = a; return nil },
	}
	if err := a.VerifyRun(context.Background(), run, "x.deb", "d"); err != nil {
		t.Fatal(err)
	}
	for _, want := range [][2]string{
		{"--certificate-identity", "https://github.com/opentepache/packages/.github/workflows/main.yml@refs/heads/main"},
		{"--certificate-github-workflow-sha", "abc123"},
		{"--certificate-github-workflow-repository", "opentepache/packages"},
		{"--certificate-github-workflow-ref", "refs/heads/main"},
	} {
		i := slices.Index(args, want[0])
		if i < 0 || args[i+1] != want[1] {
			t.Errorf("falta %s %s en %v", want[0], want[1], args)
		}
	}
	// Ninguna verifica (otro repositorio, workflow, ref o commit).
	a.Cosign = func(context.Context, string, string, []string) error {
		return errors.New("certificate identity mismatch")
	}
	var ae *AttestError
	if err := a.VerifyRun(context.Background(), run, "x.deb", "d"); !errors.As(err, &ae) || !strings.Contains(err.Error(), "abc123") {
		t.Fatalf("ajena: %v", err)
	}
	// Sin atestación: falla (es obligatoria para lo que se publica).
	a.List = func(context.Context, string, string) ([]json.RawMessage, error) { return nil, nil }
	if err := a.VerifyRun(context.Background(), run, "x.deb", "d"); !errors.As(err, &ae) || !strings.Contains(err.Error(), "no tiene") {
		t.Fatalf("sin atestación: %v", err)
	}
	// Run incompleto.
	if err := a.VerifyRun(context.Background(), Run{Repo: "r"}, "x.deb", "d"); !errors.As(err, &ae) {
		t.Fatalf("run incompleto: %v", err)
	}
}
