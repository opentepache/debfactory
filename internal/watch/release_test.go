package watch

import (
	"os/exec"
	"testing"

	"debfactory/internal/installtest"
)

func TestVersionFromTag(t *testing.T) {
	cases := []struct {
		tmpl, tag, want string
		ok              bool
	}{
		{"v{{version}}", "v1.2.3", "1.2.3", true},
		{"v{{version}}", "1.2.3", "", false},
		{"{{version}}", "1.2.3", "1.2.3", true},
		{"release-{{version}}", "release-1.3.0", "1.3.0", true},
		{"release-{{version}}", "v1.3.0", "", false},
		{"{{version}}-stable", "2.0-stable", "2.0", true},
		{"v{{version}}", "v", "", false},
		{"cli-v{{version}}", "sdk-v1.0.0", "", false},
		{"fijo", "fijo", "", false},
		{"{{version}}{{version}}", "11", "", false},
	}
	for _, c := range cases {
		got, ok := VersionFromTag(c.tmpl, c.tag)
		if got != c.want || ok != c.ok {
			t.Errorf("VersionFromTag(%q, %q) = %q, %v; se esperaba %q, %v", c.tmpl, c.tag, got, ok, c.want, c.ok)
		}
	}
}

// TestCompare: el orden de dpkg (constitución VI), contrastado con
// `dpkg --compare-versions` cuando está disponible.
func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.10", "1.2.9", 1},
		{"1.10", "1.9.9", 1},
		{"4.4.0", "4.3.0", 1},
		{"0.47.0", "0.46.1", 1},
		{"1.0", "1.0.0", -1},
		{"1.0~rc1", "1.0", -1},
		{"1.0a", "1.0", 1},
		{"1.0+b1", "1.0", 1},
		{"1.0+b1", "1.0a", 1},
		{"2026.12.1", "2026.11.9", 1},
		{"007", "7", 0},
		{"1.2.3", "1.2.3.0", -1},
		{"10.0", "9.99", 1},
	}
	_, err := exec.LookPath("dpkg")
	haveDpkg := err == nil
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d; se esperaba %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d; no es antisimétrica", c.b, c.a, got)
		}
		if !haveDpkg {
			continue
		}
		op := map[int]string{-1: "lt", 0: "eq", 1: "gt"}[c.want]
		if ok, err := installtest.CompareVersions(c.a, op, c.b); err != nil || !ok {
			t.Errorf("dpkg --compare-versions %s %s %s no lo confirma (%v)", c.a, op, c.b, err)
		}
	}
}
