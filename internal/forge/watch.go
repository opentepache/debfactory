package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Llamadas que usa el detector de versiones (specs/005-upstream-release-watch,
// research D9–D11): releases de otros repositorios con peticiones
// condicionales, el límite de peticiones, PRs del detector y auto-merge.

// Rate es el límite de peticiones según la última respuesta de la API
// (cabeceras X-RateLimit-*). Known es false si ninguna respuesta las traía.
type Rate struct {
	Limit, Remaining int
	Reset            time.Time
	Known            bool
}

// Rate devuelve el límite según la última respuesta.
func (c *Client) Rate() Rate { return c.rate }

func (c *Client) noteRate(h http.Header) {
	rem, err := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if err != nil {
		return
	}
	r := Rate{Remaining: rem, Known: true}
	r.Limit, _ = strconv.Atoi(h.Get("X-RateLimit-Limit"))
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		r.Reset = time.Unix(reset, 0).UTC()
	}
	c.rate = r
}

// RateLimitError es un rechazo por el límite de peticiones, primario
// (X-RateLimit-Remaining: 0) o secundario (Retry-After). El detector no
// reintenta: se detiene y sigue en el ciclo siguiente (FR-021).
type RateLimitError struct {
	Method, Path string
	Reset        time.Time // cuándo se recupera, si la API lo dice
}

func (e *RateLimitError) Error() string {
	msg := fmt.Sprintf("forge: %s %s: límite de peticiones de la API alcanzado", e.Method, e.Path)
	if !e.Reset.IsZero() {
		msg += " (se recupera el " + e.Reset.Format(time.RFC3339) + ")"
	}
	return msg
}

// IsRateLimit indica un *RateLimitError.
func IsRateLimit(err error) bool {
	var rl *RateLimitError
	return errors.As(err, &rl)
}

func rateLimited(resp *http.Response, msg string, rate Rate) *RateLimitError {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		secs, _ := strconv.Atoi(ra)
		return &RateLimitError{Reset: time.Now().Add(time.Duration(secs) * time.Second).UTC()}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(msg), "rate limit") {
		return &RateLimitError{Reset: rate.Reset}
	}
	return nil
}

var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Release es un release de un repositorio upstream.
type Release struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []struct {
		Name string `json:"name"`
		// Digest es "sha256:<hex>" (GitHub lo calcula al subir el asset);
		// puede faltar en releases antiguos.
		Digest string `json:"digest"`
	} `json:"assets"`
}

// ListReleases pide los 30 releases más recientes de repo (otro repositorio,
// el upstream de un paquete). Con etag no vacío la petición es condicional:
// si nada cambió, notModified es true y body está vacío (un 304 no cuenta
// contra el límite). body es la respuesta tal cual, para la caché.
func (c *Client) ListReleases(ctx context.Context, repo, etag string) (body []byte, newETag string, notModified bool, err error) {
	if !repoNameRe.MatchString(repo) || strings.Contains(repo, "..") {
		return nil, "", false, fmt.Errorf("forge: repositorio upstream inválido %q", repo)
	}
	var extra map[string]string
	if etag != "" {
		extra = map[string]string{"If-None-Match": etag}
	}
	resp, err := c.send(ctx, http.MethodGet, "/repos/"+repo+"/releases?per_page=30", nil, extra)
	if err != nil {
		return nil, "", false, err
	}
	if resp.status == http.StatusNotModified {
		return nil, etag, true, nil
	}
	return resp.data, resp.header.Get("ETag"), false, nil
}

// ParseReleases decodifica la respuesta de ListReleases.
func ParseReleases(body []byte) ([]Release, error) {
	var rs []Release
	if err := json.Unmarshal(body, &rs); err != nil {
		return nil, fmt.Errorf("forge: releases: respuesta inválida: %v", err)
	}
	return rs, nil
}

