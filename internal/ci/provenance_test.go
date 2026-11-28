//go:build integration

package ci

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"debfactory/internal/fetch"
)

// TestMainProvenance (T035): cada versión publicada deja provenance.json en
// <data> y su sha256 en la entrada de auditoría de la rama de estado.
func TestMainProvenance(t *testing.T) {
	f := newMainFixture(t)
	if err := f.main(); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	audit := f.g.RemoteFile("debfactory/state", AuditFile)
	entries, err := ParseAudit([]byte(audit))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("sin entradas")
	}
	for _, e := range entries {
		b, err := os.ReadFile(ProvenancePath(f.data, e.Package, e.Version))
		if err != nil {
			t.Errorf("%s %s: %v", e.Package, e.Version, err)
			continue
		}
		if e.Provenance != fetch.SHA256(b) {
			t.Errorf("%s: la auditoría dice %s y provenance.json da %s", e.Package, e.Provenance, fetch.SHA256(b))
		}
		var pv Provenance
		if err := json.Unmarshal(b, &pv); err != nil {
			t.Fatal(err)
		}
		if pv.AuditSeq != e.Seq || pv.ManifestCommit != e.Commit || len(pv.Attestations) == 0 ||
			!strings.HasSuffix(pv.RunURL, "/actions/runs/1000") || pv.VerifiedByPC.Mode != "local" || len(pv.Upstream.Files) == 0 {
			t.Errorf("%s: provenance.json = %s", e.Package, b)
		}
		for _, a := range pv.Attestations {
			if a.RekorURL != "" {
				t.Errorf("modo local con enlace a Rekor: %+v", a)
			}
		}
	}
}
