package ci

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"debfactory/ci/runner"
	"debfactory/internal/ci/citest"
	"debfactory/internal/forge"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

func doctorRunner(t *testing.T) (*Runner, *citest.FakeGitHub, *forge.Client) {
	t.Helper()
	gh := citest.NewFakeGitHub(t)
	c, _ := forge.New(gh.URL, citest.Repo, secretToken)
	var out bytes.Buffer
	return &Runner{Root: testutil.ProjectRoot(), Cfg: testConfig(t), Out: &report.Printer{Out: &out, Err: &out}}, gh, c
}

func goodConfig(gh *citest.FakeGitHub) {
	gh.Config[""] = map[string]any{"private": false}
	gh.Config["/actions/permissions/fork-pr-contributor-approval"] = map[string]string{"approval_policy": "all_external_contributors"}
	gh.Config["/actions/permissions/workflow"] = map[string]any{"default_workflow_permissions": "read", "can_approve_pull_request_reviews": true}
	gh.Config["/rulesets"] = []map[string]any{{"id": 1}}
	gh.Config["/rulesets/1"] = map[string]any{"id": 1, "name": "main", "target": "branch", "enforcement": "active",
		"bypass_actors": []any{}, "conditions": map[string]any{"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}}},
		"rules": []map[string]any{{"type": "pull_request"}, {"type": "non_fast_forward"}, {"type": "deletion"},
			{"type": "required_status_checks", "parameters": map[string]any{
				"required_status_checks": []map[string]any{{"context": "verify", "integration_id": 15368}}}}}}
	gh.Labels = []string{"pipeline-failure"}
	pinned := parseEnv(runner.Env)["RUNNER_VERSION"]
	gh.Config["/repos/actions/runner/releases/latest"] = map[string]any{"tag_name": "v" + pinned, "published_at": time.Now().Add(-40 * 24 * time.Hour)}
}

func failing(cs []Check) []string {
	var out []string
	for _, c := range cs {
		if !c.OK && !c.Warn {
			out = append(out, c.Name)
		}
	}
	return out
}

func TestDoctorGitHub(t *testing.T) {
	ctx := context.Background()
	r, gh, c := doctorRunner(t)
	goodConfig(gh)
	if f := failing(r.GitHubChecks(ctx, c, false)); len(f) != 0 {
		t.Fatalf("configuración correcta con fallos: %v", f)
	}
	gh.Config["/actions/permissions/fork-pr-contributor-approval"] = map[string]string{"approval_policy": "first_time_contributors"}
	gh.Config["/actions/permissions/workflow"] = map[string]any{"default_workflow_permissions": "write", "can_approve_pull_request_reviews": false}
	rs := gh.Config["/rulesets/1"].(map[string]any)
	rs["bypass_actors"] = []any{map[string]any{"actor_type": "RepositoryRole"}}
	gh.Labels = nil
	gh.Config["/repos/actions/runner/releases/latest"] = map[string]any{"tag_name": "v9.999.0", "published_at": time.Now().Add(-20 * 24 * time.Hour)}
	got := strings.Join(failing(r.GitHubChecks(ctx, c, false)), "; ")
	for _, want := range []string{"aprobación de forks", "solo lectura", "create and approve", "ruleset", "etiqueta", "versión del runner"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta el fallo %q en %s", want, got)
		}
	}
	// --fix crea la etiqueta.
	r.GitHubChecks(ctx, c, true)
	if len(gh.Labels) != 1 {
		t.Error("--fix no creó la etiqueta")
	}
}

// TestDoctorRulesetVerify: un ruleset sin el check verify obligatorio no
// basta; sin él, un PR integrado antes de su check llega a main sin probar.
func TestDoctorRulesetVerify(t *testing.T) {
	ctx := context.Background()
	r, gh, c := doctorRunner(t)
	goodConfig(gh)
	rs := gh.Config["/rulesets/1"].(map[string]any)
	rs["rules"] = rs["rules"].([]map[string]any)[:3]
	if got := strings.Join(failing(r.GitHubChecks(ctx, c, false)), "; "); !strings.Contains(got, "ruleset") {
		t.Errorf("no detecta la falta del check verify: %q", got)
	}
}

