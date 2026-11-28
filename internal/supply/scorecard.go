package supply

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// ScorecardAPI es la API pública de OpenSSF Scorecard (research D12).
const ScorecardAPI = "https://api.securityscorecards.dev"

// Scorecard es el resumen que va al informe y al comentario del PR. Es una
// señal de aviso: nunca bloquea (FR-017).
type Scorecard struct {
	Available bool             `json:"available"`
	Score     float64          `json:"score,omitempty"`
	Date      string           `json:"date,omitempty"`
	Lowest    []ScorecardCheck `json:"lowest,omitempty"` // las tres comprobaciones más bajas
}

// ScorecardCheck es una comprobación de Scorecard.
type ScorecardCheck struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

// FetchScorecard consulta la puntuación de github.com/<upstream>. Sin
// respuesta o con error devuelve Available=false (sin error: es un aviso).
func FetchScorecard(ctx context.Context, c *http.Client, api, upstream string) *Scorecard {
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	if api == "" {
		api = ScorecardAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"/projects/github.com/"+upstream, nil)
	if err != nil {
		return &Scorecard{}
	}
	req.Header.Set("User-Agent", "debfactory")
	resp, err := c.Do(req)
	if err != nil {
		return &Scorecard{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &Scorecard{}
	}
	var raw struct {
		Score  float64 `json:"score"`
		Date   string  `json:"date"`
		Checks []struct {
			Name  string `json:"name"`
			Score int    `json:"score"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return &Scorecard{}
	}
	s := &Scorecard{Available: true, Score: raw.Score, Date: raw.Date}
	var checks []ScorecardCheck
	for _, ch := range raw.Checks {
		if ch.Score >= 0 { // -1 = no aplica
			checks = append(checks, ScorecardCheck{Name: ch.Name, Score: ch.Score})
		}
	}
	sort.SliceStable(checks, func(i, j int) bool {
		if checks[i].Score != checks[j].Score {
			return checks[i].Score < checks[j].Score
		}
		return checks[i].Name < checks[j].Name
	})
	if len(checks) > 3 {
		checks = checks[:3]
	}
	s.Lowest = checks
	return s
}

// Markdown resume la puntuación para el comentario del PR.
func (s *Scorecard) Markdown(upstream string) string {
	if s == nil || !s.Available {
		return fmt.Sprintf("**OpenSSF Scorecard** de `%s`: no disponible.", upstream)
	}
	out := fmt.Sprintf("**OpenSSF Scorecard** de `%s`: %.1f/10 (%s).", upstream, s.Score, s.Date)
	if len(s.Lowest) > 0 {
		out += " Más bajas:"
		for i, c := range s.Lowest {
			sep := ","
			if i == 0 {
				sep = ""
			}
			out += fmt.Sprintf("%s %s (%d)", sep, c.Name, c.Score)
		}
		out += "."
	}
	return out
}
