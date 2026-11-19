package ci

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/build"
	"debfactory/internal/ci/citest"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

// Pruebas de `ci watch` (specs/005-upstream-release-watch): un repositorio git
// de prueba, el GitHub falso de citest y un upstream simulado de bob servido
// por HTTPS. Sin Docker ni red.

const (
	appToken = "ghs_APP_secreto_de_prueba_123"
	jobToken = "ghs_JOB_secreto_de_prueba_456"
	bobRepo  = "MordechaiHadad/bob"
	bobPR    = "debfactory/update/bob"
	appSlug  = "opentepache-updates"
)

type watchFixture struct {
	t     *testing.T
	g     *citest.Git
	gh    *citest.FakeGitHub
	up    *citest.Upstream
	cfg   *config.Config
	now   time.Time
	out   bytes.Buffer
	temp  string // RUNNER_TEMP: persiste entre ciclos, como la caché de Actions
	cache string
	vars  map[string]string
	rels  map[string][]citest.Release
}

// newWatchFixture crea main con la configuración de prueba y los paquetes
// dados (por defecto bob, publicado en 4.3.0).
func newWatchFixture(t *testing.T, pkgs ...string) *watchFixture {
	t.Helper()
	if len(pkgs) == 0 {
		pkgs = []string{"bob"}
	}
	files := map[string]string{"README.md": "x\n"}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	files[config.MainFile], files[config.LicensesFile] = read(config.MainFile), read(config.LicensesFile)
	for _, p := range pkgs {
		files[manifest.Path(p)], files[lock.Path(p)] = read(manifest.Path(p)), read(lock.Path(p))
	}
	f := &watchFixture{t: t, g: citest.GitFixture(t, files), gh: citest.NewFakeGitHub(t), up: citest.NewUpstream(t),
		now: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), temp: t.TempDir(), cache: t.TempDir(),
		rels: map[string][]citest.Release{}}
	cfg, err := config.Load(f.g.Work)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CI.Repository = citest.Repo
	f.cfg = cfg
	f.vars = map[string]string{
		"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_REF": "refs/heads/main",
		"GITHUB_REPOSITORY": citest.Repo, "GITHUB_RUN_ID": "77", "GITHUB_SHA": "0123456789abcdef",
		"GITHUB_API_URL": f.gh.URL, "RUNNER_TEMP": f.temp, "RUNNER_LABELS": "self-hosted,debfactory-pr",
		"GITHUB_TOKEN": jobToken, "DEBFACTORY_APP_TOKEN": appToken, "DEBFACTORY_APP_SLUG": appSlug,
	}
	f.release(bobRepo, "4.3.0", 24*30, nil)
	return f
}

