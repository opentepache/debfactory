// Package forge es un cliente REST mínimo de GitHub con net/http (issues,
// PRs, refs, configuración de Actions y dispatch). Aísla la forja para que
// cambiarla toque un solo paquete (specs/003-ci-pipeline, research D16).
//
// El token solo se envía a api.github.com (o a 127.0.0.1 en los tests) y
// nunca aparece en los errores.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"debfactory/internal/redact"
)

// UserAgent es el que exige el proyecto para todas las llamadas.
const UserAgent = "debfactory"

// Client habla con la API REST de un repositorio.
type Client struct {
	base  *url.URL
	repo  string // owner/name
	token string
	http  *http.Client
	rate  Rate // según la última respuesta (fase 005, FR-021)
}

// New crea un cliente. baseURL es https://api.github.com en producción; un
// http://127.0.0.1:<puerto> solo se admite para los tests.
func New(baseURL, repo, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("forge: URL de la API inválida")
	}
	switch {
	case u.Scheme == "https" && u.Host == "api.github.com":
	case u.Scheme == "http" && u.Hostname() == "127.0.0.1":
	default:
		return nil, fmt.Errorf("forge: no se envía el token a %s (solo api.github.com)", u.Host)
	}
	return &Client{base: u, repo: repo, token: token, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

// Repo devuelve owner/name.
func (c *Client) Repo() string { return c.repo }

// APIError es una respuesta HTTP de error. No contiene cabeceras ni token.
type APIError struct {
	Method, Path string
	Status       int
	Message      string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("forge: %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Message)
}

// IsNotFound indica un 404.
func IsNotFound(err error) bool {
	ae, ok := err.(*APIError)
	return ok && ae.Status == http.StatusNotFound
}

// do envía la petición y decodifica la respuesta en out (si no es nil).
// Devuelve el siguiente enlace de paginación, si lo hay.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (next string, err error) {
	resp, err := c.send(ctx, method, path, body, nil)
	if err != nil {
		return "", err
	}
	if out != nil && len(resp.data) > 0 {
		if err := json.Unmarshal(resp.data, out); err != nil {
			return "", fmt.Errorf("forge: %s %s: respuesta inválida: %v", method, cleanPath(path), err)
		}
	}
	return nextLink(resp.header.Get("Link")), nil
}

// response es una respuesta 2xx o 304 ya leída.
type response struct {
	status int
	header http.Header
	data   []byte
}

// send envía la petición con las cabeceras extra dadas y devuelve la
// respuesta si es 2xx o 304. Anota el límite de peticiones de cada respuesta;
// un rechazo por límite es un *RateLimitError y cualquier otro error HTTP un
// *APIError.
func (c *Client) send(ctx context.Context, method, path string, body any, extra map[string]string) (*response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	target := path
	if !strings.HasPrefix(path, "http") {
		target = c.base.String() + path
	} else if u, err := url.Parse(path); err != nil || u.Host != c.base.Host {
		return nil, fmt.Errorf("forge: enlace de paginación a otro host")
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", UserAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forge: %s %s: %s", method, cleanPath(path), redact.String(err.Error(), c.token))
	}
	defer resp.Body.Close()
	c.noteRate(resp.Header)
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		var msg struct {
			Message string `json:"message"`
		}
		json.Unmarshal(data, &msg)
		if msg.Message == "" {
			msg.Message = strings.TrimSpace(string(data))
			if len(msg.Message) > 200 {
				msg.Message = msg.Message[:200]
			}
		}
		if rl := rateLimited(resp, msg.Message, c.rate); rl != nil {
			rl.Method, rl.Path = method, cleanPath(path)
			return nil, rl
		}
		return nil, &APIError{Method: method, Path: cleanPath(path), Status: resp.StatusCode,
			Message: redact.String(msg.Message, c.token)}
	}
	return &response{status: resp.StatusCode, header: resp.Header, data: data}, nil
}

var nextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func nextLink(h string) string {
	if m := nextRe.FindStringSubmatch(h); m != nil {
		return m[1]
	}
	return ""
}

// cleanPath quita la consulta y el host de una ruta para los mensajes.
func cleanPath(p string) string {
	if u, err := url.Parse(p); err == nil {
		return u.Path
	}
	return p
}

func (c *Client) repoPath(format string, args ...any) string {
	return "/repos/" + c.repo + fmt.Sprintf(format, args...)
}

// --- Issues ---

// Issue es un issue (sin los campos que no usa debfactory).
type Issue struct {
	Number  int     `json:"number"`
	Title   string  `json:"title"`
	Body    string  `json:"body"`
	State   string  `json:"state"`
	HTMLURL string  `json:"html_url"`
	Labels  []Label `json:"labels"`
	// CreatedAt sirve al detector para saber si el manifiesto cambió después
	// de notificar (FR-018 de la fase 005).
	CreatedAt   time.Time `json:"created_at"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request,omitempty"`
}

