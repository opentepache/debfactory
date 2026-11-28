package ci

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"debfactory/internal/ci/citest"
	"debfactory/internal/report"
	"debfactory/internal/supply"
)

// TestRunFromEnv (T030): el run sale de GITHUB_REPOSITORY, GITHUB_WORKFLOW_REF
// y GITHUB_SHA; sin ellos, ci main --artifacts se niega.
func TestRunFromEnv(t *testing.T) {
	env := &Env{Repository: "opentepache/packages", SHA: "c0ffee",
		WorkflowRef: "opentepache/packages/.github/workflows/main.yml@refs/heads/main"}
	run, err := RunFromEnv(env)
	want := supply.Run{Repo: "opentepache/packages", Workflow: ".github/workflows/main.yml", Ref: "refs/heads/main", Commit: "c0ffee"}
	if err != nil || run != want {
		t.Fatalf("run = %+v, %v", run, err)
	}
	for _, bad := range []*Env{
		{Repository: "opentepache/packages", SHA: "c0ffee"},
		{Repository: "opentepache/packages", WorkflowRef: env.WorkflowRef},
		{Repository: "otro/repo", SHA: "c0ffee", WorkflowRef: env.WorkflowRef},
	} {
		if _, err := RunFromEnv(bad); err == nil {
			t.Errorf("%+v: se esperaba error", bad)
		}
	}
}

// TestHostedRequiresPublicRepo (FR-012): con el repositorio de paquetes
// privado, el modo hospedado se rechaza y se indica ci main --local.
func TestHostedRequiresPublicRepo(t *testing.T) {
	gh := citest.NewFakeGitHub(t)
	cfg := testConfig(t)
	cfg.CI.Repository = citest.Repo
	var out bytes.Buffer
	r := &Runner{Cfg: cfg, Out: &report.Printer{Out: &out, Err: &out},
		Env: &Env{APIURL: gh.URL, Token: "t", Repository: citest.Repo, SHA: "c0ffee", Trust: "pub", EventName: "push", Ref: "refs/heads/main",
			WorkflowRef: citest.Repo + "/.github/workflows/main.yml@refs/heads/main"}}
	gh.Config[""] = map[string]any{"private": true}
	if _, err := r.hostedAccept(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "--local") {
		t.Fatalf("privado: %v", err)
	}
	gh.Config[""] = map[string]any{"private": false}
	a, err := r.hostedAccept(context.Background(), t.TempDir())
	if err != nil || a.Run.Workflow != ".github/workflows/main.yml" || a.Run.Commit != "c0ffee" {
		t.Fatalf("público: %+v, %v", a, err)
	}
	if err := r.Main(context.Background(), Options{Artifacts: "x", Local: true}); err == nil || !strings.Contains(err.Error(), "no se combinan") {
		t.Errorf("--artifacts con --local: %v", err)
	}
}