func bobZip(version, token string) []byte {
	return testutil.Zip([]testutil.Entry{{Name: "bob-linux-" + token + "/bob", Body: []byte("bob " + version + " " + token + "\n"), Mode: 0o755}})
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// release publica una versión de un upstream hace hoursAgo horas, con sus
// assets de bob y su LICENSE (MIT), y la añade a la API.
func (f *watchFixture) release(repoName, version string, hoursAgo int, mod func(*citest.Release)) {
	f.t.Helper()
	tag := "v" + version
	r := citest.Release{Tag: tag, PublishedAt: f.now.Add(-time.Duration(hoursAgo) * time.Hour)}
	for _, token := range []string{"x86_64", "arm"} {
		name := "bob-linux-" + token + ".zip"
		r.Assets = append(r.Assets, name)
		f.up.Serve("https://github.com/"+repoName+"/releases/download/"+tag+"/"+name, bobZip(version, token))
	}
	f.up.Serve("https://raw.githubusercontent.com/"+repoName+"/"+tag+"/LICENSE", []byte(testutil.MITText))
	if mod != nil {
		mod(&r)
	}
	f.rels[repoName] = append(f.rels[repoName], r)
	f.gh.SetReleases(repoName, f.rels[repoName]...)
}

// retract quita un release de la API.
func (f *watchFixture) retract(repoName, version string) {
	var keep []citest.Release
	for _, r := range f.rels[repoName] {
		if r.Tag != "v"+version {
			keep = append(keep, r)
		}
	}
	f.rels[repoName] = keep
	f.gh.SetReleases(repoName, keep...)
}

func (f *watchFixture) runner() *Runner {
	return &Runner{Root: f.g.Work, Cfg: f.cfg, Env: newEnv(f.cfg, f.vars), Out: &report.Printer{Out: &f.out, Err: &f.out},
		Repo: &repo.Env{Root: f.g.Work, Cfg: f.cfg, Now: func() time.Time { return f.now }},
		Build: func() *build.Env {
			return &build.Env{Cfg: f.cfg, Fetcher: fetch.New(f.cache, time.Minute, f.up.Transport()),
				GitHub: &fetch.GitHub{API: f.gh.URL, Token: jobToken}}
		}}
}

// cycle ejecuta un ciclo sobre un checkout nuevo de main (como watch.yml).
func (f *watchFixture) cycle(o Options) error {
	f.t.Helper()
	f.g.Git("fetch", "-q", "origin")
	f.g.Git("reset", "-q", "--hard", "origin/main")
	f.out.Reset()
	err := f.runner().Watch(context.Background(), o)
	if strings.Contains(f.out.String(), appToken) || strings.Contains(f.gh.AllBodies(), appToken) ||
		strings.Contains(f.gh.AllBodies(), jobToken) {
		f.t.Fatal("un token aparece en la salida o en la API")
	}
	return err
}

func (f *watchFixture) mustCycle(o Options) {
	f.t.Helper()
	if err := f.cycle(o); err != nil {
		f.t.Fatalf("ciclo: %v\n%s", err, f.out.String())
	}
}

// commitMain cambia un archivo de main como lo haría el mantenedor.
func (f *watchFixture) commitMain(rel string, edit func(string) string) {
	f.t.Helper()
	c := f.g.Clone()
	data, err := os.ReadFile(filepath.Join(c.Work, rel))
	if err != nil {
		f.t.Fatal(err)
	}
	c.Write(rel, edit(string(data)))
	c.Commit("mantenedor: " + rel)
	c.Push()
}

// remoteGit ejecuta git sobre el remoto bare.
func (f *watchFixture) remoteGit(args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"--git-dir", f.g.Remote}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (f *watchFixture) openPR() *citest.PR {
	f.t.Helper()
	prs := f.gh.OpenPRs()
	if len(prs) != 1 {
		f.t.Fatalf("PRs abiertos = %d; se esperaba 1\n%s", len(prs), f.out.String())
	}
	return prs[0]
}

func (f *watchFixture) writes() []string {
	var out []string
	for _, r := range f.gh.Requests {
		if !strings.HasPrefix(r, "GET ") {
			out = append(out, r)
		}
	}
	return out
}

func (f *watchFixture) wantOut(subs ...string) {
	f.t.Helper()
	for _, s := range subs {
		if !strings.Contains(f.out.String(), s) {
			f.t.Errorf("falta %q en la salida:\n%s", s, f.out.String())
		}
	}
}

// TestWatchProposesNewVersion: US1 escenario 1, SC-001, FR-006–FR-010, FR-022.
func TestWatchProposesNewVersion(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 49, nil)
	f.mustCycle(Options{})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-abierto #1 (auto-merge)", "watch: consultas de releases 1 (0 × 304)")

	pr := f.openPR()
	if pr.Title != "bob: 4.3.0 → 4.4.0" || pr.Head != bobPR || pr.Base != "main" || !pr.AutoMerge {
		t.Errorf("PR = %+v", pr)
	}
	for _, s := range []string{"<!-- debfactory-watch pkg=bob version=4.4.0 -->", "| Anterior | 4.3.0 (`4.3.0-0~tepache1`) |",
		"| Nueva | 4.4.0 → `4.4.0-0~tepache1` |", "https://github.com/" + bobRepo + "/releases/tag/v4.4.0",
		"2026-11-18 11:00 UTC (espera de 48 h cumplida)", "| Licencia | MIT, sin cambios |", "| Cambio mayor | no |",
		"| Auto-merge | sí: se integra solo cuando `verify` pase |"} {
		if !strings.Contains(pr.Body, s) {
			t.Errorf("falta %q en el cuerpo:\n%s", s, pr.Body)
		}
	}
	// Solo cambian versión, revisión y lockfile, en un commit de la App.
	man := f.g.RemoteFile(bobPR, "packages/bob.yaml")
	if !strings.Contains(man, "\nversion: 4.4.0\n") || !strings.Contains(man, "\nrevision: 1\n") {
		t.Errorf("manifiesto de la rama:\n%s", man)
	}
	if diff, _ := f.remoteGit("diff", "--name-only", "main", bobPR); diff != "packages/bob.lock.yaml\npackages/bob.yaml" {
		t.Errorf("archivos cambiados = %q", diff)
	}
	if author, _ := f.remoteGit("log", "-1", "--format=%an <%ae>", bobPR); author != appSlug+"[bot] <4242+"+appSlug+"[bot]@users.noreply.github.com>" {
		t.Errorf("autor = %q", author)
	}
	lk, err := lock.Parse([]byte(f.g.RemoteFile(bobPR, "packages/bob.lock.yaml")), "packages/bob.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	e := lk.Versions["4.4.0"]
	if e == nil || lk.Versions["4.3.0"] == nil || e.Artifacts["amd64"] != sha(bobZip("4.4.0", "x86_64")) ||
		e.Artifacts["arm64"] != sha(bobZip("4.4.0", "arm")) || e.LicenseSHA256 != sha([]byte(testutil.MITText)) ||
		e.SourceDateEpoch != f.now.Add(-49*time.Hour).Unix() {
		t.Errorf("lockfile de la rama = %+v", lk.Versions)
	}

	// Otro ciclo sin novedades: 304, ningún cambio (FR-022).
	f.gh.ResetRequests()
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-al-día #1", "watch: consultas de releases 1 (1 × 304)")
	if w := f.writes(); len(w) != 0 {
		t.Errorf("un ciclo sin novedades escribió: %v", w)
	}
}

// TestWatchInterval: FR-001 (intervalo de la configuración) y FR-002 (ciclo a
// mano con --force o --package).
func TestWatchInterval(t *testing.T) {
	f := newWatchFixture(t)
	f.mustCycle(Options{})
	f.wantOut("watch: bob 4.3.0: sin-cambios")
	f.gh.ResetRequests()
	f.now = f.now.Add(23 * time.Hour)
	f.mustCycle(Options{})
	f.wantOut("watch: ciclo no vencido (último ciclo completo 2026-11-20 12:00 UTC; el siguiente a partir de 2026-11-21 12:00 UTC)")
	if len(f.gh.Requests) != 0 {
		t.Errorf("un ciclo no vencido hizo peticiones: %v", f.gh.Requests)
	}
	f.mustCycle(Options{Package: "bob"})
	f.wantOut("watch: bob 4.3.0: sin-cambios")
	f.now = f.now.Add(2 * time.Hour)
	f.mustCycle(Options{})
	f.wantOut("watch: bob 4.3.0: sin-cambios")

	// Un fallo global (la API caída) no cuenta como ciclo completo: se repite
	// en el disparo siguiente.
	f.gh.Fail = true
	f.now = f.now.Add(25 * time.Hour)
	if err := f.cycle(Options{}); err == nil {
		t.Fatal("un ciclo con la API caída debería salir con error")
	}
	f.gh.Fail = false
	f.now = f.now.Add(time.Hour)
	f.mustCycle(Options{})
	f.wantOut("watch: bob 4.3.0: sin-cambios")
	if is := f.gh.OpenIssues(); len(is) != 0 {
		t.Errorf("el aviso del fallo del ciclo no se cerró: %+v", is)
	}

	// Un error de un paquete (su upstream falla) sí cuenta como completo: se
	// notifica y no se repite cada hora.
	f.gh.FailPrefix = "/repos/" + bobRepo + "/releases"
	f.now = f.now.Add(25 * time.Hour)
	if err := f.cycle(Options{}); err == nil || !strings.Contains(f.out.String(), "watch: bob 4.3.0: error al consultar los releases") {
		t.Fatalf("error = %v\n%s", err, f.out.String())
	}
	if is := f.gh.OpenIssues(); len(is) != 1 || is[0].Title != "fallo: pipeline - en watch" {
		t.Fatalf("issues = %+v", is)
	}
	f.now = f.now.Add(time.Hour)
	f.mustCycle(Options{})
	f.wantOut("watch: ciclo no vencido")
}

// TestWatchWaiting: la espera de 48 h (FR-004a), también a mano.
func TestWatchWaiting(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 10, nil)
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: en-espera (elegible 2026-11-22 02:00 UTC)")
	if len(f.gh.OpenPRs()) != 0 || len(f.writes()) != 0 {
		t.Fatalf("PR o escrituras durante la espera: %v", f.writes())
	}
	// US1 escenario 5: 4.4.0 elegible y 4.4.1 en espera → 4.4.0; luego 4.4.1.
	f.now = f.now.Add(40 * time.Hour)
	f.release(bobRepo, "4.4.1", 5, nil)
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-abierto #1")
	f.now = f.now.Add(48 * time.Hour)
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.1: pr-actualizado #1")
	if pr := f.openPR(); pr.Title != "bob: 4.3.0 → 4.4.1" {
		t.Errorf("PR = %q", pr.Title)
	}
}

