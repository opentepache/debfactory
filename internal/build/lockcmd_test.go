package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/lock"
	aptrepo "debfactory/internal/repo"
	"debfactory/internal/testutil"
)

const exampleURL = "https://github.com/example/example/releases/download/v1.0.0/example-"

// TestLock: lock descarga, verifica, pasa el gate y escribe la entrada; si los
// hashes de una versión ya registrada cambian, falla salvo con --force.
func TestLock(t *testing.T) {
	h := newHarness(t)
	h.loadOK()
	committed, _ := os.ReadFile(filepath.Join(h.root, "packages/example.lock.yaml"))
	os.Remove(filepath.Join(h.root, "packages/example.lock.yaml"))
	ctx := context.Background()

	if err := h.env.Lock(ctx, "example", false); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	lk, err := lock.Read(h.root, "example")
	if err != nil {
		t.Fatal(err)
	}
	got := lk.Versions["1.0.0"]
	if got == nil || got.SourceDateEpoch != 1767225600 || !strings.Contains(string(committed), got.Artifacts["amd64"]) ||
		!strings.Contains(string(committed), got.LicenseSHA256) {
		t.Fatalf("entrada = %+v", got)
	}
	cached, _ := os.ReadDir(filepath.Join(h.env.Fetcher.Cache, "sha256"))
	if len(cached) != 2 { // dos artefactos; la licencia sale del artefacto
		t.Errorf("caché = %d archivos", len(cached))
	}

	// Re-lock sin cambios: mismo archivo.
	before, _ := os.ReadFile(filepath.Join(h.root, "packages/example.lock.yaml"))
	if err := h.env.Lock(ctx, "example", false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(h.root, "packages/example.lock.yaml"))
	if string(before) != string(after) {
		t.Error("re-lock sin cambios modificó el lockfile")
	}

	// Upstream reemplaza el asset de amd64: falla sin --force.
	other := exampleArchives("example", "example 1.0.0 (rebuilt)\n", []byte(testutil.MITText))
	h.serve(exampleURL+"x86_64.tar.gz", other["x86_64"])
	err = h.env.Lock(ctx, "example", false)
	if err == nil || !strings.Contains(err.Error(), "debfactory: example 1.0.0 -: lock: los hashes de 1.0.0 cambiaron respecto al lockfile (usa --force si el cambio es legítimo)") ||
		!strings.Contains(err.Error(), "artifacts.amd64: ") {
		t.Fatalf("error = %v", err)
	}
	if now, _ := os.ReadFile(filepath.Join(h.root, "packages/example.lock.yaml")); string(now) != string(after) {
		t.Error("un lock fallido modificó el lockfile")
	}
	if err := h.env.Lock(ctx, "example", true); err != nil {
		t.Fatalf("--force: %v", err)
	}
	lk, _ = lock.Read(h.root, "example")
	if lk.Versions["1.0.0"].Artifacts["amd64"] == got.Artifacts["amd64"] {
		t.Error("--force no actualizó el hash")
	}
	if entries, _ := os.ReadDir(filepath.Join(h.env.Fetcher.Cache, "tmp")); len(entries) != 0 {
		t.Errorf("quedan temporales sin verificar: %d", len(entries))
	}
}

// TestPrune deja solo la versión del manifiesto.
func TestPrune(t *testing.T) {
	h := newHarness(t)
	h.loadOK()
	lk, _ := lock.Read(h.root, "example")
	old := *lk.Versions["1.0.0"]
	lk.Versions["0.9.0"] = &old
	if err := lk.Write(h.root, "example"); err != nil {
		t.Fatal(err)
	}
	// FR-038: si 0.9.0 sigue publicada, --prune falla.
	st := aptrepo.Empty()
	st.Serial = 1
	st.Packages["example"] = []aptrepo.Published{{Version: "0.9.0-0~pav1", Suites: []string{"trixie"}}}
	if err := st.Write(h.root); err != nil {
		t.Fatal(err)
	}
	err := h.env.Prune("example")
	if err == nil || !strings.Contains(err.Error(), "example 1.0.0 -: lock: 0.9.0 sigue publicada; retírala o déjala caer por retención antes de --prune") {
		t.Fatalf("prune con versión publicada: %v", err)
	}
	os.Remove(filepath.Join(h.root, aptrepo.StateFile))
	if err := h.env.Prune("example"); err != nil {
		t.Fatal(err)
	}
	lk, _ = lock.Read(h.root, "example")
	if len(lk.Versions) != 1 || lk.Versions["1.0.0"] == nil {
		t.Errorf("versiones = %v", lk.Versions)
	}
}