// Label es una etiqueta.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// ListOpenIssues devuelve todos los issues abiertos con la etiqueta (sin
// PRs), recorriendo la paginación. No usa la API de búsqueda.
func (c *Client) ListOpenIssues(ctx context.Context, label string) ([]Issue, error) {
	var all []Issue
	path := c.repoPath("/issues?state=open&per_page=100&labels=%s", url.QueryEscape(label))
	for path != "" {
		var page []Issue
		next, err := c.do(ctx, http.MethodGet, path, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, is := range page {
			if is.PullRequest == nil {
				all = append(all, is)
			}
		}
		path = next
	}
	return all, nil
}

// CreateIssue abre un issue con las etiquetas dadas.
func (c *Client) CreateIssue(ctx context.Context, title, body string, labels []string) (*Issue, error) {
	var is Issue
	_, err := c.do(ctx, http.MethodPost, c.repoPath("/issues"),
		map[string]any{"title": title, "body": body, "labels": labels}, &is)
	return &is, err
}

// CommentIssue añade un comentario a un issue o PR.
func (c *Client) CommentIssue(ctx context.Context, number int, body string) error {
	_, err := c.do(ctx, http.MethodPost, c.repoPath("/issues/%d/comments", number), map[string]string{"body": body}, nil)
	return err
}

// CloseIssue cierra un issue.
func (c *Client) CloseIssue(ctx context.Context, number int) error {
	_, err := c.do(ctx, http.MethodPatch, c.repoPath("/issues/%d", number),
		map[string]string{"state": "closed", "state_reason": "completed"}, nil)
	return err
}

// GetLabel lee una etiqueta.
func (c *Client) GetLabel(ctx context.Context, name string) (*Label, error) {
	var l Label
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/labels/%s", url.PathEscape(name)), nil, &l)
	return &l, err
}

// CreateLabel crea una etiqueta.
func (c *Client) CreateLabel(ctx context.Context, name, color, description string) error {
	_, err := c.do(ctx, http.MethodPost, c.repoPath("/labels"),
		map[string]string{"name": name, "color": color, "description": description}, nil)
	return err
}

// --- Pull requests ---

// PR es un pull request.
type PR struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	Merged    bool      `json:"merged"`
	CreatedAt time.Time `json:"created_at"`
	Head      struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	// NodeID identifica el PR en la API GraphQL (auto-merge, fase 005).
	NodeID string `json:"node_id"`
	// AutoMerge no es nil si el PR tiene el auto-merge activado.
	AutoMerge *struct {
		MergeMethod string `json:"merge_method"`
	} `json:"auto_merge"`
}

// FindOpenPR devuelve el PR abierto de head hacia base, o nil.
func (c *Client) FindOpenPR(ctx context.Context, head, base string) (*PR, error) {
	owner, _, _ := strings.Cut(c.repo, "/")
	var prs []PR
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/pulls?state=open&head=%s&base=%s",
		url.QueryEscape(owner+":"+head), url.QueryEscape(base)), nil, &prs)
	if err != nil || len(prs) == 0 {
		return nil, err
	}
	return &prs[0], nil
}

// GetPR lee un PR.
func (c *Client) GetPR(ctx context.Context, number int) (*PR, error) {
	var pr PR
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/pulls/%d", number), nil, &pr)
	return &pr, err
}

// CreatePR abre un PR.
func (c *Client) CreatePR(ctx context.Context, head, base, title, body string) (*PR, error) {
	var pr PR
	_, err := c.do(ctx, http.MethodPost, c.repoPath("/pulls"),
		map[string]string{"head": head, "base": base, "title": title, "body": body}, &pr)
	return &pr, err
}

// UpdatePR cambia título y cuerpo.
func (c *Client) UpdatePR(ctx context.Context, number int, title, body string) error {
	_, err := c.do(ctx, http.MethodPatch, c.repoPath("/pulls/%d", number),
		map[string]string{"title": title, "body": body}, nil)
	return err
}

// SetStatus publica un estado de commit (context, p. ej. "verify") sobre sha.
// state es success, failure, error o pending.
func (c *Client) SetStatus(ctx context.Context, sha, state, context_, description, targetURL string) error {
	body := map[string]string{"state": state, "context": context_, "description": description}
	if targetURL != "" {
		body["target_url"] = targetURL
	}
	_, err := c.do(ctx, http.MethodPost, c.repoPath("/statuses/%s", sha), body, nil)
	return err
}

// --- Refs ---

