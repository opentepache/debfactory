package license

import (
	"slices"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := map[string]string{
		"MIT":                                  "MIT",
		"MIT OR Apache-2.0":                    "MIT,Apache-2.0",
		"(MIT AND BSD-3-Clause) OR Apache-2.0": "MIT,BSD-3-Clause,Apache-2.0",
		"GPL-2.0-only WITH Classpath-exception-2.0": "GPL-2.0-only WITH Classpath-exception-2.0",
		"GPL-2.0+":               "GPL-2.0+",
		"LicenseRef-foo AND MIT": "LicenseRef-foo,MIT",
	}
	for in, want := range cases {
		e, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		var got []string
		for _, term := range Terms(e) {
			got = append(got, term.String())
		}
		if s := join(got); s != want {
			t.Errorf("Terms(%q) = %s; se esperaba %s", in, s, want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"", "MIT/Apache", "MIT OR", "(MIT", "MIT)", "AND MIT", "MIT WITH", "(MIT OR X) WITH Y", "mit or apache"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) debería fallar", in)
		}
	}
}

func TestAllowed(t *testing.T) {
	allow := func(id string) bool { return slices.Contains([]string{"MIT", "Apache-2.0"}, id) }
	cases := []struct {
		expr string
		ok   bool
		bad  string
	}{
		{"MIT", true, ""},
		{"BUSL-1.1", false, "BUSL-1.1"},
		{"MIT OR BUSL-1.1", true, ""},
		{"BUSL-1.1 OR SSPL-1.0", false, "BUSL-1.1"},
		{"MIT AND BUSL-1.1", false, "BUSL-1.1"},
		{"MIT AND Apache-2.0", true, ""},
		{"(MIT AND BUSL-1.1) OR Apache-2.0", true, ""},
		{"GPL-2.0-only WITH Classpath-exception-2.0", false, "GPL-2.0-only WITH Classpath-exception-2.0"},
	}
	for _, c := range cases {
		e, err := Parse(c.expr)
		if err != nil {
			t.Fatal(err)
		}
		ok, bad := Allowed(e, allow)
		if ok != c.ok || bad != c.bad {
			t.Errorf("Allowed(%q) = %v, %q; se esperaba %v, %q", c.expr, ok, bad, c.ok, c.bad)
		}
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}
