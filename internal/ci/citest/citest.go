// Package citest da un GitHub falso (httptest) y un repositorio git de
// prueba para las pruebas de internal/ci. Ningún test habla con github.com.
package citest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"debfactory/internal/testutil"
)

// Repo es el repositorio que simula FakeGitHub.
const Repo = "owner/debfactory"

// Issue es un issue del servidor falso.
type Issue struct {
	Number    int
	Title     string
	Body      string
	State     string
	Labels    []string
	Comments  []string
	CreatedAt time.Time
}

// PR es un pull request del servidor falso.
type PR struct {
	Number    int
	Head      string
	Base      string
	Title     string
	Body      string
	State     string // open | closed
	Merged    bool
	CreatedAt time.Time
	Comments  []string
	AutoMerge bool // auto-merge activado por GraphQL (fase 005)
}

// Release es un release de un repositorio upstream simulado (fase 005).
type Release struct {
	Tag         string
	Draft       bool
	Prerelease  bool
	PublishedAt time.Time
	Assets      []string
	Digests     map[string]string // asset -> sha256 (campo digest de la API)
}

// FakeGitHub es un servidor con estado en memoria para issues, PRs, labels,
// la configuración de Actions y dispatch. Registra cada petición.
type FakeGitHub struct {
	*httptest.Server
	mu       sync.Mutex
	next     int
	Issues   map[int]*Issue
	PRs      map[int]*PR
	Labels   []string
	Requests []string // "MÉTODO ruta"
	Bodies   []string // cuerpos recibidos (para buscar secretos)
	Config   map[string]any
	Dispatch []map[string]any
	Statuses map[string]map[string]string // sha → context → state
	Fail     bool                         // si es true, todas las respuestas son 500

	// Releases de repositorios upstream ("owner/repo"), para el detector de
	// versiones (fase 005). NotModified cuenta las respuestas 304.
	Releases    map[string][]Release
	NotModified int
	// Límite de peticiones simulado: solo para las peticiones con RateToken;
	// cada una (salvo los 304) gasta una de RateRemaining y, a 0, la API
	// responde 403 de límite. RateToken vacío = sin límite.
	RateToken     string
	RateRemaining int
	// AutoMergeDisallowed simula "Allow auto-merge" desactivado.
	AutoMergeDisallowed bool
	// FailPrefix: las peticiones cuya ruta empieza así responden 500.
	FailPrefix string
}

var (
	issueRe   = regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues/(\d+)$`)
	commentRe = regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues/(\d+)/comments$`)
	pullRe    = regexp.MustCompile(`^/repos/[^/]+/[^/]+/pulls/(\d+)$`)
	labelRe   = regexp.MustCompile(`^/repos/[^/]+/[^/]+/labels/([^/]+)$`)
	statusRe  = regexp.MustCompile(`^/repos/[^/]+/[^/]+/statuses/([0-9a-f]{40})$`)
	// Fase 005.
	releasesRe   = regexp.MustCompile(`^/repos/([^/]+/[^/]+)/releases$`)
	releaseTagRe = regexp.MustCompile(`^/repos/([^/]+/[^/]+)/releases/tags/([^/]+)$`)
	userRe       = regexp.MustCompile(`^/users/([^/]+)$`)
)

