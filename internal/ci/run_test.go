//go:build integration

package ci

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/build"
	"debfactory/internal/ci/citest"
	"debfactory/internal/config"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/testutil"
)

const emptyState = "# Generado por debfactory publish/retire. No editar a mano.\nschema: 1\nserial: 0\npackages: {}\nretired: []\n"

// mainFixture es un repositorio git con los 4 paquetes reales y el estado
// vacío, un GitHub falso, un <data> con la llave de prueba y el destino local.
type mainFixture struct {
	t        *testing.T
	g        *citest.Git
	gh       *citest.FakeGitHub
	cfg      *config.Config
	data     string
	out      bytes.Buffer
	all      bytes.Buffer // salida de todas las ejecuciones
	failGate map[string]bool
	alter    map[string]bool // el build de estos paquetes no reproduce el .deb
	built    []string
}

func newMainFixture(t *testing.T) *mainFixture {
	t.Helper()
	files := map[string]string{"repo/state.yaml": emptyState, "README.md": "x\n"}
	for _, d := range []string{"config", "packages", "schema"} {
		filepath.WalkDir(filepath.Join("../..", d), func(p string, de os.DirEntry, err error) error {
			if err == nil && de.Type().IsRegular() {
				rel, _ := filepath.Rel("../..", p)
				data, _ := os.ReadFile(p)
				files[rel] = string(data)
			}
			return nil
		})
	}
	f := &mainFixture{t: t, g: citest.GitFixture(t, files), gh: citest.NewFakeGitHub(t), failGate: map[string]bool{}}
	cfg, err := config.Load(f.g.Work)
	if err != nil {
		t.Fatal(err)
	}
	home, master, sub := testutil.TestKey(t)
	cfg.Repo.Signing.Masters, cfg.Repo.Signing.Subkey = []string{master}, sub
	cfg.CI.Repository = citest.Repo
	cfg.Repo.Destinations["ci-local"] = config.Destination{Remote: t.TempDir()}
	cfg.CI.Destination = "ci-local"
	f.cfg = cfg
	f.data = t.TempDir()
	if err := os.Symlink(home, filepath.Join(f.data, "gnupg")); err != nil {
		t.Fatal(err)
	}
	return f
}

// checkout pone el checkout del pipeline en la punta de main, como un job
// nuevo (actions/checkout).
func (f *mainFixture) checkout() string {
	f.t.Helper()
	f.g.Git("fetch", "-q", "origin")
	f.g.Git("reset", "-q", "--hard")
	f.g.Git("clean", "-qfdx", "--", "repo")
	f.g.Git("checkout", "-q", "--detach", "origin/main")
	return f.g.Git("rev-parse", "HEAD")
}

func (f *mainFixture) runner(sha string) *Runner {
	pr := &report.Printer{Out: &f.out, Err: &f.out}
	vars := map[string]string{
		"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/heads/main", "GITHUB_SHA": sha,
		"GITHUB_REPOSITORY": citest.Repo, "GITHUB_RUN_ID": "1000", "RUNNER_LABELS": "self-hosted,debfactory-main",
		"GITHUB_TOKEN": secretToken, "GITHUB_API_URL": f.gh.URL,
	}
	suites := []string{}
	for _, s := range f.cfg.Suites {
		suites = append(suites, s.Name)
	}
	return &Runner{
		Root: f.g.Work, Cfg: f.cfg, Env: newEnv(f.cfg, vars), Out: pr,
		Repo:  &repo.Env{Root: f.g.Work, DataDir: f.data, Cfg: f.cfg, Out: pr},
		Build: func() *build.Env { return &build.Env{Cfg: f.cfg} },
		Gates: func(ctx context.Context, root, name string) (map[string]string, error) {
			m, err := manifest.Validate(root, f.cfg, name)
			if err != nil {
				return nil, err
			}
			f.built = append(f.built, name)
			if f.failGate[name] {
				return nil, &report.Error{Package: name, Version: m.DebVersion(), Arch: "arm64", Suite: "noble",
					Stage: report.Test, Cause: "la orden de comprobación salió con código 127"}
			}
			shas := citest.FakeDist(f.t, root, name, m.DebVersion(), f.cfg.Arches, suites, m.Exclude.Suites)
			if f.alter[name] {
				for _, arch := range f.cfg.Arches {
					p := filepath.Join(root, "dist", name+"_"+m.DebVersion()+"_"+arch+".deb")
					fh, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
					fh.WriteString("x")
					fh.Close()
					shas[arch] = "alterado"
				}
			}
			return shas, nil
		},
	}
}

func (f *mainFixture) main() error {
	f.t.Helper()
	f.all.Write(f.out.Bytes())
	f.out.Reset()
	f.built = nil
	sha := f.checkout()
	return f.runner(sha).Main(context.Background(), Options{Jobs: 4})
}

