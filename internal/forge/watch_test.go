package forge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestListReleasesConditional: la segunda petición lleva If-None-Match y un
// 304 no devuelve cuerpo (research D9). El límite se anota de cada respuesta.
func TestListReleasesConditional(t *testing.T) {
	const etag = `"abc123"`
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/MordechaiHadad/bob/releases" || r.URL.Query().Get("per_page") != "30" {
			t.Errorf("ruta = %s", r.URL)
		}
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4990")
		w.Header().Set("X-RateLimit-Reset", "1795000000")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Write([]byte(`[{"tag_name":"v4.4.0","draft":false,"prerelease":false,"published_at":"2026-11-18T08:00:00Z",
			"html_url":"https://github.com/MordechaiHadad/bob/releases/tag/v4.4.0","assets":[{"name":"bob-linux-x86_64.zip"}]}]`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "o/r", token)
	ctx := context.Background()
	body, got, nm, err := c.ListReleases(ctx, "MordechaiHadad/bob", "")
	if err != nil || nm || got != etag {
		t.Fatalf("primera: %v %q %v", err, got, nm)
	}
	rs, err := ParseReleases(body)
	if err != nil || len(rs) != 1 || rs[0].TagName != "v4.4.0" || rs[0].Assets[0].Name != "bob-linux-x86_64.zip" ||
		!rs[0].PublishedAt.Equal(time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("releases = %+v %v", rs, err)
	}
	body, got, nm, err = c.ListReleases(ctx, "MordechaiHadad/bob", etag)
	if err != nil || !nm || got != etag || len(body) != 0 {
		t.Fatalf("condicional: %v %q %v %q", err, got, nm, body)
	}
	if r := c.Rate(); !r.Known || r.Remaining != 4990 || r.Limit != 5000 || r.Reset.Unix() != 1795000000 {
		t.Errorf("límite = %+v", r)
	}
	if _, _, _, err := c.ListReleases(ctx, "../../etc/passwd", ""); err == nil || calls != 2 {
		t.Errorf("repositorio inválido aceptado: %v", err)
	}
}

// TestRateLimitError: un 403 con X-RateLimit-Remaining: 0 y un 429 con
// Retry-After son *RateLimitError; un 403 normal sigue siendo *APIError.
func TestRateLimitError(t *testing.T) {
	mode := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "primary":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1795000000")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"API rate limit exceeded for installation"}`))
		case "secondary":
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
		}
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "o/r", token)
	for _, m := range []string{"primary", "secondary"} {
		mode = m
		_, _, _, err := c.ListReleases(context.Background(), "a/b", "")
		if !IsRateLimit(err) || strings.Contains(err.Error(), token) {
			t.Errorf("%s: error = %v", m, err)
		}
	}
	mode = ""
	_, _, _, err := c.ListReleases(context.Background(), "a/b", "")
	if IsRateLimit(err) {
		t.Errorf("un 403 sin límite no es de límite: %v", err)
	}
}

// TestAutoMergeGraphQL: la mutación lleva el node_id del PR y los errores de
// GraphQL (con HTTP 200) son errores.
func TestAutoMergeGraphQL(t *testing.T) {
	var got map[string]any
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("petición = %s %s", r.Method, r.URL)
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		if fail {
			w.Write([]byte(`{"errors":[{"message":"Pull request Auto merge is not allowed for this repository"}]}`))
			return
		}
		w.Write([]byte(`{"data":{"enablePullRequestAutoMerge":{"clientMutationId":null}}}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "o/r", token)
	if err := c.EnableAutoMerge(context.Background(), "PR_kwDO123"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["query"].(string), "enablePullRequestAutoMerge") || !strings.Contains(got["query"].(string), "MERGE") ||
		got["variables"].(map[string]any)["id"] != "PR_kwDO123" {
		t.Errorf("mutación = %v", got)
	}
	fail = true
	if err := c.DisableAutoMerge(context.Background(), "PR_kwDO123"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error = %v", err)
	}
	if !strings.Contains(got["query"].(string), "disablePullRequestAutoMerge") {
		t.Errorf("mutación = %v", got)
	}
}