// NewFakeGitHub arranca el servidor; se cierra al terminar el test.
func NewFakeGitHub(t *testing.T) *FakeGitHub {
	t.Helper()
	f := &FakeGitHub{next: 1, Issues: map[int]*Issue{}, PRs: map[int]*PR{}, Config: map[string]any{},
		Statuses: map[string]map[string]string{}, Releases: map[string][]Release{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

func (f *FakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	f.Requests = append(f.Requests, r.Method+" "+r.URL.Path)
	f.Bodies = append(f.Bodies, string(body))
	if r.Header.Get("User-Agent") != "debfactory" {
		http.Error(w, `{"message":"falta User-Agent: debfactory"}`, 400)
		return
	}
	if f.Fail || (f.FailPrefix != "" && strings.HasPrefix(r.URL.Path, f.FailPrefix)) {
		http.Error(w, `{"message":"fallo simulado"}`, 500)
		return
	}
	var in map[string]any
	json.Unmarshal(body, &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	base := "/repos/" + Repo
	p := r.URL.Path
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if v != nil {
			json.NewEncoder(w).Encode(v)
		}
	}
	var relBody []byte
	notModified := false
	if m := releasesRe.FindStringSubmatch(p); m != nil && r.Method == "GET" && m[1] != Repo {
		var etag string
		relBody, etag = f.releasesJSON(m[1])
		w.Header().Set("ETag", etag)
		notModified = r.Header.Get("If-None-Match") == etag
	}
	if f.RateToken != "" && r.Header.Get("Authorization") == "Bearer "+f.RateToken {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		if f.RateRemaining <= 0 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			reply(403, map[string]string{"message": "API rate limit exceeded for installation"})
			return
		}
		if !notModified {
			f.RateRemaining--
		}
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(f.RateRemaining))
	}
	switch {
	case relBody != nil:
		if notModified {
			f.NotModified++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(relBody)
	case r.Method == "GET" && releaseTagRe.MatchString(p):
		m := releaseTagRe.FindStringSubmatch(p)
		tag, _ := neturl.PathUnescape(m[2])
		for _, rel := range f.Releases[m[1]] {
			if rel.Tag == tag {
				reply(200, releaseJSON(m[1], rel))
				return
			}
		}
		reply(404, map[string]string{"message": "Not Found"})
	case r.Method == "GET" && userRe.MatchString(p):
		reply(200, map[string]any{"login": userRe.FindStringSubmatch(p)[1], "id": 4242})
	case r.Method == "POST" && p == "/graphql":
		q, _ := in["query"].(string)
		vars, _ := in["variables"].(map[string]any)
		id, _ := vars["id"].(string)
		if f.AutoMergeDisallowed {
			reply(200, map[string]any{"errors": []map[string]string{{"message": "Pull request Auto merge is not allowed for this repository"}}})
			return
		}
		for _, pr := range f.PRs {
			if fmt.Sprintf("PR_%d", pr.Number) == id {
				pr.AutoMerge = strings.Contains(q, "enablePullRequestAutoMerge")
				reply(200, map[string]any{"data": map[string]any{}})
				return
			}
		}
		reply(200, map[string]any{"errors": []map[string]string{{"message": "Could not resolve to a node with the global id of '" + id + "'"}}})
	case r.Method == "GET" && commentRe.MatchString(p):
		n, _ := strconv.Atoi(commentRe.FindStringSubmatch(p)[1])
		var bodies []string
		if is, ok := f.Issues[n]; ok {
			bodies = is.Comments
		} else if pr, ok := f.PRs[n]; ok {
			bodies = pr.Comments
		}
		out := []map[string]any{}
		for _, b := range bodies {
			out = append(out, map[string]any{"body": b, "user": map[string]string{"login": "bot"}})
		}
		reply(200, out)
	case r.Method == "GET" && p == base+"/issues":
		label := r.URL.Query().Get("labels")
		var all []map[string]any
		for _, n := range f.sortedIssues() {
			is := f.Issues[n]
			if is.State == "open" && (label == "" || slices.Contains(is.Labels, label)) {
				all = append(all, f.issueJSON(is))
			}
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		page = max(page, 1)
		lo, hi := (page-1)*100, min(page*100, len(all))
		if lo > len(all) {
			lo = len(all)
		}
		if hi < len(all) {
			q := r.URL.Query()
			q.Set("page", strconv.Itoa(page+1))
			w.Header().Set("Link", fmt.Sprintf(`<%s%s?%s>; rel="next"`, f.URL, p, q.Encode()))
		}
		reply(200, all[lo:hi])
	case r.Method == "POST" && p == base+"/issues":
		is := &Issue{Number: f.next, Title: str("title"), Body: str("body"), State: "open", CreatedAt: time.Now().UTC()}
		if ls, ok := in["labels"].([]any); ok {
			for _, l := range ls {
				is.Labels = append(is.Labels, l.(string))
			}
		}
		f.next++
		f.Issues[is.Number] = is
		reply(201, f.issueJSON(is))
	case r.Method == "POST" && commentRe.MatchString(p):
		n, _ := strconv.Atoi(commentRe.FindStringSubmatch(p)[1])
		if is, ok := f.Issues[n]; ok {
			is.Comments = append(is.Comments, str("body"))
		} else if pr, ok := f.PRs[n]; ok {
			pr.Comments = append(pr.Comments, str("body"))
		} else {
			reply(404, map[string]string{"message": "Not Found"})
			return
		}
		reply(201, map[string]any{"id": 1})
	case r.Method == "PATCH" && issueRe.MatchString(p):
		n, _ := strconv.Atoi(issueRe.FindStringSubmatch(p)[1])
		is, ok := f.Issues[n]
		if !ok {
			reply(404, map[string]string{"message": "Not Found"})
			return
		}
		if s := str("state"); s != "" {
			is.State = s
		}
		reply(200, f.issueJSON(is))
	case r.Method == "GET" && p == base+"/pulls":
		head := r.URL.Query().Get("head")
		var out []map[string]any
		for _, n := range f.sortedPRs() {
			pr := f.PRs[n]
			if pr.State == "open" && (head == "" || head == "owner:"+pr.Head) {
				out = append(out, prJSON(pr))
			}
		}
		reply(200, out)
	case r.Method == "POST" && p == base+"/pulls":
		for _, pr := range f.PRs {
			if pr.State == "open" && pr.Head == str("head") {
				reply(422, map[string]string{"message": "A pull request already exists"})
				return
			}
		}
		pr := &PR{Number: f.next, Head: str("head"), Base: str("base"), Title: str("title"), Body: str("body"),
			State: "open", CreatedAt: time.Now().UTC()}
		f.next++
		f.PRs[pr.Number] = pr
		reply(201, prJSON(pr))
	case pullRe.MatchString(p):
		n, _ := strconv.Atoi(pullRe.FindStringSubmatch(p)[1])
		pr, ok := f.PRs[n]
		if !ok {
			reply(404, map[string]string{"message": "Not Found"})
			return
		}
		if r.Method == "PATCH" {
			if s := str("title"); s != "" {
				pr.Title = s
			}
			if s := str("body"); s != "" {
				pr.Body = s
			}
			if str("state") == "closed" {
				pr.State, pr.AutoMerge = "closed", false
			}
		}
		reply(200, prJSON(pr))
	case r.Method == "GET" && labelRe.MatchString(p):
		name := labelRe.FindStringSubmatch(p)[1]
		if !slices.Contains(f.Labels, name) {
			reply(404, map[string]string{"message": "Not Found"})
			return
		}
		reply(200, map[string]string{"name": name})
	case r.Method == "POST" && p == base+"/labels":
		f.Labels = append(f.Labels, str("name"))
		reply(201, map[string]string{"name": str("name")})
	case r.Method == "POST" && statusRe.MatchString(p):
		sha := statusRe.FindStringSubmatch(p)[1]
		if f.Statuses[sha] == nil {
			f.Statuses[sha] = map[string]string{}
		}
		f.Statuses[sha][str("context")] = str("state")
		reply(201, map[string]string{"state": str("state"), "context": str("context")})
	case r.Method == "POST" && strings.HasSuffix(p, "/dispatches"):
		f.Dispatch = append(f.Dispatch, in)
		reply(204, nil)
	default:
		key := strings.TrimPrefix(p, base)
		if strings.HasPrefix(p, "/repos/actions/runner/") {
			key = p
		}
		if v, ok := f.Config[key]; ok {
			reply(200, v)
			return
		}
		reply(404, map[string]string{"message": "Not Found: " + p})
	}
}

func (f *FakeGitHub) sortedIssues() []int {
	var ns []int
	for n := range f.Issues {
		ns = append(ns, n)
	}
	slices.Sort(ns)
	return ns
}

func (f *FakeGitHub) sortedPRs() []int {
	var ns []int
	for n := range f.PRs {
		ns = append(ns, n)
	}
	slices.Sort(ns)
	return ns
}

func (f *FakeGitHub) issueJSON(is *Issue) map[string]any {
	var labels []map[string]string
	for _, l := range is.Labels {
		labels = append(labels, map[string]string{"name": l})
	}
	return map[string]any{"number": is.Number, "title": is.Title, "body": is.Body, "state": is.State,
		"labels": labels, "html_url": fmt.Sprintf("https://github.com/%s/issues/%d", Repo, is.Number),
		"created_at": is.CreatedAt.Format(time.RFC3339)}
}

func prJSON(pr *PR) map[string]any {
	out := map[string]any{"number": pr.Number, "state": pr.State, "title": pr.Title, "body": pr.Body,
		"merged": pr.Merged, "created_at": pr.CreatedAt.Format(time.RFC3339),
		"html_url": fmt.Sprintf("https://github.com/%s/pull/%d", Repo, pr.Number),
		"head":     map[string]string{"ref": pr.Head}, "node_id": fmt.Sprintf("PR_%d", pr.Number)}
	if pr.AutoMerge {
		out["auto_merge"] = map[string]string{"merge_method": "merge"}
	}
	return out
}

func releaseJSON(repo string, r Release) map[string]any {
	assets := []map[string]string{}
	for _, a := range r.Assets {
		asset := map[string]string{"name": a}
		if d := r.Digests[a]; d != "" {
			asset["digest"] = "sha256:" + d
		}
		assets = append(assets, asset)
	}
	return map[string]any{"tag_name": r.Tag, "draft": r.Draft, "prerelease": r.Prerelease,
		"published_at": r.PublishedAt.UTC().Format(time.RFC3339),
		"html_url":     "https://github.com/" + repo + "/releases/tag/" + r.Tag, "assets": assets}
}

// releasesJSON es la respuesta de /releases (más reciente primero) y su ETag.
func (f *FakeGitHub) releasesJSON(repo string) ([]byte, string) {
	rs := slices.Clone(f.Releases[repo])
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].PublishedAt.After(rs[j].PublishedAt) })
	out := []map[string]any{}
	for _, r := range rs {
		out = append(out, releaseJSON(repo, r))
	}
	b, _ := json.Marshal(out)
	sum := sha256.Sum256(b)
	return b, `"` + hex.EncodeToString(sum[:8]) + `"`
}

