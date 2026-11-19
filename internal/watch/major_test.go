package watch

import "testing"

// TestIsMajor: FR-015a.
func TestIsMajor(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"4.3.0", "5.0.0", true},
		{"4.3.0", "4.4.0", false},
		{"4.3.0", "4.3.1", false},
		{"0.46.0", "0.47.0", true},
		{"0.46.0", "0.46.1", false},
		{"0.9", "1.0", true},
		{"2026.11.9", "2026.12.1", false},
		{"2026.12.1", "2027.1.0", true},
		{"r45", "r46", true},
		{"4.3.0", "04.4.0", false},
		{"1", "1.1", false},
		{"0", "0.1", true},
	}
	for _, c := range cases {
		if got := IsMajor(c.from, c.to); got != c.want {
			t.Errorf("IsMajor(%q, %q) = %v; se esperaba %v", c.from, c.to, got, c.want)
		}
	}
}
