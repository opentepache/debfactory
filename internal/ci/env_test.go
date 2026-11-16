package ci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

const secretToken = "ghs_SECRETO_de_prueba_123"

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	cfg.CI.Repository = "owner/debfactory"
	return cfg
}

func writerVars() map[string]string {
	return map[string]string{
		"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/heads/main", "GITHUB_SHA": strings.Repeat("a", 40),
		"GITHUB_REPOSITORY": "owner/debfactory", "GITHUB_RUN_ID": "1234", "RUNNER_LABELS": "self-hosted,debfactory-main",
		"GITHUB_TOKEN": secretToken,
	}
}

func TestRequireWriter(t *testing.T) {
	cfg := testConfig(t)
	if err := newEnv(cfg, writerVars()).RequireWriter(cfg); err != nil {
		t.Fatalf("push a main en pub: %v", err)
	}
	cases := map[string]struct{ key, val string }{
		"nivel ci":     {"RUNNER_LABELS", "self-hosted,debfactory-pr"},
		"sin etiqueta": {"RUNNER_LABELS", ""},
		"pull_request": {"GITHUB_EVENT_NAME", "pull_request"},
		"schedule":     {"GITHUB_EVENT_NAME", "schedule"},
		"rama":         {"GITHUB_REF", "refs/heads/otra"},
		"ref de PR":    {"GITHUB_REF", "refs/pull/1/merge"},
		"otro repo":    {"GITHUB_REPOSITORY", "fork/debfactory"},
		"sin token":    {"GITHUB_TOKEN", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			v := writerVars()
			v[tc.key] = tc.val
			var env *report.EnvError
			if err := newEnv(cfg, v).RequireWriter(cfg); !errors.As(err, &env) {
				t.Fatalf("err = %v; se esperaba EnvError (código 3)", err)
			}
		})
	}
	t.Run("ci.repository vacío", func(t *testing.T) {
		c := *cfg
		c.CI.Repository = ""
		var env *report.EnvError
		if err := newEnv(&c, writerVars()).RequireWriter(&c); !errors.As(err, &env) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestInputs(t *testing.T) {
	cfg := testConfig(t)
	ok := []map[string]string{
		{},
		{"INPUT_ACTION": "refresh"},
		{"INPUT_ACTION": "restore-state-pr"},
		{"INPUT_ACTION": "rebuild", "INPUT_PACKAGE": "fnm", "INPUT_VERSION": "1.39.0-0~tepache1"},
		{"INPUT_ACTION": "retire", "INPUT_PACKAGE": "fnm", "INPUT_VERSION": "1.39.0-0~tepache1", "INPUT_REASON": "segfault en arm64", "INPUT_LAST": "true"},
	}
	for _, v := range ok {
		if _, err := newEnv(cfg, v).Inputs(); err != nil {
			t.Errorf("%v: %v", v, err)
		}
	}
	bad := map[string]map[string]string{
		"acción":            {"INPUT_ACTION": "publish"},
		"paquete con ;":     {"INPUT_ACTION": "rebuild", "INPUT_PACKAGE": "fnm;rm -rf /", "INPUT_VERSION": "1.0-0~tepache1"},
		"versión con $()":   {"INPUT_ACTION": "rebuild", "INPUT_PACKAGE": "fnm", "INPUT_VERSION": "$(id)"},
		"versión vacía":     {"INPUT_ACTION": "rebuild", "INPUT_PACKAGE": "fnm"},
		"motivo vacío":      {"INPUT_ACTION": "retire", "INPUT_PACKAGE": "fnm", "INPUT_VERSION": "1.0-0~tepache1"},
		"motivo multilínea": {"INPUT_ACTION": "retire", "INPUT_PACKAGE": "fnm", "INPUT_VERSION": "1.0-0~tepache1", "INPUT_REASON": "a\nb"},
	}
	for name, v := range bad {
		t.Run(name, func(t *testing.T) {
			var ie *InputError
			if _, err := newEnv(cfg, v).Inputs(); !errors.As(err, &ie) {
				t.Errorf("err = %v; se esperaba InputError", err)
			}
		})
	}
}

func TestEnvNeverPrintsToken(t *testing.T) {
	cfg := testConfig(t)
	e := newEnv(cfg, writerVars())
	for _, s := range []string{fmt.Sprintf("%v", e), fmt.Sprintf("%+v", e), fmt.Sprintf("%#v", e), e.String()} {
		if strings.Contains(s, secretToken) {
			t.Errorf("el token aparece en %q", s)
		}
	}
	if e.RunURL() != "https://github.com/owner/debfactory/actions/runs/1234" {
		t.Errorf("RunURL = %s", e.RunURL())
	}
}

func TestEnvFile(t *testing.T) {
	cfg := testConfig(t)
	p := filepath.Join(t.TempDir(), "env")
	os.WriteFile(p, []byte("# local\nGITHUB_EVENT_NAME=workflow_dispatch\nRUNNER_LABELS=debfactory-main\nINPUT_ACTION=refresh\n"), 0o600)
	e, err := LoadEnv(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	if e.EventName != "workflow_dispatch" || e.Trust != "pub" {
		t.Errorf("env = %v", e)
	}
	if in, _ := e.Inputs(); in.Action != "refresh" {
		t.Errorf("action = %q", in.Action)
	}
	os.WriteFile(p, []byte("sin igual\n"), 0o600)
	if _, err := LoadEnv(cfg, p); err == nil {
		t.Error("se esperaba error de formato")
	}
}