// Ref es una referencia git.
type Ref struct {
	Ref    string `json:"ref"`
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

// GetRef lee heads/<branch>. Devuelve nil si no existe.
func (c *Client) GetRef(ctx context.Context, branch string) (*Ref, error) {
	var r Ref
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/git/ref/heads/%s", branch), nil, &r)
	if IsNotFound(err) {
		return nil, nil
	}
	return &r, err
}

// DeleteRef borra heads/<branch>; no falla si ya no existe.
func (c *Client) DeleteRef(ctx context.Context, branch string) error {
	_, err := c.do(ctx, http.MethodDelete, c.repoPath("/git/refs/heads/%s", branch), nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// --- Configuración de Actions (ci doctor) ---

// RepoInfo son los datos del repositorio que interesan a ci doctor.
type RepoInfo struct {
	Private bool `json:"private"`
	// AllowAutoMerge es "Allow auto-merge" (fase 005, research D10).
	AllowAutoMerge bool `json:"allow_auto_merge"`
}

// GetRepo lee los datos del repositorio.
func (c *Client) GetRepo(ctx context.Context) (*RepoInfo, error) {
	var r RepoInfo
	_, err := c.do(ctx, http.MethodGet, c.repoPath(""), nil, &r)
	return &r, err
}

// ForkPrivate es la política de workflows de forks en un repositorio privado.
type ForkPrivate struct {
	RunWorkflows    bool `json:"run_workflows_from_fork_pull_requests"`
	SendWriteTokens bool `json:"send_write_tokens_to_workflows"`
	SendSecrets     bool `json:"send_secrets_and_variables"`
}

// GetForkPrivate lee Settings → Actions → Fork pull request workflows in
// private repositories.
func (c *Client) GetForkPrivate(ctx context.Context) (*ForkPrivate, error) {
	var f ForkPrivate
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/actions/permissions/fork-pr-workflows-private-repos"), nil, &f)
	return &f, err
}

// ForkApproval es la política de aprobación de PRs de forks.
type ForkApproval struct {
	ApprovalPolicy string `json:"approval_policy"`
}

// GetForkApproval lee Settings → Actions → Fork pull request workflows.
func (c *Client) GetForkApproval(ctx context.Context) (*ForkApproval, error) {
	var f ForkApproval
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/actions/permissions/fork-pr-contributor-approval"), nil, &f)
	return &f, err
}

// WorkflowPermissions son los permisos por defecto del GITHUB_TOKEN.
type WorkflowPermissions struct {
	DefaultWorkflowPermissions   string `json:"default_workflow_permissions"`
	CanApprovePullRequestReviews bool   `json:"can_approve_pull_request_reviews"`
}

// GetWorkflowPermissions lee Settings → Actions → Workflow permissions.
func (c *Client) GetWorkflowPermissions(ctx context.Context) (*WorkflowPermissions, error) {
	var w WorkflowPermissions
	_, err := c.do(ctx, http.MethodGet, c.repoPath("/actions/permissions/workflow"), nil, &w)
	return &w, err
}

// Ruleset es un ruleset del repositorio (con sus reglas).
type Ruleset struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Target       string `json:"target"`
	Enforcement  string `json:"enforcement"`
	BypassActors []any  `json:"bypass_actors"`
	Conditions   struct {
		RefName struct {
			Include []string `json:"include"`
		} `json:"ref_name"`
	} `json:"conditions"`
	Rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			RequiredStatusChecks []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	} `json:"rules"`
}

// ListRulesets devuelve los rulesets con sus reglas.
func (c *Client) ListRulesets(ctx context.Context) ([]Ruleset, error) {
	var list []Ruleset
	if _, err := c.do(ctx, http.MethodGet, c.repoPath("/rulesets?per_page=100"), nil, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if _, err := c.do(ctx, http.MethodGet, c.repoPath("/rulesets/%d", list[i].ID), nil, &list[i]); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// LatestRelease devuelve el tag y la fecha del último release de otro
// repositorio público (actions/runner, para ci doctor).
func (c *Client) LatestRelease(ctx context.Context, repo string) (tag string, published time.Time, err error) {
	var r struct {
		TagName     string    `json:"tag_name"`
		PublishedAt time.Time `json:"published_at"`
	}
	_, err = c.do(ctx, http.MethodGet, "/repos/"+repo+"/releases/latest", nil, &r)
	return r.TagName, r.PublishedAt, err
}

// Dispatch lanza workflow_dispatch de un workflow en ref.
func (c *Client) Dispatch(ctx context.Context, workflow, ref string, inputs map[string]string) error {
	_, err := c.do(ctx, http.MethodPost, c.repoPath("/actions/workflows/%s/dispatches", workflow),
		map[string]any{"ref": ref, "inputs": inputs}, nil)
	return err
}
