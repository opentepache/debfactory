//go:build e2e

package supply

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"debfactory/internal/fetch"
)

// TestProvenanceAquaReal (research D11, T001): la atestación SLSA real de
// aqua_linux_amd64.tar.gz v2.66.1 verifica con su repositorio y tag, y falla
// con otro repositorio o con otro tag. Usa la red (API de GitHub, Rekor).
func TestProvenanceAquaReal(t *testing.T) {
	ctx := context.Background()
	f := fetch.New(filepath.Join(os.TempDir(), "debfactory-e2e-cache"), 5*time.Minute, nil)
	file, _, err := f.Get(ctx, "https://github.com/aquaproj/aqua/releases/download/v2.66.1/aqua_linux_amd64.tar.gz", aquaAMD64)
	if err != nil {
		t.Fatal(err)
	}
	gh := &fetch.GitHub{Token: os.Getenv("GITHUB_TOKEN")}
	a := &Attestor{List: gh.Attestations}
	if p, err := a.Check(ctx, "aquaproj/aqua", "v2.66.1", file, aquaAMD64); err != nil || !p.OK {
		t.Fatalf("correcta: %+v, %v", p, err)
	}
	// Mismo archivo y atestación, pero exigiendo otro repositorio u otro tag.
	b, _ := gh.Attestations(ctx, "aquaproj/aqua", aquaAMD64)
	other := &Attestor{List: func(context.Context, string, string) ([]json.RawMessage, error) { return b, nil }}
	for _, c := range []struct{ repo, tag string }{{"otro/aqua", "v2.66.1"}, {"aquaproj/aqua", "v2.66.0"}} {
		var pe *ProvenanceError
		if _, err := other.Check(ctx, c.repo, c.tag, file, aquaAMD64); !errors.As(err, &pe) {
			t.Errorf("%s@%s: se esperaba ProvenanceError, %v", c.repo, c.tag, err)
		}
	}
}