// TestWatchOnePRPerPackage: US1 escenarios 2 y 3, FR-008.
func TestWatchOnePRPerPackage(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 100, nil)
	f.release(bobRepo, "4.5.0", 60, nil)
	f.mustCycle(Options{})
	f.wantOut("watch: bob 4.3.0 → 4.5.0: pr-abierto #1")
	f.release(bobRepo, "4.6.0", 50, nil)
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.6.0: pr-actualizado #1")
	if pr := f.openPR(); pr.Title != "bob: 4.3.0 → 4.6.0" || !strings.Contains(pr.Body, "version=4.6.0") {
		t.Errorf("PR = %q", pr.Title)
	}
	if !strings.Contains(f.g.RemoteFile(bobPR, "packages/bob.yaml"), "version: 4.6.0") {
		t.Error("la rama no pasó a 4.6.0")
	}
}

// TestWatchForeignCommits: FR-009, un comentario por versión.
func TestWatchForeignCommits(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 60, nil)
	f.mustCycle(Options{})
	c := f.g.Clone()
	c.Git("checkout", "-q", "-b", "fix", "origin/"+bobPR)
	c.Write("packages/bob.yaml", strings.Replace(c.RemoteFile(bobPR, "packages/bob.yaml"), "summary: version manager for Neovim", "summary: Neovim version manager", 1))
	c.Commit("ajuste del mantenedor")
	c.Git("push", "-q", "origin", "HEAD:refs/heads/"+bobPR)
	tip := f.g.RemoteBranch(bobPR)
	f.release(bobRepo, "4.5.0", 50, nil)
	for range 2 {
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: bob 4.3.0 → 4.5.0: comentado #1 (la rama tiene commits ajenos)")
	}
	if f.g.RemoteBranch(bobPR) != tip {
		t.Error("el detector reescribió una rama con commits ajenos")
	}
	pr := f.gh.GetPR(1)
	if len(pr.Comments) != 1 || !strings.Contains(pr.Comments[0], "<!-- debfactory-watch-newer version=4.5.0 -->") ||
		!strings.Contains(pr.Comments[0], "**4.5.0**") {
		t.Errorf("comentarios = %q", pr.Comments)
	}
}