func (f *mainFixture) state() *repo.State {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.data, "ci", "state.yaml"))
	if err != nil {
		f.t.Fatal(err)
	}
	st, err := (&Ref{State: data}).ParseState()
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

// bump sube la revision de un paquete en un clon del mantenedor y lo integra.
func (f *mainFixture) bump(names ...string) {
	f.t.Helper()
	c := f.g.Clone()
	for _, name := range names {
		p := filepath.Join(c.Work, "packages", name+".yaml")
		data, _ := os.ReadFile(p)
		os.WriteFile(p, bytes.Replace(data, []byte("revision: 1"), []byte("revision: 2"), 1), 0o644)
	}
	c.Commit("subir " + strings.Join(names, " "))
	c.Push()
}

func (f *mainFixture) mergeStatePR() {
	f.t.Helper()
	f.g.MergeBranch("debfactory/state")
	for _, pr := range f.gh.OpenPRs() {
		f.gh.ClosePR(pr.Number, true)
	}
}

func gens(t *testing.T, data string) int {
	m, _ := filepath.Glob(filepath.Join(data, "public", "gen-*.json"))
	return len(m)
}

// TestMain recorre US1: publicar todo, integrar el PR de estado (nada que
// publicar), subir un paquete (solo ese cambia), un fallo de un paquete no
// impide los demás (FR-004) y una ejecución superada.
func TestCIMain(t *testing.T) {
	f := newMainFixture(t)
	if err := f.main(); err != nil {
		t.Fatalf("primera ejecución: %v\n%s", err, f.out.String())
	}
	st := f.state()
	if st.Serial != 1 || len(st.PackageNames()) != 4 {
		t.Fatalf("estado = serial %d, %v", st.Serial, st.PackageNames())
	}
	if prs := f.gh.OpenPRs(); len(prs) != 1 {
		t.Fatalf("PRs de estado = %d", len(prs))
	}
	audit := f.g.RemoteFile("debfactory/state", AuditFile)
	if strings.Count(audit, `"op":"publish"`) != 4 || !strings.Contains(audit, `"run":"https://github.com/owner/debfactory/actions/runs/1000"`) {
		t.Fatalf("auditoría en la rama de estado:\n%s", audit)
	}
	before := st.Clone()

	// Integrar el PR de estado: su push no publica nada (US1 escenario 6).
	f.mergeStatePR()
	if err := f.main(); err != nil {
		t.Fatalf("tras integrar el PR de estado: %v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "nada que publicar") || f.state().Serial != 1 || len(f.built) != 0 {
		t.Fatalf("el merge del PR de estado publicó algo:\n%s", f.out.String())
	}
	if f.g.RemoteBranch("debfactory/state") != "" {
		t.Error("la rama de estado no se borró tras integrarse")
	}

	// Subir fnm: solo fnm se construye y publica (US1 escenario 2).
	f.bump("fnm")
	if err := f.main(); err != nil {
		t.Fatalf("subir fnm: %v\n%s", err, f.out.String())
	}
	st = f.state()
	if strings.Join(f.built, " ") != "fnm" || st.Serial != 2 {
		t.Fatalf("construidos %v, serial %d", f.built, st.Serial)
	}
	for _, name := range []string{"aqua", "bob", "buf"} {
		if st.Packages[name][0].Debs["amd64"] != before.Packages[name][0].Debs["amd64"] {
			t.Errorf("%s cambió", name)
		}
	}
	if v := st.Packages["fnm"]; len(v) != 2 || v[0].Version != "1.39.0-0~tepache2" {
		t.Errorf("fnm = %+v (la anterior debe seguir disponible)", v)
	}

	// Dos paquetes y uno falla: el otro se publica y hay un issue (FR-004, US4).
	f.bump("bob", "buf")
	f.failGate["bob"] = true
	err := f.main()
	if err == nil || !strings.Contains(err.Error(), "bob") {
		t.Fatalf("se esperaba el fallo de bob: %v", err)
	}
	st = f.state()
	if st.Packages["buf"][0].Version != "1.75.0-0~tepache2" || st.Packages["bob"][0].Version != "4.3.0-0~tepache1" {
		t.Fatalf("buf debía publicarse y bob no: %+v %+v", st.Packages["buf"][0], st.Packages["bob"][0])
	}
	issues := f.gh.OpenIssues()
	if len(issues) != 1 || issues[0].Title != "fallo: bob 4.3.0-0~tepache2 en test" ||
		!strings.HasPrefix(issues[0].Body, "<!-- debfactory:failure package=bob version=4.3.0-0~tepache2 stage=test -->") ||
		!strings.Contains(issues[0].Body, "noble/arm64") {
		t.Fatalf("issues = %+v", issues)
	}
	// El mismo fallo otra vez: comentario, no otro issue (FR-021).
	if err := f.main(); err == nil {
		t.Fatal("se esperaba el mismo fallo")
	}
	if issues := f.gh.OpenIssues(); len(issues) != 1 || len(issues[0].Comments) != 1 {
		t.Fatalf("deduplicación: %+v", issues)
	}
	// Corregido: se publica y el issue se cierra con el enlace (US4 escenario 3).
	f.failGate["bob"] = false
	if err := f.main(); err != nil {
		t.Fatalf("bob corregido: %v\n%s", err, f.out.String())
	}
	if issues := f.gh.OpenIssues(); len(issues) != 0 {
		t.Fatalf("el issue no se cerró: %+v", issues)
	}
	if prs := f.gh.OpenPRs(); len(prs) != 1 {
		t.Fatalf("nunca dos PR de estado: %d", len(prs))
	}

	// Ejecución superada: código 0 sin construir.
	old := f.g.Git("rev-parse", "HEAD~1")
	f.bump("aqua")
	f.out.Reset()
	f.built = nil
	f.checkout()
	if err := f.runner(old).Main(context.Background(), Options{Jobs: 2}); err != nil || len(f.built) != 0 ||
		!strings.Contains(f.out.String(), "superada") {
		t.Fatalf("superada: %v %v\n%s", err, f.built, f.out.String())
	}
	f.checkNoSecrets()
}

// TestCIMainRecovery: matar la ejecución tras el swap o tras el sync; la
// siguiente completa sync, verify y push sin otra generación ni entradas
// duplicadas (SC-007, FR-029).
func TestCIMainRecovery(t *testing.T) {
	for _, when := range []string{"swap", "sync"} {
		t.Run(when, func(t *testing.T) {
			f := newMainFixture(t)
			kill := errors.New("ejecución matada")
			if when == "swap" {
				TestHookAfterSwap = func() error { return kill }
			} else {
				TestHookAfterSync = func() error { return kill }
			}
			err := f.main()
			TestHookAfterSwap, TestHookAfterSync = nil, nil
			if err == nil || !strings.Contains(err.Error(), kill.Error()) {
				t.Fatalf("err = %v", err)
			}
			n := gens(t, f.data)
			if f.g.RemoteBranch("debfactory/state") != "" {
				t.Fatal("la rama no debía haberse empujado aún")
			}
			if err := f.main(); err != nil {
				t.Fatalf("relanzar: %v\n%s", err, f.out.String())
			}
			if gens(t, f.data) != n {
				t.Errorf("se generó otra generación (%d → %d)", n, gens(t, f.data))
			}
			audit := f.g.RemoteFile("debfactory/state", AuditFile)
			if strings.Count(audit, "\n") != 4 {
				t.Errorf("auditoría duplicada o incompleta:\n%s", audit)
			}
			if err := f.runner(f.g.Git("rev-parse", "HEAD")).Repo.Verify(context.Background()); err != nil &&
				!strings.Contains(err.Error(), "docs/usuario.md") {
				t.Errorf("repo verify: %v", err)
			}
		})
	}
}

// TestCIMainAuditTampered: editar una línea antigua de audit.jsonl en main
// hace fallar la siguiente ejecución en la etapa audit sin publicar.
func TestCIMainAuditTampered(t *testing.T) {
	f := newMainFixture(t)
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	f.mergeStatePR()
	c := f.g.Clone()
	p := filepath.Join(c.Work, AuditFile)
	data, _ := os.ReadFile(p)
	os.WriteFile(p, bytes.Replace(data, []byte(`"serial":1`), []byte(`"serial":7`), 1), 0o644)
	c.Commit("editar auditoría")
	c.Push()
	f.bump("fnm")
	err := f.main()
	if err == nil || !strings.Contains(err.Error(), ": state: ") && !strings.Contains(err.Error(), ": audit: ") {
		t.Fatalf("err = %v", err)
	}
	if f.state().Serial != 1 || len(f.built) != 0 {
		t.Error("publicó con la auditoría alterada")
	}
}

// checkNoSecrets busca el token, la frase de paso y llaves privadas en todo lo
// que imprimió el pipeline y en todo lo que recibió el GitHub falso (FR-016,
// SC-003). Se acumula la salida de todas las ejecuciones del test.
func (f *mainFixture) checkNoSecrets() {
	f.t.Helper()
	pass, err := os.ReadFile(filepath.Join(f.data, "gnupg", "passphrase"))
	if err != nil || len(bytes.TrimSpace(pass)) == 0 {
		f.t.Fatalf("frase de paso de prueba: %v", err)
	}
	for where, text := range map[string]string{"salida": f.all.String() + f.out.String(), "GitHub": f.gh.AllBodies()} {
		for _, secret := range []string{secretToken, strings.TrimSpace(string(pass)), "PRIVATE KEY"} {
			if strings.Contains(text, secret) {
				f.t.Errorf("un secreto aparece en %s", where)
			}
		}
	}
}