// SetReleases sustituye los releases de un repositorio upstream.
func (f *FakeGitHub) SetReleases(repo string, rs ...Release) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Releases[repo] = rs
}

// SetRate fija el límite de peticiones simulado para un token.
func (f *FakeGitHub) SetRate(token string, remaining int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RateToken, f.RateRemaining = token, remaining
}

// GetPR devuelve una copia de un PR por número.
func (f *FakeGitHub) GetPR(n int) PR {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.PRs[n]
}

// SetIssueCreated cambia la fecha de creación de un issue.
func (f *FakeGitHub) SetIssueCreated(n int, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Issues[n].CreatedAt = t
}

// CountRequests cuenta las peticiones registradas que empiezan por prefix
// (p. ej. "GET /repos/o/r/releases").
func (f *FakeGitHub) CountRequests(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.Requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// ResetRequests vacía el registro de peticiones.
func (f *FakeGitHub) ResetRequests() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests, f.Bodies, f.NotModified = nil, nil, 0
}

// Upstream es un servidor HTTPS que sirve los archivos de releases
// simulados (assets en github.com/<repo>/releases/download/<tag>/ y textos en
// raw.githubusercontent.com/<repo>/<tag>/) a través de Transport.
type Upstream struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte // host+ruta -> contenido
	hits  map[string]int
}

