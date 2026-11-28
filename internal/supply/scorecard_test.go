package supply

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestScorecard (T044): puntuación y las tres comprobaciones más bajas (sin
// las que no aplican); sin respuesta, "no disponible".
func TestScorecard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "debfactory" || r.URL.Path != "/projects/github.com/aquaproj/aqua" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"score":6.5,"date":"2026-08-31","checks":[{"name":"Binary-Artifacts","score":10},
{"name":"Branch-Protection","score":3},{"name":"CII-Best-Practices","score":0},{"name":"Code-Review","score":2},
{"name":"Packaging","score":-1},{"name":"Fuzzing","score":0}]}`))
	}))
	defer srv.Close()
	s := FetchScorecard(context.Background(), srv.Client(), srv.URL, "aquaproj/aqua")
	if !s.Available || s.Score != 6.5 || len(s.Lowest) != 3 || s.Lowest[0].Name != "CII-Best-Practices" ||
		s.Lowest[1].Name != "Fuzzing" || s.Lowest[2].Name != "Code-Review" {
		t.Fatalf("scorecard = %+v", s)
	}
	if md := s.Markdown("aquaproj/aqua"); !strings.Contains(md, "6.5/10") || !strings.Contains(md, "CII-Best-Practices (0)") {
		t.Errorf("markdown = %s", md)
	}
	none := FetchScorecard(context.Background(), srv.Client(), srv.URL, "otro/repo")
	if none.Available || !strings.Contains(none.Markdown("otro/repo"), "no disponible") {
		t.Errorf("sin datos = %+v", none)
	}
}
