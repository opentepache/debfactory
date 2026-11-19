package watch

import "time"

// Candidate es la versión que el detector propone (data-model.md §4).
type Candidate struct {
	From, To    string // versión del manifiesto en main y versión nueva
	Tag         string
	PublishedAt time.Time
	NotesURL    string
	Assets      []string
	Digests     map[string]string
}

// Waiting es una versión estable más nueva que aún no cumple la espera
// (FR-004a); Eligible es cuándo la cumplirá.
type Waiting struct {
	Version  string
	Eligible time.Time
}

// Pick elige la candidata entre los releases de upstream (research D6): la
// versión estable más alta, mayor que current, publicada hace al menos
// cooldown. waiting es la versión estable más alta que todavía espera y que
// supera a la candidata (o a current si no hay candidata); sirve para el
// resumen del ciclo.
func Pick(current, tagTmpl string, ignoreFlag bool, releases []Release, now time.Time, cooldown time.Duration) (cand *Candidate, waiting *Waiting) {
	for _, r := range releases {
		v, reason := Stable(r, tagTmpl, ignoreFlag)
		if reason != "" || Compare(v, current) <= 0 || r.PublishedAt.IsZero() {
			continue
		}
		eligible := r.PublishedAt.Add(cooldown)
		if eligible.After(now) {
			if waiting == nil || Compare(v, waiting.Version) > 0 {
				waiting = &Waiting{Version: v, Eligible: eligible}
			}
			continue
		}
		if cand == nil || Compare(v, cand.To) > 0 {
			cand = &Candidate{From: current, To: v, Tag: r.Tag, PublishedAt: r.PublishedAt, NotesURL: r.URL,
				Assets: r.Assets, Digests: r.Digests}
		}
	}
	if waiting != nil && cand != nil && Compare(waiting.Version, cand.To) <= 0 {
		waiting = nil
	}
	return cand, waiting
}
