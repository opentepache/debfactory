package ci

import (
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/repo"
)

// TestPlanExtendArches (specs/009, T042): quitar exclude.arches de un
// paquete publicado sin esa arquitectura amplía la versión vigente; con la
// exclusión puesta no hay nada que hacer.
func TestPlanExtendArches(t *testing.T) {
	cfg := testConfig(t)
	root := fixtureRoot(t)
	st, err := repo.ReadState(root)
	if err != nil {
		t.Fatal(err)
	}
	bob := st.Packages["bob"][0]
	delete(bob.Debs, "arm64")
	bob.Arches = []string{"amd64"}
	for _, s := range cfg.Suites {
		if !contains(bob.Suites, s.Name) {
			if _, ex := map[string]bool{"bookworm": true}[s.Name]; !ex {
				bob.Suites = append(bob.Suites, s.Name)
			}
		}
	}
	st.Packages["bob"][0] = bob
	if got := MissingArches(root, cfg, st, "bob", bob.Version); strings.Join(got, ",") != "arm64" {
		t.Fatalf("MissingArches = %v", got)
	}
	p, err := PlanMain(root, cfg, st, Reference{Source: "main"}, "abc")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range p.Extend {
		if e.Package == "bob" {
			found = strings.Join(e.Arches, ",") == "arm64"
		}
	}
	if !found || !strings.Contains(p.Summary(), "bob → ") {
		t.Errorf("plan = %s (%+v)", p.Summary(), p.Extend)
	}
	// Con la exclusión, nada que ampliar en arquitecturas.
	editFile(t, filepath.Join(root, "packages/bob.yaml"), "exclude:\n", "exclude:\n  arches:\n    arm64: no cabe en el runner estandar\n")
	if got := MissingArches(root, cfg, st, "bob", bob.Version); len(got) != 0 {
		t.Errorf("con exclusión: %v", got)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
