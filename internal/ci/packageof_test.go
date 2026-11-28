package ci

import "testing"

// TestPackageOf: un cambio en cualquier archivo de un paquete lo afecta,
// también en packages/<p>/debian/ (nivel 2) y en la ubicación autocontenida
// (specs/009, research D21).
func TestPackageOf(t *testing.T) {
	cases := map[string]string{
		"packages/fnm.yaml":                           "fnm",
		"packages/fnm.lock.yaml":                      "fnm",
		"packages/ghostty/ghostty.yaml":               "ghostty",
		"packages/ghostty/ghostty.lock.yaml":          "ghostty",
		"packages/tepache-hello/debian/rules":         "tepache-hello",
		"packages/tepache-hello/debian/source/format": "tepache-hello",
		"config/debfactory.yaml":                      "",
		"packages/":                                   "",
		"docs/usuario.md":                             "",
	}
	for path, want := range cases {
		got, ok := packageOf(path)
		if got != want || ok != (want != "") {
			t.Errorf("packageOf(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
}