// TestDoctorWatch: con watch.yml en el repositorio de paquetes, ci doctor
// comprueba el environment watch, sus secretos (por nombre), "Allow
// auto-merge" si algún paquete lo usa y el timer (specs/005, contracts/cli.md).
func TestDoctorWatch(t *testing.T) {
	ctx := context.Background()
	r, gh, c := doctorRunner(t)
	goodConfig(gh)
	if f := failing(r.GitHubChecks(ctx, c, false)); len(f) != 0 {
		t.Fatalf("sin watch.yml no debería comprobar el detector: %v", f)
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(testutil.ProjectRoot())); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, ".github/workflows"), 0o755)
	os.WriteFile(filepath.Join(root, WatchWorkflow), []byte("name: watch\n"), 0o644)
	r.Root = root
	got := strings.Join(failing(r.GitHubChecks(ctx, c, false)), "; ")
	if !strings.Contains(got, "environment watch") || !strings.Contains(got, "Allow auto-merge (lo usan bob)") {
		t.Errorf("fallos = %s", got)
	}
	gh.Config[""] = map[string]any{"private": false, "allow_auto_merge": true}
	gh.Config["/environments/watch"] = map[string]any{"name": "watch",
		"deployment_branch_policy": map[string]bool{"protected_branches": false, "custom_branch_policies": true}}
	gh.Config["/environments/watch/deployment-branch-policies"] = map[string]any{
		"branch_policies": []map[string]string{{"name": "main", "type": "branch"}}}
	gh.Config["/environments/watch/secrets"] = map[string]any{"secrets": []map[string]string{{"name": "DEBFACTORY_APP_CLIENT_ID"}}}
	if got := strings.Join(failing(r.GitHubChecks(ctx, c, false)), "; "); !strings.Contains(got, "secretos de la App") {
		t.Errorf("falta el secreto DEBFACTORY_APP_KEY sin fallo: %s", got)
	}
	gh.Config["/environments/watch/secrets"] = map[string]any{"secrets": []map[string]string{{"name": "DEBFACTORY_APP_CLIENT_ID"}, {"name": "DEBFACTORY_APP_KEY"}}}
	if f := failing(r.GitHubChecks(ctx, c, false)); len(f) != 0 {
		t.Errorf("configuración correcta con fallos: %v", f)
	}
	gh.Config["/environments/watch/deployment-branch-policies"] = map[string]any{
		"branch_policies": []map[string]string{{"name": "*", "type": "branch"}}}
	if got := strings.Join(failing(r.GitHubChecks(ctx, c, false)), "; "); !strings.Contains(got, "environment watch limitado a main") {
		t.Errorf("acepta un environment abierto a todas las ramas: %s", got)
	}
	var units []string
	h := Host{Stat: func(string) (fs.FileInfo, error) { return nil, os.ErrNotExist },
		LookupUser: func(string) (*user.User, error) { return nil, errors.New("no") },
		GroupNames: func(*user.User) ([]string, error) { return nil, nil },
		Run: func(_ string, args ...string) error {
			units = append(units, args[len(args)-1])
			return nil
		}}
	r.HostChecks(h)
	if !slices.Contains(units, "debfactory-watch.timer") {
		t.Errorf("no comprueba el timer del detector: %v", units)
	}
}

// TestDoctorPrivate: en un repositorio privado no se pide la aprobación de
// forks (no existe), sino que los forks no ejecuten workflows.
func TestDoctorPrivate(t *testing.T) {
	ctx := context.Background()
	r, gh, c := doctorRunner(t)
	goodConfig(gh)
	gh.Config[""] = map[string]any{"private": true}
	delete(gh.Config, "/actions/permissions/fork-pr-contributor-approval")
	gh.Config["/actions/permissions/fork-pr-workflows-private-repos"] = map[string]bool{
		"run_workflows_from_fork_pull_requests": false, "send_write_tokens_to_workflows": false, "send_secrets_and_variables": false}
	if f := failing(r.GitHubChecks(ctx, c, false)); len(f) != 0 {
		t.Fatalf("privado correcto con fallos: %v", f)
	}
	gh.Config["/actions/permissions/fork-pr-workflows-private-repos"] = map[string]bool{"run_workflows_from_fork_pull_requests": true}
	if got := strings.Join(failing(r.GitHubChecks(ctx, c, false)), "; "); !strings.Contains(got, "Run workflows from fork pull requests") {
		t.Errorf("no detecta los workflows de forks activados: %s", got)
	}
}