// ListOpenPRs devuelve los PR abiertos contra base (todas las páginas).
func (c *Client) ListOpenPRs(ctx context.Context, base string) ([]PR, error) {
	var all []PR
	path := c.repoPath("/pulls?state=open&per_page=100&base=%s", url.QueryEscape(base))
	for path != "" {
		var page []PR
		next, err := c.do(ctx, http.MethodGet, path, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		path = next
	}
	return all, nil
}

// ClosePR cierra un PR sin integrarlo.
func (c *Client) ClosePR(ctx context.Context, number int) error {
	_, err := c.do(ctx, http.MethodPatch, c.repoPath("/pulls/%d", number), map[string]string{"state": "closed"}, nil)
	return err
}

// Comment es un comentario de un issue o PR.
type Comment struct {
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

// ListComments devuelve los comentarios de un issue o PR (todas las páginas).
func (c *Client) ListComments(ctx context.Context, number int) ([]Comment, error) {
	var all []Comment
	path := c.repoPath("/issues/%d/comments?per_page=100", number)
	for path != "" {
		var page []Comment
		next, err := c.do(ctx, http.MethodGet, path, nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		path = next
	}
	return all, nil
}

// UserID devuelve el id numérico de una cuenta (p. ej. "<app>[bot]"), para la
// dirección noreply de sus commits.
func (c *Client) UserID(ctx context.Context, login string) (int64, error) {
	var u struct {
		ID int64 `json:"id"`
	}
	_, err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(login), nil, &u)
	return u.ID, err
}

// graphql envía una consulta a la API GraphQL (misma base que la REST).
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any) error {
	var out struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := c.do(ctx, http.MethodPost, "/graphql", map[string]any{"query": query, "variables": vars}, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		msgs := make([]string, len(out.Errors))
		for i, e := range out.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("forge: graphql: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// EnableAutoMerge activa el auto-merge nativo (método merge) de un PR: GitHub
// lo integra cuando pasen los checks obligatorios del ruleset (research D10).
func (c *Client) EnableAutoMerge(ctx context.Context, prNodeID string) error {
	return c.graphql(ctx, `mutation($id: ID!) {
  enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: MERGE}) { clientMutationId }
}`, map[string]any{"id": prNodeID})
}

// DisableAutoMerge lo desactiva.
func (c *Client) DisableAutoMerge(ctx context.Context, prNodeID string) error {
	return c.graphql(ctx, `mutation($id: ID!) {
  disablePullRequestAutoMerge(input: {pullRequestId: $id}) { clientMutationId }
}`, map[string]any{"id": prNodeID})
}

// Environment es un environment de Actions (para ci doctor: watch.yml lee la
// llave de la App desde el environment "watch", limitado a main).
type Environment struct {
	Name                   string `json:"name"`
	DeploymentBranchPolicy *struct {
		ProtectedBranches    bool `json:"protected_branches"`
		CustomBranchPolicies bool `json:"custom_branch_policies"`
	} `json:"deployment_branch_policy"`
}

// GetEnvironment lee un environment.
func (c *Client) GetEnvironment(ctx context.Context, name string) (*Environment, error) {
	var e Environment
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/environments/%s", url.PathEscape(name)), nil, &e)
	return &e, err
}

// EnvironmentBranches devuelve las ramas y patrones permitidos de un
// environment con custom_branch_policies.
func (c *Client) EnvironmentBranches(ctx context.Context, name string) ([]string, error) {
	var out struct {
		BranchPolicies []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"branch_policies"`
	}
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/environments/%s/deployment-branch-policies?per_page=100", url.PathEscape(name)), nil, &out)
	var names []string
	for _, p := range out.BranchPolicies {
		names = append(names, p.Type+":"+p.Name)
	}
	return names, err
}

// EnvironmentSecrets devuelve los nombres (nunca los valores) de los secretos
// de un environment.
func (c *Client) EnvironmentSecrets(ctx context.Context, name string) ([]string, error) {
	var out struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/environments/%s/secrets?per_page=100", url.PathEscape(name)), nil, &out)
	var names []string
	for _, s := range out.Secrets {
		names = append(names, s.Name)
	}
	return names, err
}
