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

// PublishedAt devuelve la fecha de publicación del release de repo con ese tag
// (es el SOURCE_DATE_EPOCH del build).
func (g *GitHub) PublishedAt(ctx context.Context, repo, tag string) (time.Time, error) {
	api := g.API
	if api == "" {
		api = "https://api.github.com"
	}
	url := api + "/repos/" + repo + "/releases/tags/" + neturl.PathEscape(tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
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
		return time.Time{}, fmt.Errorf("API de GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("API de GitHub: release %s de %s: HTTP %d", tag, repo, resp.StatusCode)
	}
	var body struct {
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return time.Time{}, fmt.Errorf("API de GitHub: respuesta ilegible: %w", err)
	}
	if body.PublishedAt.IsZero() {
		return time.Time{}, fmt.Errorf("API de GitHub: el release %s de %s no tiene published_at", tag, repo)
	}
	return body.PublishedAt, nil
}

// LicenseURL es la URL del archivo p del repo al tag exacto.
func LicenseURL(repo, tag, p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = neturl.PathEscape(s)
	}
	return "https://" + rawHost + "/" + repo + "/" + neturl.PathEscape(tag) + "/" + strings.Join(segs, "/")
}