// NewUpstream arranca el servidor; se cierra al terminar el test.
func NewUpstream(t *testing.T) *Upstream {
	t.Helper()
	u := &Upstream{files: map[string][]byte{}, hits: map[string]int{}}
	u.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		b, ok := u.files[r.Host+r.URL.Path]
		u.hits[r.Host+r.URL.Path]++
		u.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(u.Close)
	return u
}

// Serve publica data en una URL https.
func (u *Upstream) Serve(url string, data []byte) {
	p, err := neturl.Parse(url)
	if err != nil {
		panic(err)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.files[p.Host+p.Path] = data
}

// Remove deja de servir una URL.
func (u *Upstream) Remove(url string) {
	p, _ := neturl.Parse(url)
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.files, p.Host+p.Path)
}

// Downloads es el número total de descargas servidas.
func (u *Upstream) Downloads() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, h := range u.hits {
		n += h
	}
	return n
}

// Transport envía al servidor de prueba las peticiones a cualquier host.
func (u *Upstream) Transport() http.RoundTripper { return testutil.Redirect{Server: u.Server} }

// OpenIssues devuelve los issues abiertos.
func (f *FakeGitHub) OpenIssues() []*Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*Issue
	for _, n := range f.sortedIssues() {
		if f.Issues[n].State == "open" {
			out = append(out, f.Issues[n])
		}
	}
	return out
}

