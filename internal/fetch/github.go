package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// GitHub consulta la API de releases. API es la base (por defecto
// https://api.github.com; los tests la cambian) y Token, si existe, se envía
// como Authorization (nunca se imprime).
type GitHub struct {
	Client *http.Client
	API    string
	Token  string
}

// Release es lo que la herramienta necesita de un release de upstream.
type Release struct {
	PublishedAt time.Time
	Assets      []string // nombres; vacío si upstream no publica releases en GitHub
	FromTag     bool     // sin release: la fecha es la del commit del tag
}

// PublishedAt devuelve la fecha de publicación del release de repo con ese tag
// (es el SOURCE_DATE_EPOCH del build).
func (g *GitHub) PublishedAt(ctx context.Context, repo, tag string) (time.Time, error) {
	r, err := g.Release(ctx, repo, tag)
	if err != nil {
		return time.Time{}, err
	}
	return r.PublishedAt, nil
}

// Release devuelve la fecha y los assets del release de repo con ese tag. Si
// upstream no publica releases en GitHub (ghostty publica en su sitio), usa
// la fecha del commit del tag (specs/009-hosted-hardened-builds).
func (g *GitHub) Release(ctx context.Context, repo, tag string) (*Release, error) {
	var body struct {
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	status, err := g.getJSON(ctx, "/repos/"+repo+"/releases/tags/"+neturl.PathEscape(tag), &body)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		t, err := g.tagDate(ctx, repo, tag)
		if err != nil {
			return nil, err
		}
		return &Release{PublishedAt: t, FromTag: true}, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("API de GitHub: release %s de %s: HTTP %d", tag, repo, status)
	}
	if body.PublishedAt.IsZero() {
		return nil, fmt.Errorf("API de GitHub: el release %s de %s no tiene published_at", tag, repo)
	}
	r := &Release{PublishedAt: body.PublishedAt}
	for _, a := range body.Assets {
		r.Assets = append(r.Assets, a.Name)
	}
	return r, nil
}

// tagDate es la fecha del commit al que apunta el tag (anotado o ligero).
func (g *GitHub) tagDate(ctx context.Context, repo, tag string) (time.Time, error) {
	var ref struct {
		Object struct{ Type, SHA string } `json:"object"`
	}
	status, err := g.getJSON(ctx, "/repos/"+repo+"/git/ref/tags/"+neturl.PathEscape(tag), &ref)
	if err != nil {
		return time.Time{}, err
	}
	if status != http.StatusOK {
		return time.Time{}, fmt.Errorf("API de GitHub: %s no tiene release ni tag %s (HTTP %d)", repo, tag, status)
	}
	sha := ref.Object.SHA
	if ref.Object.Type == "tag" {
		var t struct {
			Object struct{ SHA string } `json:"object"`
		}
		if status, err := g.getJSON(ctx, "/repos/"+repo+"/git/tags/"+sha, &t); err != nil || status != http.StatusOK {
			return time.Time{}, fmt.Errorf("API de GitHub: tag anotado %s de %s ilegible", tag, repo)
		}
		sha = t.Object.SHA
	}
	var c struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	}
	if status, err := g.getJSON(ctx, "/repos/"+repo+"/git/commits/"+sha, &c); err != nil || status != http.StatusOK || c.Committer.Date.IsZero() {
		return time.Time{}, fmt.Errorf("API de GitHub: commit del tag %s de %s ilegible", tag, repo)
	}
	return c.Committer.Date, nil
}

// getJSON hace un GET a la API y decodifica el cuerpo si es 200.
func (g *GitHub) getJSON(ctx context.Context, path string, v any) (int, error) {
	api := g.API
	if api == "" {
		api = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "debfactory")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("API de GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return 0, fmt.Errorf("API de GitHub: respuesta ilegible: %w", err)
	}
	return http.StatusOK, nil
}

// LicenseURL es la URL del archivo p del repo al tag exacto.
func LicenseURL(repo, tag, p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = neturl.PathEscape(s)
	}
	return "https://" + rawHost + "/" + repo + "/" + neturl.PathEscape(tag) + "/" + strings.Join(segs, "/")
}