// TestWatchRetracted: Edge Cases, el release desaparece sin sustituta.
func TestWatchRetracted(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 60, nil)
	f.mustCycle(Options{})
	f.retract(bobRepo, "4.4.0")
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-cerrado #1")
	pr := f.gh.GetPR(1)
	if pr.State != "closed" || len(pr.Comments) != 1 || !strings.Contains(pr.Comments[0], "ya no es una versión candidata") {
		t.Errorf("PR = %+v", pr)
	}
	if f.g.RemoteBranch(bobPR) != "" {
		t.Error("la rama del detector no se borró")
	}
}

// TestWatchRegeneratesOnMainChange: Edge Cases (C1), la rama se rehace sobre
// la punta de main con la misma versión.
func TestWatchRegeneratesOnMainChange(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 60, nil)
	f.mustCycle(Options{})
	f.commitMain("packages/bob.yaml", func(s string) string {
		return strings.Replace(s, "summary: version manager for Neovim", "summary: Neovim version manager", 1)
	})
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-actualizado #1")
	if _, err := f.remoteGit("merge-base", "--is-ancestor", "main", bobPR); err != nil {
		t.Error("la rama no parte de la punta de main")
	}
	if man := f.g.RemoteFile(bobPR, "packages/bob.yaml"); !strings.Contains(man, "summary: Neovim version manager") || !strings.Contains(man, "version: 4.4.0") {
		t.Errorf("manifiesto de la rama:\n%s", man)
	}
	if len(f.gh.OpenPRs()) != 1 {
		t.Error("nunca dos PR")
	}
}

// TestWatchOmitted: FR-013a, sin peticiones.
func TestWatchOmitted(t *testing.T) {
	f := newWatchFixture(t)
	f.commitMain("packages/bob.yaml", func(s string) string {
		return strings.Replace(s, "  auto_merge: true", "  enabled: false", 1)
	})
	f.release(bobRepo, "4.4.0", 60, nil)
	f.mustCycle(Options{})
	f.wantOut("watch: bob 4.3.0: omitido (watch.enabled: false)", "consultas de releases 0")
	if f.gh.CountRequests("GET /repos/"+bobRepo) != 0 || len(f.gh.OpenPRs()) != 0 {
		t.Error("un paquete omitido se consultó o recibió un PR")
	}
}

// TestWatchPrerelease: US2, SC-003, FR-003 y FR-003a.
func TestWatchPrerelease(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 60, func(r *citest.Release) { r.Prerelease = true })
	f.release(bobRepo, "4.5.0-rc1", 60, nil)
	f.mustCycle(Options{})
	f.wantOut("watch: bob 4.3.0: sin-cambios")
	if len(f.gh.OpenPRs()) != 0 || len(f.gh.OpenIssues()) != 0 {
		t.Fatal("una versión preliminar generó un PR o un issue")
	}
	// prerelease_flag: ignore acepta 4.4.0 (marca no fiable), nunca la rc.
	f.commitMain("packages/bob.yaml", func(s string) string { return s + "  prerelease_flag: ignore\n" })
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-abierto #1")
	// US2 escenario 3: con el PR en 4.4.0, una beta no lo cambia.
	f.release(bobRepo, "4.6.0-beta.1", 60, nil)
	f.mustCycle(Options{Force: true})
	f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-al-día #1")
}

