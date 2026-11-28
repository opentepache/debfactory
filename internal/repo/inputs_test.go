package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/testutil"
)

func suiteNames(t *testing.T) []string {
	var out []string
	for _, s := range loadCfg(t).Suites {
		out = append(out, s.Name)
	}
	return out
}

func TestInputs(t *testing.T) {
	cfg := loadCfg(t)
	const v = "1.0.0-0~tepache1"
	dist := func(root, arch, ext string) string {
		return filepath.Join(root, "dist", "foo_"+v+"_"+arch+ext)
	}
	cases := []struct {
		name  string
		edit  func(t *testing.T, root string)
		want  string
		suite []string // suites esperadas si no hay error
	}{
		{name: "todo correcto", suite: suiteNames(t)},
		{name: "falta arm64", edit: func(t *testing.T, root string) { os.Remove(dist(root, "arm64", ".deb")) },
			want: "foo 1.0.0-0~tepache1 arm64: inputs: falta dist/foo_1.0.0-0~tepache1_arm64.deb"},
		{name: "lintian", edit: func(t *testing.T, root string) {
			editJSON(t, dist(root, "amd64", ".build.json"), func(m map[string]any) { m["lintian"] = "fail" })
		}, want: "amd64: inputs: build.json no aprobado (lintian: fail)"},
		{name: "overall", edit: func(t *testing.T, root string) {
			editJSON(t, dist(root, "amd64", ".test.json"), func(m map[string]any) { m["overall"] = "fail" })
		}, want: "amd64: inputs: test.json no aprobado (overall: fail)"},
		{name: "sha build.json", edit: func(t *testing.T, root string) {
			editJSON(t, dist(root, "amd64", ".build.json"), func(m map[string]any) { m["sha256"] = strings.Repeat("0", 64) })
		}, want: "amd64: inputs: sha256 del .deb no coincide con build.json"},
		{name: "sha test.json", edit: func(t *testing.T, root string) {
			editJSON(t, dist(root, "arm64", ".test.json"), func(m map[string]any) { m["deb_sha256"] = strings.Repeat("0", 64) })
		}, want: "arm64: inputs: sha256 del .deb no coincide con test.json; vuelve a ejecutar `debfactory test foo`"},
		{name: "sin deb_sha256", edit: func(t *testing.T, root string) {
			editJSON(t, dist(root, "amd64", ".test.json"), func(m map[string]any) { delete(m, "deb_sha256") })
		}, want: "amd64: inputs: test.json sin deb_sha256"},
		{name: "test.json parcial", edit: func(t *testing.T, root string) {
			editJSON(t, dist(root, "amd64", ".test.json"), func(m map[string]any) {
				res := m["results"].([]any)
				m["results"] = res[1:2] // solo trixie, como test --suite trixie
			})
		}, want: "amd64: inputs: test.json no cubre bookworm; ejecuta `debfactory test foo` sin --suite"},
		{name: "suites distintas", edit: func(t *testing.T, root string) {
			editJSON(t, dist(root, "arm64", ".test.json"), func(m map[string]any) {
				res := m["results"].([]any)
				res[0].(map[string]any)["status"] = "excluded"
			})
		}, want: "foo 1.0.0-0~tepache1 -: inputs: suites distintas entre amd64 [" + strings.Join(suiteNames(t), " ") +
			"] y arm64 [" + strings.Join(suiteNames(t)[1:], " ") + "]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeRelease(t, root, suiteNames(t), release{name: "foo", version: v,
				debs: map[string][]byte{"amd64": []byte("deb-amd64"), "arm64": []byte("deb-arm64")}})
			if tc.edit != nil {
				tc.edit(t, root)
			}
			in, err := Inputs(root, cfg, "foo", v)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if strings.Join(in.Published.Suites, ",") != strings.Join(tc.suite, ",") || len(in.Paths) != 2 {
					t.Errorf("input = %+v", in)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v\nwant %q", err, tc.want)
			}
		})
	}
}

// TestInputsExcludedArch (specs/009, T041): con exclude.arches.arm64 en el
// manifiesto, inputs no exige los archivos de arm64 y registra arches.
func TestInputsExcludedArch(t *testing.T) {
	cfg := loadCfg(t)
	root := t.TempDir()
	const v = "4.3.0-0~tepache1"
	writeRelease(t, root, suiteNames(t), release{name: "bob", version: v,
		debs: map[string][]byte{"amd64": []byte("deb-amd64")}})
	src, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), "packages", "bob", "bob.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := strings.Replace(string(src), "exclude:\n", "exclude:\n  arches:\n    arm64: no cabe en el runner estandar\n", 1)
	os.MkdirAll(filepath.Join(root, "packages", "bob"), 0o755)
	os.WriteFile(filepath.Join(root, "packages", "bob", "bob.yaml"), []byte(m), 0o644)
	in, err := Inputs(root, cfg, "bob", v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(in.Published.Arches, ",") != "amd64" || len(in.Published.Debs) != 1 {
		t.Errorf("published = %+v", in.Published)
	}
}
