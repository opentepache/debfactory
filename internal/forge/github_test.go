package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const token = "ghs_SECRETO_de_prueba_123"

func TestHostRestriction(t *testing.T) {
	for _, u := range []string{"https://api.github.com", "http://127.0.0.1:8080"} {
		if _, err := New(u, "o/r", token); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"https://evil.example", "http://api.github.com", "https://api.github.com.evil.example", "http://localhost:80"} {
		if _, err := New(u, "o/r", token); err == nil {
			t.Errorf("%s: se esperaba rechazo", u)
		}
	}
}

func TestHeadersAndPagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "debfactory" || r.Header.Get("Authorization") != "Bearer "+token ||
			r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Errorf("cabeceras: %v", r.Header)
		}
		page := r.URL.Query().Get("page")
		var out []map[string]any
		switch page {
		case "", "1":
			for i := 1; i <= 100; i++ {
				out = append(out, map[string]any{"number": i, "title": "x"})
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/issues?page=2>; rel="next", <%s/repos/o/r/issues?page=2>; rel="last"`, srv.URL, srv.URL))
		case "2":
			for i := 101; i <= 150; i++ {
				out = append(out, map[string]any{"number": i})
			}
			out = append(out, map[string]any{"number": 999, "pull_request": map[string]string{"url": "x"}})
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	c, err := New(srv.URL, "o/r", token)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := c.ListOpenIssues(context.Background(), "pipeline-failure")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 150 {
		t.Errorf("%d issues; se esperaban 150 (sin el PR)", len(issues))
	}
}

func TestErrorHasNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprintf(w, `{"message":"fallo interno con %s y Authorization: Bearer %s"}`, token, token)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "o/r", token)
	_, err := c.CreateIssue(context.Background(), "t", "b", nil)
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("el token aparece en el error: %v", err)
	}
	if !strings.Contains(err.Error(), "POST /repos/o/r/issues: HTTP 500") {
		t.Errorf("error = %v", err)
	}
}

func TestNextLinkOtherHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://evil.example/steal>; rel="next"`)
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "o/r", token)
	if _, err := c.ListOpenIssues(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "otro host") {
		t.Errorf("err = %v", err)
	}
}

func TestGetRefNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "o/r", token)
	ref, err := c.GetRef(context.Background(), "debfactory/state")
	if err != nil || ref != nil {
		t.Errorf("GetRef = %v, %v", ref, err)
	}
	if err := c.DeleteRef(context.Background(), "x"); err != nil {
		t.Errorf("DeleteRef: %v", err)
	}
}
