package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/testutil"
)

const mitText = `MIT License

Copyright (c) 2024 Someone

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

// TestDepsLicenses (FR-027, constitución IV 1.12.0): el gate de licencias de
// las dependencias falla cerrado y las excepciones valen solo para la
// dependencia declarada.
func TestDepsLicenses(t *testing.T) {
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := t.TempDir()
	put := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(s), 0o644)
	}
	put(filepath.Join(dir, "a-1-h", "License.TXT"), mitText)         // nombre sin distinguir mayúsculas
	put(filepath.Join(dir, "b-1-h", "LICENSES", "MIT.txt"), mitText) // REUSE
	put(filepath.Join(dir, "c-1-h", "COPYING"), "texto propio que nadie reconoce")
	put(filepath.Join(dir, "d-1-h", "build.zig"), "")
	put(filepath.Join(root, "packages", "p", "licenses", "d.txt"), mitText)
	deps := []lock.Dep{{Name: "a", Hash: "a-1-h"}, {Name: "b", Hash: "b-1-h"}, {Name: "c", Hash: "c-1-h"}, {Name: "d", Hash: "d-1-h"}}
	e := &Env{Root: root, Cfg: cfg}
	m := &manifest.Manifest{Name: "p"}
	err = e.depsLicenses(m, dir, deps)
	if err == nil || !strings.Contains(err.Error(), "c: licencia no reconocida") ||
		!strings.Contains(err.Error(), "d: sin archivo de licencia") || strings.Contains(err.Error(), "a:") ||
		strings.Contains(err.Error(), "b:") {
		t.Fatalf("sin excepciones: %v", err)
	}
	m.LicenseExceptions = []manifest.LicenseException{
		{Dependency: "c", Licenses: []string{"MIT"}, Reason: "texto revisado a mano"},
		{Dependency: "d", Licenses: []string{"MIT"}, RepoFile: "licenses/d.txt", Reason: "upstream no lo incluye"},
	}
	if err := e.depsLicenses(m, dir, deps); err != nil {
		t.Fatalf("con excepciones: %v", err)
	}
	m.LicenseExceptions = append(m.LicenseExceptions, manifest.LicenseException{Dependency: "zz", Licenses: []string{"MIT"}, Reason: "no existe aquí"})
	if err := e.depsLicenses(m, dir, deps); err == nil || !strings.Contains(err.Error(), `"zz"`) {
		t.Fatalf("excepción sobrante: %v", err)
	}
}
