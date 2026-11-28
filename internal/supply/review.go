package supply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ModelsAPI es el endpoint de inferencia de GitHub Models.
const ModelsAPI = "https://models.github.ai/inference/chat/completions"

// Diff es la comparación determinista del fuente de dos versiones (research
// D13): lo que un revisor debe mirar primero.
type Diff struct {
	Added, Removed, Changed []string
	Binaries                []string // archivos binarios nuevos o cambiados
	BuildScripts            []string // scripts de build o instalación cambiados
	NewURLs                 []string // URLs que aparecen en la versión nueva y no en la anterior
}

var (
	urlRe       = regexp.MustCompile(`https?://[A-Za-z0-9.-]+[A-Za-z0-9/._~:?#@!$&'()*+,;=%-]*`)
	buildScript = regexp.MustCompile(`(^|/)(Makefile|makefile|GNUmakefile|CMakeLists\.txt|configure(\.ac)?|meson\.build|build\.zig(\.zon)?|build\.rs|Cargo\.toml|go\.mod|setup\.py|pyproject\.toml|package\.json|Dockerfile|install[^/]*|[^/]*\.sh|[^/]*\.(ps1|bat))$|(^|/)\.github/workflows/`)
)

// DiffTrees compara dos árboles de fuente extraídos.
func DiffTrees(oldDir, newDir string) (*Diff, error) {
	oldF, err := treeFiles(oldDir)
	if err != nil {
		return nil, err
	}
	newF, err := treeFiles(newDir)
	if err != nil {
		return nil, err
	}
	d := &Diff{}
	oldURLs := map[string]bool{}
	for p := range oldF {
		if b, err := os.ReadFile(filepath.Join(oldDir, p)); err == nil && !isBinary(b) {
			for _, u := range urlRe.FindAllString(string(b), -1) {
				oldURLs[u] = true
			}
		}
	}
	newURLs := map[string]bool{}
	for p, sum := range newF {
		prev, existed := oldF[p]
		if existed && prev == sum {
			continue
		}
		if existed {
			d.Changed = append(d.Changed, p)
		} else {
			d.Added = append(d.Added, p)
		}
		b, err := os.ReadFile(filepath.Join(newDir, p))
		if err != nil {
			return nil, err
		}
		if isBinary(b) {
			d.Binaries = append(d.Binaries, p)
		} else {
			for _, u := range urlRe.FindAllString(string(b), -1) {
				if !oldURLs[u] {
					newURLs[u] = true
				}
			}
		}
		if buildScript.MatchString(p) {
			d.BuildScripts = append(d.BuildScripts, p)
		}
	}
	for p := range oldF {
		if _, ok := newF[p]; !ok {
			d.Removed = append(d.Removed, p)
		}
	}
	for u := range newURLs {
		d.NewURLs = append(d.NewURLs, u)
	}
	for _, l := range [][]string{d.Added, d.Removed, d.Changed, d.Binaries, d.BuildScripts, d.NewURLs} {
		sort.Strings(l)
	}
	return d, nil
}

func treeFiles(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256sum(b))
		return nil
	})
	return out, err
}

func isBinary(b []byte) bool {
	n := min(len(b), 8000)
	return bytes.IndexByte(b[:n], 0) >= 0
}

// Markdown es la parte determinista del comentario (no depende de la IA).
func (d *Diff) Markdown(pkg, from, to string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Cambios del fuente de %s: %s → %s\n\n", pkg, from, to)
	fmt.Fprintf(&b, "%d archivos nuevos, %d quitados, %d cambiados.\n", len(d.Added), len(d.Removed), len(d.Changed))
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n**%s** (%d):\n", title, len(items))
		for i, it := range items {
			if i == 20 {
				fmt.Fprintf(&b, "- … y %d más\n", len(items)-20)
				break
			}
			fmt.Fprintf(&b, "- `%s`\n", it)
		}
	}
	list("Binarios nuevos o cambiados", d.Binaries)
	list("Scripts de build o instalación cambiados", d.BuildScripts)
	list("URLs nuevas", d.NewURLs)
	return b.String()
}

// Summarize pide a GitHub Models un resumen en español con "cambios
// sospechosos" a partir de la parte determinista. Sin token, sin cuota o con
// error devuelve "" y el error: el comentario dice "resumen no disponible".
func Summarize(ctx context.Context, c *http.Client, endpoint, token, model, deterministic string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("sin GITHUB_TOKEN")
	}
	if c == nil {
		c = &http.Client{Timeout: 2 * time.Minute}
	}
	if endpoint == "" {
		endpoint = ModelsAPI
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "Eres un revisor de seguridad de paquetes Debian. Responde en español, en markdown breve. " +
				"Resume el cambio entre versiones y termina con una sección «Cambios sospechosos» (endpoints de red nuevos, " +
				"código ofuscado, binarios en el fuente, cambios en scripts de build o instalación). Si no ves nada, dilo. " +
				"No inventes archivos que no estén en la lista."},
			{"role": "user", "content": deterministic},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "debfactory")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub Models: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("GitHub Models: respuesta ilegible")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// ReviewMarkdown une la parte determinista, el resumen con IA (o "resumen no
// disponible") y la puntuación de Scorecard en el comentario del PR.
func ReviewMarkdown(deterministic, summary string, sc *Scorecard, upstream string) string {
	var b strings.Builder
	b.WriteString(deterministic)
	b.WriteString("\n### Resumen asistido por IA\n\n")
	if summary == "" {
		b.WriteString("Resumen no disponible.\n")
	} else {
		b.WriteString(summary + "\n")
	}
	if sc != nil {
		b.WriteString("\n" + sc.Markdown(upstream) + "\n")
	}
	b.WriteString("\n_Señales de aviso: no bloquean la integración (FR-017, FR-018)._\n")
	return b.String()
}

func sha256sum(b []byte) [32]byte { return sha256.Sum256(b) }