// TestWatchAutoMerge: US3, FR-013–FR-015a.
func TestWatchAutoMerge(t *testing.T) {
	t.Run("mayor", func(t *testing.T) {
		f := newWatchFixture(t)
		f.release(bobRepo, "5.0.0", 60, nil)
		f.mustCycle(Options{})
		pr := f.openPR()
		if pr.AutoMerge || !strings.Contains(pr.Body, "| Cambio mayor | **sí** |") ||
			!strings.Contains(pr.Body, "| Auto-merge | no (cambio de versión mayor) |") {
			t.Errorf("PR mayor = %v\n%s", pr.AutoMerge, pr.Body)
		}
	})
	t.Run("sin auto_merge", func(t *testing.T) {
		f := newWatchFixture(t)
		f.commitMain("packages/bob.yaml", func(s string) string { return strings.Replace(s, "  auto_merge: true", "  auto_merge: false", 1) })
		f.release(bobRepo, "4.4.0", 60, nil)
		f.mustCycle(Options{})
		if pr := f.openPR(); pr.AutoMerge || !strings.Contains(pr.Body, "| Auto-merge | no (desactivado en el manifiesto) |") {
			t.Errorf("PR = %v\n%s", pr.AutoMerge, pr.Body)
		}
	})
	t.Run("licencia cambiada", func(t *testing.T) {
		f := newWatchFixture(t)
		apache, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), "testdata/licenses/buf-v1.75.0-LICENSE"))
		if err != nil {
			t.Fatal(err)
		}
		f.release(bobRepo, "4.4.0", 60, nil)
		f.up.Serve("https://raw.githubusercontent.com/"+bobRepo+"/v4.4.0/LICENSE", apache)
		f.mustCycle(Options{})
		pr := f.openPR()
		if pr.AutoMerge || !strings.Contains(pr.Body, "| Auto-merge | no (cambio de licencia MIT → Apache-2.0) |") ||
			!strings.Contains(pr.Body, "**cambia**: MIT → Apache-2.0") {
			t.Errorf("PR = %v\n%s", pr.AutoMerge, pr.Body)
		}
		if man := f.g.RemoteFile(bobPR, "packages/bob.yaml"); !strings.Contains(man, "  spdx: Apache-2.0\n") {
			t.Errorf("license.spdx no cambió:\n%s", man)
		}
	})
	t.Run("de menor a mayor se desactiva", func(t *testing.T) {
		f := newWatchFixture(t)
		f.release(bobRepo, "4.4.0", 60, nil)
		f.mustCycle(Options{})
		if !f.openPR().AutoMerge {
			t.Fatal("4.4.0 debería tener auto-merge")
		}
		f.release(bobRepo, "5.0.0", 55, nil)
		f.mustCycle(Options{Force: true})
		if pr := f.openPR(); pr.AutoMerge || pr.Title != "bob: 4.3.0 → 5.0.0" {
			t.Errorf("PR = %v %q", pr.AutoMerge, pr.Title)
		}
	})
	t.Run("Allow auto-merge desactivado", func(t *testing.T) {
		f := newWatchFixture(t)
		f.gh.AutoMergeDisallowed = true
		f.release(bobRepo, "4.4.0", 60, nil)
		err := f.cycle(Options{})
		if err == nil || !strings.Contains(err.Error(), "¿Allow auto-merge desactivado en el repositorio?") {
			t.Fatalf("error = %v\n%s", err, f.out.String())
		}
		f.openPR()
	})
}