// OpenPRs devuelve los PR abiertos.
func (f *FakeGitHub) OpenPRs() []*PR {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*PR
	for _, n := range f.sortedPRs() {
		if f.PRs[n].State == "open" {
			out = append(out, f.PRs[n])
		}
	}
	return out
}

// ClosePR cierra un PR (merged indica si se integró).
func (f *FakeGitHub) ClosePR(n int, merged bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PRs[n].State, f.PRs[n].Merged = "closed", merged
}

// SetPRCreated cambia la fecha de creación de un PR (FR-007c).
func (f *FakeGitHub) SetPRCreated(n int, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PRs[n].CreatedAt = t
}

// AllBodies devuelve todo lo que recibió el servidor.
func (f *FakeGitHub) AllBodies() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.Bodies, "\n")
}

// Git es un remoto bare y un clon de trabajo con main.
type Git struct {
	Remote string // repositorio bare (origin)
	Work   string // clon de trabajo
	t      *testing.T
}

// GitFixture crea un remoto bare con un main que contiene los archivos de
// files (ruta relativa -> contenido) y un clon de trabajo con origin.
func GitFixture(t *testing.T, files map[string]string) *Git {
	t.Helper()
	dir := t.TempDir()
	g := &Git{Remote: filepath.Join(dir, "remote.git"), Work: filepath.Join(dir, "work"), t: t}
	g.run(dir, "init", "-q", "--bare", "-b", "main", g.Remote)
	g.run(dir, "clone", "-q", g.Remote, g.Work)
	for p, c := range files {
		g.Write(p, c)
	}
	g.Commit("inicial")
	g.run(g.Work, "push", "-q", "origin", "HEAD:refs/heads/main")
	return g
}

func (g *Git) run(dir string, args ...string) string {
	g.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=t@t", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Git ejecuta git en el clon de trabajo.
func (g *Git) Git(args ...string) string { g.t.Helper(); return g.run(g.Work, args...) }

// Write escribe un archivo en el clon de trabajo.
func (g *Git) Write(path, content string) {
	g.t.Helper()
	p := filepath.Join(g.Work, path)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

// Commit confirma todo y devuelve el sha.
func (g *Git) Commit(msg string) string {
	g.t.Helper()
	g.Git("add", "-A")
	g.Git("commit", "-q", "--allow-empty", "-m", msg)
	return g.Git("rev-parse", "HEAD")
}

// Push empuja HEAD a main.
func (g *Git) Push() { g.t.Helper(); g.Git("push", "-q", "origin", "HEAD:refs/heads/main") }

// RemoteFile lee un archivo de una rama del remoto ("" si no existe).
func (g *Git) RemoteFile(branch, path string) string {
	cmd := exec.Command("git", "--git-dir", g.Remote, "show", branch+":"+path)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// RemoteBranch devuelve el sha de una rama del remoto ("" si no existe).
func (g *Git) RemoteBranch(branch string) string {
	out, err := exec.Command("git", "--git-dir", g.Remote, "rev-parse", "--verify", "-q", "refs/heads/"+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Clone hace otro clon de trabajo del remoto (el del mantenedor), separado
// del checkout que usa el pipeline.
func (g *Git) Clone() *Git {
	g.t.Helper()
	c := &Git{Remote: g.Remote, Work: filepath.Join(g.t.TempDir(), "clone"), t: g.t}
	c.run(filepath.Dir(c.Work), "clone", "-q", g.Remote, c.Work)
	return c
}

// MergeBranch integra una rama del remoto en main del remoto (como el merge
// del mantenedor), desde un clon aparte, y borra la rama.
func (g *Git) MergeBranch(branch string) {
	g.t.Helper()
	c := g.Clone()
	c.Git("merge", "-q", "--no-ff", "-m", "merge "+branch, "origin/"+branch)
	c.Push()
	c.Git("push", "-q", "origin", "--delete", branch)
}