// dirEntry es un directorio de /srv/debfactory/tools para ReadDir.
type dirEntry string

func (d dirEntry) Name() string               { return string(d) }
func (d dirEntry) IsDir() bool                { return true }
func (d dirEntry) Type() fs.FileMode          { return fs.ModeDir }
func (d dirEntry) Info() (fs.FileInfo, error) { return nil, os.ErrNotExist }

type fakeInfo struct {
	fs.FileInfo
	mode fs.FileMode
}

func (f fakeInfo) Mode() fs.FileMode { return f.mode }
func (f fakeInfo) Sys() any          { return nil }

func TestDoctorHost(t *testing.T) {
	r, _, _ := doctorRunner(t)
	h := Host{
		Stat: func(p string) (fs.FileInfo, error) {
			if p == "/home/x" {
				return fakeInfo{mode: 0o755}, nil
			}
			return nil, os.ErrNotExist
		},
		LookupUser: func(name string) (*user.User, error) {
			if name == "dfci" {
				return &user.User{Username: name}, nil
			}
			return nil, errors.New("no existe")
		},
		GroupNames: func(*user.User) ([]string, error) { return []string{"dfci", "docker"}, nil },
		Run:        func(string, ...string) error { return errors.New("disabled") },
		ReadDir: func(p string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{dirEntry("bf077e169e7c201a0f300308deb2759ee5ecad02"), dirEntry(strings.Repeat("1", 40))}, nil
		},
		Home: "/home/x",
	}
	cs := r.HostChecks(h)
	got := strings.Join(failing(cs), "; ")
	for _, want := range []string{"usuario dfci sin los grupos docker", "usuario dfpub", "/home/x con permisos 700", "/srv/debfactory/data",
		"regla nftables", "debfactory-refresh.timer", "/srv/debfactory/tools", "debfactory bf077e169e7c instalada"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta el fallo %q en %s", want, got)
		}
	}
	var warned bool
	for _, c := range cs {
		if c.Warn && strings.Contains(c.Info, "111111111111") && strings.Contains(c.Info, "revisa los PRs abiertos") {
			warned = true
		}
		if c.Warn && strings.Contains(c.Info, "bf077e169e7c") {
			t.Errorf("avisa de la versión fijada como sobrante: %s", c.Info)
		}
	}
	if !warned {
		t.Errorf("sin aviso de versiones sin fijar: %+v", cs)
	}
}

// TestDoctorSite: con site.enabled, la raíz pública de ci.destination debe
// servir HTML (specs/006-repo-website, research D4).
func TestDoctorSite(t *testing.T) {
	ctx := context.Background()
	r, _, _ := doctorRunner(t)
	status, ctype := http.StatusOK, "text/html; charset=utf-8"
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ua = req.Header.Get("User-Agent")
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	d := r.Cfg.Repo.Destinations[r.Cfg.CI.Destination]
	d.PublicURL = srv.URL
	r.Cfg.Repo.Destinations[r.Cfg.CI.Destination] = d

	cs := r.SiteChecks(ctx, srv.Client())
	if len(cs) != 1 || !cs[0].OK || ua != "debfactory" {
		t.Errorf("sitio servido: %+v (User-Agent %q)", cs, ua)
	}
	status, ctype = http.StatusNotFound, "application/xml"
	cs = r.SiteChecks(ctx, srv.Client())
	if len(cs) != 1 || cs[0].OK || cs[0].Warn || !strings.Contains(cs[0].Info, "falta la regla de reescritura") {
		t.Errorf("404 de R2: %+v", cs)
	}
	r.Cfg.Site.Enabled = false
	if cs := r.SiteChecks(ctx, srv.Client()); len(cs) != 0 {
		t.Errorf("sin site.enabled no se comprueba: %+v", cs)
	}
}