// TestWatchNotifications: US4, SC-004, FR-016–FR-019.
func TestWatchNotifications(t *testing.T) {
	t.Run("licencia fuera de la allowlist", func(t *testing.T) {
		f := newWatchFixture(t)
		f.release(bobRepo, "4.4.0", 60, nil)
		f.up.Serve("https://raw.githubusercontent.com/"+bobRepo+"/v4.4.0/LICENSE", []byte(testutil.ZlibText))
		f.mustCycle(Options{})
		f.wantOut("watch: bob 4.3.0 → 4.4.0: notificado watch-license #1")
		downloads := f.up.Downloads()
		for range 3 {
			f.mustCycle(Options{Force: true})
			f.wantOut("watch: bob 4.3.0 → 4.4.0: ya-notificado #1 (watch-license)")
		}
		is := f.gh.OpenIssues()
		if len(is) != 1 || is[0].Title != "fallo: bob 4.4.0 en watch-license" || len(is[0].Comments) != 0 ||
			!strings.Contains(is[0].Body, "licencia Zlib fuera de config/licenses.yaml") ||
			!strings.Contains(is[0].Body, "declarada: MIT") || !strings.Contains(is[0].Body, "detectada: Zlib (cobertura 100 %)") {
			t.Errorf("issues = %+v", is)
		}
		if f.up.Downloads() != downloads || len(f.gh.OpenPRs()) != 0 {
			t.Error("se reintentó con la notificación abierta o se abrió un PR")
		}
	})
	t.Run("assets renombrados y corrección del manifiesto", func(t *testing.T) {
		f := newWatchFixture(t)
		f.release(bobRepo, "4.4.0", 60, func(r *citest.Release) {
			r.Assets = []string{"bob-4.4.0-linux-x86_64.zip", "bob-4.4.0-linux-aarch64.zip"}
		})
		for _, a := range []string{"x86_64", "aarch64"} {
			f.up.Serve("https://github.com/"+bobRepo+"/releases/download/v4.4.0/bob-4.4.0-linux-"+a+".zip", bobZip("4.4.0", a))
		}
		f.mustCycle(Options{})
		f.wantOut("watch: bob 4.3.0 → 4.4.0: notificado watch-assets #1")
		is := f.gh.OpenIssues()
		if len(is) != 1 || !strings.Contains(is[0].Body, "esperados:   bob-linux-x86_64.zip, bob-linux-arm.zip") ||
			!strings.Contains(is[0].Body, "encontrados: bob-4.4.0-linux-x86_64.zip, bob-4.4.0-linux-aarch64.zip") {
			t.Fatalf("issues = %+v", is)
		}
		// El mantenedor corrige el manifiesto: se vuelve a evaluar, se abre el
		// PR y la notificación se cierra con referencia a él (FR-018, FR-019).
		f.gh.SetIssueCreated(1, time.Now().Add(-time.Hour))
		f.commitMain("packages/bob.yaml", func(s string) string {
			s = strings.Replace(s, "bob-linux-{{arch}}.zip", "bob-{{version}}-linux-{{arch}}.zip", 1)
			return strings.Replace(s, "arm64: arm}", "arm64: aarch64}", 1)
		})
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: bob 4.3.0 → 4.4.0: pr-abierto #2")
		if len(f.gh.OpenIssues()) != 0 {
			t.Error("la notificación no se cerró")
		}
		if c := f.gh.Issues[1].Comments; len(c) != 1 || !strings.Contains(c[0], "el PR #2 propone 4.4.0") {
			t.Errorf("comentario de cierre = %q", c)
		}
	})
	t.Run("checksums que no coinciden", func(t *testing.T) {
		f := newWatchFixture(t)
		f.commitMain("packages/bob.yaml", func(s string) string {
			return strings.Replace(s, "  format: zip\n", "  format: zip\nchecksums:\n  url: https://github.com/MordechaiHadad/bob/releases/download/{{tag}}/checksums.txt\n  format: sha256sum\n", 1)
		})
		f.release(bobRepo, "4.4.0", 60, func(r *citest.Release) { r.Assets = append(r.Assets, "checksums.txt") })
		bad := fmt.Sprintf("%s  bob-linux-x86_64.zip\n%s  bob-linux-arm.zip\n", strings.Repeat("0", 64), sha(bobZip("4.4.0", "arm")))
		f.up.Serve("https://github.com/"+bobRepo+"/releases/download/v4.4.0/checksums.txt", []byte(bad))
		f.mustCycle(Options{})
		f.wantOut("watch: bob 4.3.0 → 4.4.0: notificado watch-checksums #1")
		if is := f.gh.OpenIssues(); len(is) != 1 || !strings.Contains(is[0].Body, sha(bobZip("4.4.0", "x86_64"))) {
			t.Errorf("issues = %+v", is)
		}
	})
	t.Run("asset resubido con la misma etiqueta", func(t *testing.T) {
		f := newWatchFixture(t)
		f.release(bobRepo, "4.4.0", 60, nil)
		f.mustCycle(Options{})
		tip := f.g.RemoteBranch(bobPR)
		other := bobZip("4.4.0 (resubido)", "x86_64")
		f.up.Serve("https://github.com/"+bobRepo+"/releases/download/v4.4.0/bob-linux-x86_64.zip", other)
		f.rels[bobRepo][1].Digests = map[string]string{"bob-linux-x86_64.zip": sha(other)}
		f.gh.SetReleases(bobRepo, f.rels[bobRepo]...)
		for range 2 {
			f.mustCycle(Options{Force: true})
		}
		f.wantOut("watch: bob 4.3.0 → 4.4.0: ya-notificado #2 (watch-tampered)")
		is := f.gh.OpenIssues()
		if len(is) != 1 || is[0].Title != "fallo: bob 4.4.0 en watch-tampered" || !strings.Contains(is[0].Body, sha(other)) {
			t.Fatalf("issues = %+v", is)
		}
		if f.g.RemoteBranch(bobPR) != tip {
			t.Error("el detector tocó el PR con un asset resubido")
		}
		// Ni siquiera un cambio de main lo reabre: solo el mantenedor la cierra.
		f.gh.SetIssueCreated(2, time.Now().Add(-time.Hour))
		f.commitMain("README.md", func(string) string { return "y\n" })
		f.mustCycle(Options{Force: true})
		f.wantOut("ya-notificado #2 (watch-tampered)")
	})
	t.Run("resubido sin digest al regenerar", func(t *testing.T) {
		f := newWatchFixture(t)
		f.release(bobRepo, "4.4.0", 60, nil)
		f.mustCycle(Options{})
		f.up.Serve("https://github.com/"+bobRepo+"/releases/download/v4.4.0/bob-linux-arm.zip", bobZip("otro", "arm"))
		f.commitMain("README.md", func(string) string { return "y\n" }) // obliga a regenerar la rama
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: bob 4.3.0 → 4.4.0: notificado watch-tampered #2")
		if is := f.gh.OpenIssues(); len(is) != 1 || !strings.Contains(is[0].Body, "artifacts.arm64: ") {
			t.Errorf("issues = %+v", is)
		}
	})
	t.Run("una versión nueva cierra las notificaciones viejas", func(t *testing.T) {
		f := newWatchFixture(t)
		f.release(bobRepo, "4.4.0", 60, nil)
		f.up.Serve("https://raw.githubusercontent.com/"+bobRepo+"/v4.4.0/LICENSE", []byte(testutil.ZlibText))
		f.mustCycle(Options{})
		f.release(bobRepo, "4.4.1", 50, nil)
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: bob 4.3.0 → 4.4.1: pr-abierto #2")
		if len(f.gh.OpenIssues()) != 0 {
			t.Error("la notificación de 4.4.0 sigue abierta")
		}
	})
}

