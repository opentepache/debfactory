package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/pkg"
)

// release describe una versión en dist/ con sus informes.
type release struct {
	name, version string
	excluded      []string          // suites excluidas
	debs          map[string][]byte // arch -> contenido del .deb
}

// writeRelease escribe en root/dist los .deb, build.json y test.json de r
// tal como los dejan `debfactory build` y `debfactory test`.
func writeRelease(t testing.TB, root string, suites []string, r release) {
	t.Helper()
	dist := filepath.Join(root, "dist")
	os.MkdirAll(dist, 0o755)
	for arch, data := range r.debs {
		base := filepath.Join(dist, r.name+"_"+r.version+"_"+arch)
		if err := os.WriteFile(base+".deb", data, 0o644); err != nil {
			t.Fatal(err)
		}
		sha := fetch.SHA256(data)
		bi := &pkg.BuildInfo{SHA256: sha, Lintian: "pass", TestCommand: r.name + " --version"}
		b, _ := bi.Marshal()
		os.WriteFile(base+".build.json", b, 0o644)
		rep := installtest.Report{Package: r.name, Version: r.version, Arch: arch, DebSHA256: sha,
			Versions: "pass", Overall: "pass"}
		for _, s := range suites {
			res := installtest.Result{Suite: s, Status: "pass"}
			for _, e := range r.excluded {
				if e == s {
					res = installtest.Result{Suite: s, Status: "excluded", Reason: "glibc"}
				}
			}
			rep.Results = append(rep.Results, res)
		}
		tj, _ := json.MarshalIndent(rep, "", "  ")
		os.WriteFile(base+".test.json", tj, 0o644)
	}
}

// editJSON modifica un informe JSON de dist/.
func editJSON(t testing.TB, path string, f func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	f(m)
	out, _ := json.Marshal(m)
	os.WriteFile(path, out, 0o644)
}
