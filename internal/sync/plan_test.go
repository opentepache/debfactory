package sync

import (
	"reflect"
	"sort"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/repo"
	"debfactory/internal/testutil"
)

func cfg(t *testing.T) *config.Config {
	c, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func gen(files map[string]string) *repo.GenInfo { return &repo.GenInfo{Name: "gen-1", Files: files} }

var tree1 = map[string]string{
	"debfactory.gpg":                                  "k",
	"pool/main/f/fnm/fnm_1_amd64.deb":                 "a",
	"dists/trixie/InRelease":                          "i1",
	"dists/trixie/Release":                            "r1",
	"dists/trixie/Release.gpg":                        "g1",
	"dists/trixie/main/binary-amd64/Packages.gz":      "p1",
	"dists/trixie/main/binary-amd64/Release":          "c1",
	"dists/trixie/main/binary-amd64/by-hash/SHA256/x": "p1",
}

func TestPlanOrderAndHeaders(t *testing.T) {
	c := cfg(t)
	phases := Plan(c, c.Repo.Destinations["r2"], gen(tree1), nil)
	var names []string
	for _, p := range phases {
		names = append(names, p.Name)
	}
	if !reflect.DeepEqual(names, Order) {
		t.Fatalf("orden = %v", names)
	}
	want := map[string][]string{
		PhasePool:      {"debfactory.gpg", "pool/main/f/fnm/fnm_1_amd64.deb"},
		PhaseByHash:    {"dists/trixie/main/binary-amd64/by-hash/SHA256/x"},
		PhaseIndices:   {"dists/trixie/main/binary-amd64/Packages.gz", "dists/trixie/main/binary-amd64/Release"},
		PhaseRelease:   {"dists/trixie/Release", "dists/trixie/Release.gpg"},
		PhaseInRelease: {"dists/trixie/InRelease"},
	}
	for _, p := range phases[:5] {
		got := p.Files()
		sort.Strings(got)
		if !reflect.DeepEqual(got, want[p.Name]) {
			t.Errorf("%s = %v, want %v", p.Name, got, want[p.Name])
		}
	}
	for _, b := range phases[0].Batches {
		for _, f := range b.Files {
			wantCC := c.Repo.CacheControl.Mutable
			if f != "debfactory.gpg" {
				wantCC = c.Repo.CacheControl.Immutable
			}
			if b.CacheControl != wantCC {
				t.Errorf("%s: Cache-Control %q", f, b.CacheControl)
			}
		}
	}
	local := Plan(c, c.Repo.Destinations["local"], gen(tree1), nil)
	if local[0].Batches[0].CacheControl != "" {
		t.Error("el destino local no lleva Cache-Control")
	}
}

func TestPlanUnchangedAndDeferredDelete(t *testing.T) {
	c := cfg(t)
	d := c.Repo.Destinations["local"]
	rec1 := &Record{Generation: "gen-1", Files: tree1}
	if !Unchanged(Plan(c, d, gen(tree1), rec1)) {
		t.Error("sin cambios debería dar un plan vacío")
	}
	// Tras retirar el .deb: generación 2 sin él.
	tree2 := map[string]string{}
	for k, v := range tree1 {
		tree2[k] = v
	}
	delete(tree2, "pool/main/f/fnm/fnm_1_amd64.deb")
	tree2["dists/trixie/InRelease"] = "i2"
	p2 := Plan(c, d, &repo.GenInfo{Name: "gen-2", Files: tree2}, &Record{Generation: "gen-1", Files: tree1})
	if len(p2[5].Files()) != 0 {
		t.Errorf("primera sincronización tras retirar no debe borrar: %v", p2[5].Files())
	}
	// Repetir sync de la misma generación no borra nada nuevo.
	same := &repo.GenInfo{Name: "gen-2", Files: tree2}
	if got := Plan(c, d, same, &Record{Generation: "gen-2", Files: tree2, PreviousFiles: tree1})[5].Files(); len(got) != 0 {
		t.Errorf("repetir la misma generación borra %v", got)
	}
	// Siguiente generación: ahora sí se borra (estaba en N-2 y no en N-1 ni N).
	g3 := &repo.GenInfo{Name: "gen-3", Files: tree2}
	p3 := Plan(c, d, g3, &Record{Generation: "gen-2", Files: tree2, PreviousFiles: tree1})
	if got := p3[5].Files(); !reflect.DeepEqual(got, []string{"pool/main/f/fnm/fnm_1_amd64.deb"}) {
		t.Errorf("segunda sincronización: borrar %v", got)
	}
	// Un borrado que quedó pendiente se repite aunque la generación no cambie.
	pend := Plan(c, d, g3, &Record{Generation: "gen-3", Files: tree2, Pending: []string{"pool/x.deb"}})
	if got := pend[5].Files(); !reflect.DeepEqual(got, []string{"pool/x.deb"}) {
		t.Errorf("pendiente: %v", got)
	}
}

func TestClassifySymlinkNamesNeverPlanned(t *testing.T) {
	// GenInfo.Files nunca contiene enlaces (TreeFiles los salta), así que
	// los alias de by-hash con nombre de índice no se suben.
	if ph, imm := Classify("dists/trixie/main/binary-amd64/by-hash/SHA256/abc"); ph != PhaseByHash || !imm {
		t.Errorf("by-hash: %s %v", ph, imm)
	}
}