// TestWatchRateLimit: US5, SC-006, SC-007, FR-021.
func TestWatchRateLimit(t *testing.T) {
	t.Run("304 y reserva", func(t *testing.T) {
		f := newWatchFixture(t, "bob", "fnm")
		f.release("Schniz/fnm", "1.39.0", 900, nil)
		f.gh.SetRate(appToken, 5000)
		f.mustCycle(Options{})
		f.wantOut("consultas de releases 2 (0 × 304); restantes ")
		f.mustCycle(Options{Force: true})
		f.wantOut("consultas de releases 2 (2 × 304)")
		// Reserva alcanzada: se detiene en verde y deja pendientes.
		f.gh.SetRate(appToken, 501)
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: bob : pendiente-límite", "watch: fnm : pendiente-límite", "ciclo detenido por el límite (reserva 500), 2 paquetes pendientes")
		if f.gh.RateRemaining < 500 {
			t.Errorf("el ciclo bajó de la reserva: %d", f.gh.RateRemaining)
		}
	})
	t.Run("pendientes primero y aviso tras 3 ciclos", func(t *testing.T) {
		f := newWatchFixture(t, "bob", "fnm")
		f.release("Schniz/fnm", "1.39.0", 900, nil)
		// 502: la lista de PRs y un paquete; el segundo queda pendiente.
		f.gh.SetRate(appToken, 502)
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: bob 4.3.0: sin-cambios", "watch: fnm : pendiente-límite")
		f.gh.SetRate(appToken, 502)
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: fnm 1.39.0: sin-cambios", "watch: bob : pendiente-límite") // pendientes primero
		if strings.Index(f.out.String(), "fnm 1.39.0") > strings.Index(f.out.String(), "bob : ") {
			t.Error("el pendiente no se revisó primero")
		}
		if len(f.gh.OpenIssues()) != 0 {
			t.Fatal("aviso antes de 3 ciclos detenidos")
		}
		f.gh.SetRate(appToken, 501)
		f.mustCycle(Options{Force: true})
		is := f.gh.OpenIssues()
		if len(is) != 1 || is[0].Title != "fallo: pipeline - en watch-ratelimit" {
			t.Fatalf("issues = %+v", is)
		}
		f.gh.SetRate(appToken, 5000)
		f.mustCycle(Options{Force: true})
		if len(f.gh.OpenIssues()) != 0 {
			t.Error("el aviso de límite no se cerró tras un ciclo completo")
		}
	})
	t.Run("403 de límite", func(t *testing.T) {
		f := newWatchFixture(t)
		f.gh.SetRate(appToken, 0)
		f.mustCycle(Options{Force: true})
		f.wantOut("watch: bob : pendiente-límite")
	})
	t.Run("100 paquetes", func(t *testing.T) {
		f := newWatchFixture(t)
		base := f.g.RemoteFile("main", "packages/bob.yaml")
		c := f.g.Clone()
		for i := range 100 {
			name := fmt.Sprintf("p%03d", i)
			m := strings.Replace(base, "name: bob", "name: "+name, 1)
			m = strings.ReplaceAll(m, bobRepo, "o/"+name)
			c.Write("packages/"+name+".yaml", m)
			f.rels["o/"+name] = []citest.Release{{Tag: "v4.3.0", PublishedAt: f.now.Add(-900 * time.Hour)}}
			f.gh.SetReleases("o/"+name, f.rels["o/"+name]...)
		}
		c.Commit("100 paquetes")
		c.Push()
		f.gh.SetRate(appToken, 560)
		f.mustCycle(Options{Force: true})
		if f.gh.RateRemaining < 500 || !strings.Contains(f.out.String(), "pendiente-límite") {
			t.Errorf("restantes %d; salida:\n%s", f.gh.RateRemaining, f.out.String())
		}
		f.gh.SetRate(appToken, 5000)
		f.mustCycle(Options{Force: true})
		f.mustCycle(Options{Force: true})
		f.wantOut("consultas de releases 101 (101 × 304)")
	})
}

