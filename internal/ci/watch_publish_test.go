//go:build integration

package ci

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"debfactory/internal/build"
	"debfactory/internal/ci/citest"
	"debfactory/internal/fetch"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// TestWatchToPublish recorre quickstart §1 de la fase 005 con el pipeline real
// de main (gpg, aptly y el PR de estado; los gates de build e instalación
// simulados como en TestCIMain): un release estable nuevo de bob → ci watch
// abre el PR con auto-merge → el merge → ci main publica bob
// 4.4.0-0~tepache1 sin tocar los demás paquetes (SC-002 sin Docker).
func TestWatchToPublish(t *testing.T) {
	f := newMainFixture(t)
	if err := f.main(); err != nil {
		t.Fatalf("primera publicación: %v\n%s", err, f.out.String())
	}
	f.mergeStatePR()
	if err := f.main(); err != nil {
		t.Fatalf("tras el PR de estado: %v\n%s", err, f.out.String())
	}

	// Upstream publica bob 4.4.0 hace 49 h.
	now := time.Now().UTC()
	up := citest.NewUpstream(t)
	rel := citest.Release{Tag: "v4.4.0", PublishedAt: now.Add(-49 * time.Hour)}
	for _, token := range []string{"x86_64", "arm"} {
		name := "bob-linux-" + token + ".zip"
		rel.Assets = append(rel.Assets, name)
		up.Serve("https://github.com/"+bobRepo+"/releases/download/v4.4.0/"+name, bobZip("4.4.0", token))
	}
	up.Serve("https://raw.githubusercontent.com/"+bobRepo+"/v4.4.0/LICENSE", []byte(testutil.MITText))
	f.gh.SetReleases(bobRepo, citest.Release{Tag: "v4.3.0", PublishedAt: now.Add(-900 * time.Hour)}, rel)

	var out bytes.Buffer
	f.checkout()
	w := &Runner{Root: f.g.Work, Cfg: f.cfg, Out: &report.Printer{Out: &out, Err: &out},
		Env: newEnv(f.cfg, map[string]string{
			"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_REF": "refs/heads/main",
			"GITHUB_REPOSITORY": citest.Repo, "GITHUB_RUN_ID": "2000", "GITHUB_API_URL": f.gh.URL,
			"RUNNER_TEMP": t.TempDir(), "RUNNER_LABELS": "self-hosted,debfactory-pr",
			"GITHUB_TOKEN": jobToken, "DEBFACTORY_APP_TOKEN": appToken, "DEBFACTORY_APP_SLUG": appSlug}),
		Repo: &repo.Env{Root: f.g.Work, Cfg: f.cfg, Now: func() time.Time { return now }},
		Build: func() *build.Env {
			return &build.Env{Cfg: f.cfg, Fetcher: fetch.New(t.TempDir(), time.Minute, up.Transport()),
				GitHub: &fetch.GitHub{API: f.gh.URL, Token: jobToken}}
		}}
	if err := w.Watch(context.Background(), Options{}); err != nil {
		t.Fatalf("ci watch: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "watch: bob 4.3.0 → 4.4.0: pr-abierto") || !strings.Contains(out.String(), "(auto-merge)") {
		t.Fatalf("salida de ci watch:\n%s", out.String())
	}
	prs := f.gh.OpenPRs()
	if len(prs) != 1 || prs[0].Head != bobPR || !prs[0].AutoMerge {
		t.Fatalf("PRs = %+v", prs)
	}

	// GitHub integra el PR cuando verify pasa (auto-merge de la App) y el push
	// dispara main.yml.
	f.g.MergeBranch(bobPR)
	f.gh.ClosePR(prs[0].Number, true)
	if err := f.main(); err != nil {
		t.Fatalf("main tras el merge: %v\n%s", err, f.out.String())
	}
	if f.built == nil || strings.Join(f.built, ",") != "bob" {
		t.Errorf("construidos = %v; solo debía construirse bob", f.built)
	}
	st := f.state()
	var versions []string
	for _, p := range st.Packages["bob"] {
		versions = append(versions, p.Version)
	}
	if !strings.Contains(strings.Join(versions, " "), "4.4.0-0~tepache1") {
		t.Fatalf("bob publicado = %v\n%s", versions, f.out.String())
	}
	if audit := f.g.RemoteFile("debfactory/state", AuditFile); !strings.Contains(audit, `"package":"bob","version":"4.4.0-0~tepache1"`) {
		t.Errorf("la auditoría no registra bob 4.4.0:\n%s", audit)
	}
}
