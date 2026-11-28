package supply

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"debfactory/internal/fetch"
)

const aquaAMD64 = "1cd648c1835dcdfc60d935a169993f55551cfb948a0f5a442af5b51f2a28a8d1"

// recordedAPI sirve la respuesta grabada de aqua para su digest y 404 para
// cualquier otro (research D11).
func recordedAPI(t *testing.T) *fetch.GitHub {
	t.Helper()
	body, err := os.ReadFile("../../testdata/supply/aqua/attestations-amd64.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "debfactory" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		if r.URL.Path == "/repos/aquaproj/aqua/attestations/sha256:"+aquaAMD64 {
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return &fetch.GitHub{Client: srv.Client(), API: srv.URL}
}

// TestProvenance (FR-016): solo cuentan las atestaciones SLSA v1, cosign
// recibe repositorio y tag, y sin atestaciones no falla.
func TestProvenance(t *testing.T) {
	gh := recordedAPI(t)
	var calls [][]string
	ok := func(_ context.Context, _, bundle string, args []string) error {
		b, _ := os.ReadFile(bundle)
		if predicateType(b) != SLSAProvenanceV1 {
			t.Errorf("se verificó una atestación que no es SLSA")
		}
		calls = append(calls, args)
		return nil
	}
	a := &Attestor{List: gh.Attestations, Cosign: ok}
	p, err := a.Check(context.Background(), "aquaproj/aqua", "v2.66.1", "aqua.tar.gz", aquaAMD64)
	if err != nil || !p.Present || !p.OK || p.Count != 1 {
		t.Fatalf("aqua: %+v, %v", p, err)
	}
	args := calls[0]
	for _, want := range [][2]string{
		{"--certificate-github-workflow-repository", "aquaproj/aqua"},
		{"--certificate-github-workflow-ref", "refs/tags/v2.66.1"},
		{"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com"},
		{"--type", "slsaprovenance1"},
	} {
		i := slices.Index(args, want[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != want[1] {
			t.Errorf("falta %s %s en %v", want[0], want[1], args)
		}
	}

	// Ninguna verifica: falla (procedencia de otro repositorio u otro tag).
	a.Cosign = func(context.Context, string, string, []string) error {
		return errors.New("none of the attestations matched")
	}
	p, err = a.Check(context.Background(), "aquaproj/aqua", "v2.66.1", "aqua.tar.gz", aquaAMD64)
	var pe *ProvenanceError
	if !errors.As(err, &pe) || !p.Present || p.OK || !strings.Contains(err.Error(), "refs/tags/v2.66.1") {
		t.Fatalf("ajena: %+v, %v", p, err)
	}

	// Sin atestaciones: ausente, sin error.
	p, err = a.Check(context.Background(), "aquaproj/aqua", "v2.66.1", "otro.tar.gz", strings.Repeat("0", 64))
	if err != nil || p.Present || p.OK {
		t.Fatalf("ausente: %+v, %v", p, err)
	}
}

// TestProvenanceOnlyRelease: una atestación que no es SLSA (release/v0.2)
// cuenta como ausente.
func TestProvenanceOnlyRelease(t *testing.T) {
	var resp struct {
		Attestations []struct {
			Bundle json.RawMessage `json:"bundle"`
		} `json:"attestations"`
	}
	body, _ := os.ReadFile("../../testdata/supply/aqua/attestations-amd64.json")
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	var release []json.RawMessage
	for _, a := range resp.Attestations {
		if predicateType(a.Bundle) != SLSAProvenanceV1 {
			release = append(release, a.Bundle)
		}
	}
	if len(release) != 1 {
		t.Fatalf("se esperaba una atestación release/v0.2, hay %d", len(release))
	}
	a := &Attestor{
		List:   func(context.Context, string, string) ([]json.RawMessage, error) { return release, nil },
		Cosign: func(context.Context, string, string, []string) error { t.Error("no debía verificar"); return nil },
	}
	p, err := a.Check(context.Background(), "aquaproj/aqua", "v2.66.1", "aqua.tar.gz", aquaAMD64)
	if err != nil || p.Present {
		t.Fatalf("%+v, %v", p, err)
	}
}