// TestWatchBarrier: contracts/cli.md § Barrera de confianza y --dry-run.
func TestWatchBarrier(t *testing.T) {
	f := newWatchFixture(t)
	f.release(bobRepo, "4.4.0", 60, nil)
	cases := map[string]struct{ key, val, want string }{
		"fuera de Actions":    {"GITHUB_ACTIONS", "", "en local usa --dry-run"},
		"runner pub":          {"RUNNER_LABELS", "self-hosted,debfactory-main", "nunca en el que tiene la llave"},
		"evento push":         {"GITHUB_EVENT_NAME", "push", "solo corre con workflow_dispatch"},
		"otra rama":           {"GITHUB_REF", "refs/heads/x", "solo corre sobre refs/heads/main"},
		"sin token de App":    {"DEBFACTORY_APP_TOKEN", "", "falta DEBFACTORY_APP_TOKEN"},
		"sin slug":            {"DEBFACTORY_APP_SLUG", "", "falta DEBFACTORY_APP_SLUG"},
		"otro repositorio":    {"GITHUB_REPOSITORY", "x/y", "se esperaba"},
		"paquete inválido":    {"INPUT_PACKAGE", "../etc", "nombre de paquete inválido"},
		"paquete inexistente": {"INPUT_PACKAGE", "nope", "no existe packages/nope.yaml"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			old := f.vars[c.key]
			f.vars[c.key] = c.val
			defer func() { f.vars[c.key] = old }()
			err := f.cycle(Options{})
			var env *report.EnvError
			var in *InputError
			if err == nil || !strings.Contains(err.Error(), c.want) || (!errors.As(err, &env) && !errors.As(err, &in)) {
				t.Fatalf("error = %v", err)
			}
			if len(f.writes()) != 0 {
				t.Error("escribió sin pasar la barrera")
			}
		})
	}
	// --dry-run: sin barrera ni escrituras, con lo que haría.
	f.vars["GITHUB_ACTIONS"], f.vars["DEBFACTORY_APP_TOKEN"] = "", ""
	f.mustCycle(Options{DryRun: true})
	f.wantOut("watch:   bob v4.4.0 (2026-11-18): estable 4.4.0", "watch: bob 4.3.0 → 4.4.0: (dry-run) abriría un PR")
	if len(f.writes()) != 0 || len(f.gh.OpenPRs()) != 0 || f.g.RemoteBranch(bobPR) != "" {
		t.Errorf("--dry-run escribió: %v", f.writes())
	}
}

// TestSetVersion: la edición del manifiesto conserva comentarios y estilo, y
// pone comillas si la versión se leería como un número en YAML.
func TestSetVersion(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), "packages/bob.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := setVersion(src, "packages/bob.yaml", "4.10.1")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(string(src), "version: 4.3.0", "version: 4.10.1", 1)
	if string(out) != want {
		t.Errorf("edición:\n%s", out)
	}
	out, err = setVersion(src, "packages/bob.yaml", "4.10")
	if err != nil || !strings.Contains(string(out), "version: \"4.10\"\n") {
		t.Errorf("4.10 sin comillas: %v\n%s", err, out)
	}
	lic, err := setLicense(out, "packages/bob.yaml", "Apache-2.0")
	if err != nil || !strings.Contains(string(lic), "license:\n  spdx: Apache-2.0\n") {
		t.Errorf("license.spdx: %v\n%s", err, lic)
	}
}
