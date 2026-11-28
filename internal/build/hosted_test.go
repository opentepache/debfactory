package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHostedLevel2 (specs/009, T025/T026): level2 source deja el fuente de
// cada suite, source.json y supply.json; level2 build compila una variante
// desde ese fuente y deja el .deb y build.json; un .orig alterado falla antes
// de compilar.
func TestHostedLevel2(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	f := h.loadLevel2()
	out := t.TempDir()
	if err := h.env.Level2Source(ctx, "tepache-hello", out); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	var src SourceResult
	b, err := os.ReadFile(filepath.Join(out, "source.json"))
	if err != nil || json.Unmarshal(b, &src) != nil || len(src.Suites) != len(f.sources) || src.OrigSHA256 == "" {
		t.Fatalf("source.json = %s (%v)", b, err)
	}
	if _, err := os.Stat(filepath.Join(out, "supply.json")); err != nil {
		t.Error(err)
	}
	if d := h.distFiles(); len(d) != 0 {
		t.Errorf("level2 source no escribe en dist/: %v", d)
	}

	bout := t.TempDir()
	if err := h.env.Level2BuildOne(ctx, "tepache-hello", "trixie", "arm64", 1, filepath.Join(out, "trixie"), bout, Options{Disk: true}); err != nil {
		t.Fatal(err)
	}
	var hb HostedBuild
	b, _ = os.ReadFile(filepath.Join(bout, "build.json"))
	if json.Unmarshal(b, &hb) != nil || hb.Variant != 1 || hb.Arch != "arm64" || hb.Suite != "trixie" ||
		hb.SHA256 == "" || hb.DSCSHA256 != src.Suites["trixie"].DSCSHA256 {
		t.Fatalf("build.json = %s", b)
	}
	if _, err := os.Stat(filepath.Join(bout, hb.Deb)); err != nil {
		t.Error(err)
	}
	if last := f.builds[len(f.builds)-1]; last != "trixie/arm64/1" {
		t.Errorf("compilación = %s", last)
	}

	// .orig alterado entre el job de fuente y el de compilación.
	orig := filepath.Join(out, "trixie", "tepache-hello_1.0.0.orig.tar.gz")
	os.WriteFile(orig, []byte("otra cosa"), 0o644)
	n := len(f.builds)
	err = h.env.Level2BuildOne(ctx, "tepache-hello", "trixie", "amd64", 0, filepath.Join(out, "trixie"), t.TempDir(), Options{})
	if err == nil || !strings.Contains(err.Error(), ": verify:") || len(f.builds) != n {
		t.Fatalf("orig alterado: %v (compilaciones %d → %d)", err, n, len(f.builds))
	}
	// Suite excluida por el manifiesto.
	if err := h.env.Level2BuildOne(ctx, "tepache-hello", "forky", "amd64", 0, out, t.TempDir(), Options{}); err == nil {
		t.Error("compiló una suite excluida")
	}
}

// TestHostedLevel1Out (T025): build --out deja el .deb, build.json y
// supply.json en esa carpeta y no toca dist/.
func TestHostedLevel1Out(t *testing.T) {
	h := newHarness(t)
	h.loadOK()
	h.env.Dist = t.TempDir()
	if err := h.env.Build(context.Background(), "example", Options{Arch: "arm64"}); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(h.env.Dist)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "example_1.0.0-0~tepache1_arm64.build.json example_1.0.0-0~tepache1_arm64.deb example_1.0.0-0~tepache1_arm64.supply.json" {
		t.Errorf("--out = %v", names)
	}
	if d := h.distFiles(); len(d) != 0 {
		t.Errorf("dist/ = %v", d)
	}
}
