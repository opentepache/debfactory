package ci

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/user"
	"strings"
	"testing"
	"time"

	"debfactory/internal/ci/citest"
	"debfactory/internal/forge"
	"debfactory/internal/report"
)

func doctorRunner(t *testing.T) (*Runner, *citest.FakeGitHub, *forge.Client) {
	t.Helper()
	gh := citest.NewFakeGitHub(t)
	c, _ := forge.New(gh.URL, citest.Repo, secretToken)
	var out bytes.Buffer
	return &Runner{Root: "../..", Cfg: testConfig(t), Out: &report.Printer{Out: &out, Err: &out}}, gh, c
}

func goodConfig(gh *citest.FakeGitHub) {
	gh.Config[""] = map[string]any{"private": false}
	gh.Config["/actions/permissions/fork-pr-contributor-approval"] = map[string]string{"approval_policy": "all_external_contributors"}
	gh.Config["/actions/permissions/workflow"] = map[string]any{"default_workflow_permissions": "read", "can_approve_pull_request_reviews": true}
	gh.Config["/rulesets"] = []map[string]any{{"id": 1}}
	gh.Config["/rulesets/1"] = map[string]any{"id": 1, "name": "main", "target": "branch", "enforcement": "active",
		"bypass_actors": []any{}, "conditions": map[string]any{"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}}},
		"rules": []map[string]string{{"type": "pull_request"}, {"type": "non_fast_forward"}, {"type": "deletion"}}}
	gh.Labels = []string{"pipeline-failure"}
	pinned := readEnvFile("../../ci/runner/runner.env")["RUNNER_VERSION"]
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
		Home:       "/home/x",
	}
	got := strings.Join(failing(r.HostChecks(h)), "; ")
	for _, want := range []string{"usuario dfci sin los grupos docker", "usuario dfpub", "/home/x con permisos 700", "/srv/debfactory/data", "regla nftables", "debfactory-refresh.timer"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta el fallo %q en %s", want, got)
		}
	}
}
