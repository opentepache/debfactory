package watch

import (
	"testing"
	"time"
)

// TestPick: research D6, FR-004 y FR-004a.
func TestPick(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	rel := func(tag string, h int) Release {
		return Release{Tag: tag, PublishedAt: ago(h), URL: "https://example.invalid/" + tag}
	}
	const day2 = 48 * time.Hour
	cases := []struct {
		name     string
		current  string
		releases []Release
		cooldown time.Duration
		want     string // candidata, o ""
		waiting  string // versión en espera, o ""
	}{
		{"sin novedades", "4.3.0", []Release{rel("v4.3.0", 900), rel("v4.2.0", 2000)}, day2, "", ""},
		{"una nueva elegible", "4.3.0", []Release{rel("v4.4.0", 49), rel("v4.3.0", 900)}, day2, "4.4.0", ""},
		{"la más alta", "4.3.0", []Release{rel("v4.4.0", 100), rel("v4.5.0", 60), rel("v4.4.1", 80)}, day2, "4.5.0", ""},
		{"orden de versión, no de texto", "4.9.0", []Release{rel("v4.10.0", 60), rel("v4.9.1", 60)}, day2, "4.10.0", ""},
		{"menor que la publicada", "4.3.0", []Release{rel("v4.2.9", 60), rel("v3.9.0", 60)}, day2, "", ""},
		{"en espera", "4.3.0", []Release{rel("v4.4.0", 10)}, day2, "", "4.4.0"},
		{"US1 escenario 5", "4.3.0", []Release{rel("v4.4.1", 5), rel("v4.4.0", 60)}, day2, "4.4.0", "4.4.1"},
		{"espera más vieja que la candidata no cuenta", "4.3.0", []Release{rel("v4.5.0", 60), rel("v4.4.1", 5)}, day2, "4.5.0", ""},
		{"borrador descartado", "4.3.0", []Release{{Tag: "v4.4.0", Draft: true, PublishedAt: ago(100)}}, day2, "", ""},
		{"pre-release descartado", "4.3.0", []Release{{Tag: "v4.4.0", Prerelease: true, PublishedAt: ago(100)}}, day2, "", ""},
		{"rc descartada", "4.3.0", []Release{rel("v4.4.0-rc1", 100)}, day2, "", ""},
		{"sin fecha", "4.3.0", []Release{{Tag: "v4.4.0"}}, day2, "", ""},
		{"sin espera", "4.3.0", []Release{rel("v4.4.0", 0)}, 0, "4.4.0", ""},
		{"justo en el límite", "4.3.0", []Release{rel("v4.4.0", 48)}, day2, "4.4.0", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cand, wait := Pick(c.current, "v{{version}}", false, c.releases, now, c.cooldown)
			got := ""
			if cand != nil {
				got = cand.To
				if cand.From != c.current || cand.Tag != "v"+cand.To || cand.NotesURL != "https://example.invalid/v"+cand.To {
					t.Errorf("candidata incompleta: %+v", cand)
				}
			}
			if got != c.want {
				t.Errorf("candidata = %q; se esperaba %q", got, c.want)
			}
			w := ""
			if wait != nil {
				w = wait.Version
				var pub time.Time
				for _, r := range c.releases {
					if r.Tag == "v"+w {
						pub = r.PublishedAt
					}
				}
				if !wait.Eligible.Equal(pub.Add(c.cooldown)) {
					t.Errorf("elegible = %v; se esperaba %v", wait.Eligible, pub.Add(c.cooldown))
				}
			}
			if w != c.waiting {
				t.Errorf("en espera = %q; se esperaba %q", w, c.waiting)
			}
		})
	}
}
