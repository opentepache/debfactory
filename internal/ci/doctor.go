package ci

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"debfactory/ci/runner"
	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/forge"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
)

// Check es una comprobación de ci doctor.
type Check struct {
	Name string
	OK   bool
	Warn bool // no se pudo comprobar (no cuenta como fallo)
	Info string
}

// Host abstrae lo que ci doctor mira de la PC, para poder probarlo.
type Host struct {
	Stat       func(path string) (fs.FileInfo, error)
	LookupUser func(name string) (*user.User, error)
	GroupNames func(u *user.User) ([]string, error)
	Run        func(name string, args ...string) error
	ReadDir    func(path string) ([]fs.DirEntry, error)
	Home       string
}

// ToolsDir es donde `just tool-install` deja cada versión de la herramienta
// (specs/004-split-repos, research D3).
const ToolsDir = "/srv/debfactory/tools"

// RealHost es la PC real.
func RealHost() Host {
	home, _ := os.UserHomeDir()
	return Host{
		Stat:       os.Stat,
		ReadDir:    os.ReadDir,
		LookupUser: user.Lookup,
		GroupNames: func(u *user.User) ([]string, error) {
			ids, err := u.GroupIds()
			if err != nil {
				return nil, err
			}
			var names []string
			for _, id := range ids {
				if g, err := user.LookupGroupId(id); err == nil {
					names = append(names, g.Name)
				}
			}
			return names, nil
		},
		Run:  func(name string, args ...string) error { return exec.Command(name, args...).Run() },
		Home: home,
	}
}

// Doctor es `ci doctor [--fix]`: comprueba con la API la configuración de
// GitHub (research §2) y en local la de la PC (contracts/runner-host.md). Usa
// el token del mantenedor en GH_TOKEN, que nunca se imprime.
func (r *Runner) Doctor(ctx context.Context, o Options) error {
	if err := r.Cfg.CI.RequireRepository(); err != nil {
		return err
	}
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		return &report.EnvError{Cause: "ci doctor necesita GH_TOKEN (un token del mantenedor con lectura de la administración del repositorio)"}
	}
	api := os.Getenv("GITHUB_API_URL")
	if api == "" {
		api = "https://api.github.com"
	}
	gh, err := forge.New(api, r.Cfg.CI.Repository, token)
	if err != nil {
		return &report.EnvError{Cause: err.Error()}
	}
	checks := r.GitHubChecks(ctx, gh, o.Fix)
	checks = append(checks, r.SiteChecks(ctx, http.DefaultClient)...)
	checks = append(checks, r.Level2Checks()...)
	checks = append(checks, r.HostChecks(RealHost())...)
	failed := 0
	for _, c := range checks {
		mark := "✅"
		switch {
		case c.Warn:
			mark = "⚠️"
		case !c.OK:
			mark = "❌"
			failed++
		}
		line := mark + " " + c.Name
		if c.Info != "" {
			line += ": " + c.Info
		}
		r.Out.Line("%s", line)
	}
	if failed > 0 {
		return report.Newf(report.Config, "ci doctor: %d comprobaciones fallan", failed)
	}
	return nil
}

// Level2Checks comprueba que las compilaciones de nivel 2 simultáneas caben
// en la RAM de la PC (specs/008-source-builds, research D2): level2.jobs ×
// level2.tmpfs ≤ 18 GB de los 31 GB.
func (r *Runner) Level2Checks() []Check {
	l := r.Cfg.Level2
	tmpfs, err := config.ParseSize(l.TmpfsOrDefault())
	if err != nil {
		return []Check{{Name: "tmpfs de nivel 2", Info: err.Error()}}
	}
	total := int64(l.JobsOrDefault()) * tmpfs
	name := fmt.Sprintf("tmpfs de nivel 2: %d × %s", l.JobsOrDefault(), l.TmpfsOrDefault())
	if total > config.Level2TmpfsBudget {
		return []Check{{Name: name, Info: fmt.Sprintf("%d GB supera los %d GB que admite la PC; baja level2.jobs o level2.tmpfs",
			total>>30, int64(config.Level2TmpfsBudget)>>30)}}
	}
	return []Check{{Name: name, OK: true, Info: fmt.Sprintf("%d GB de %d GB", total>>30, int64(config.Level2TmpfsBudget)>>30)}}
}

// SiteChecks comprueba, con site.enabled, que la raíz pública de
// ci.destination sirve el sitio (specs/006-repo-website, research D4): sin la
// regla de reescritura de "/" a "/index.html", un bucket de R2 responde 404.
func (r *Runner) SiteChecks(ctx context.Context, client *http.Client) []Check {
	if !r.Cfg.Site.Enabled {
		return nil
	}
	dest, ok := r.Cfg.Repo.Destinations[r.Cfg.CI.Destination]
	if !ok || dest.PublicURL == "" {
		return nil
	}
	url := strings.TrimSuffix(dest.PublicURL, "/") + "/"
	name := "sitio web en " + url
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return []Check{{Name: name, Info: err.Error()}}
	}
	req.Header.Set("User-Agent", "debfactory")
	resp, err := client.Do(req)
	if err != nil {
		return []Check{{Name: name, Warn: true, Info: "no se pudo consultar: " + err.Error()}}
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode == http.StatusOK && strings.HasPrefix(ct, "text/html") {
		return []Check{{Name: name, OK: true}}
	}
	return []Check{{Name: name, Info: fmt.Sprintf("HTTP %d (%s); falta la regla de reescritura de / a /index.html o el sitio aún no se subió (docs/operacion-ci.md)", resp.StatusCode, ct)}}
}

// GitHubChecks comprueba la configuración del repositorio en GitHub.
func (r *Runner) GitHubChecks(ctx context.Context, gh *forge.Client, fix bool) []Check {
	var cs []Check
	add := func(name string, ok bool, info string) { cs = append(cs, Check{Name: name, OK: ok, Info: info}) }
	// Un repositorio privado no tiene aprobación de forks (GitHub responde
	// 422); en su lugar, los forks MUST NOT ejecutar workflows.
	repo, err := gh.GetRepo(ctx)
	repoOK := err == nil
	switch {
	case err != nil:
		add("visibilidad del repositorio", false, err.Error())
	case repo.Private:
		if f, err := gh.GetForkPrivate(ctx); err != nil {
			add("workflows de forks", false, err.Error())
		} else {
			add("repositorio privado: Run workflows from fork pull requests desactivado",
				!f.RunWorkflows && !f.SendWriteTokens && !f.SendSecrets,
				fmt.Sprintf("valor actual run=%t write_tokens=%t secrets=%t", f.RunWorkflows, f.SendWriteTokens, f.SendSecrets))
		}
	default:
		f, err := gh.GetForkApproval(ctx)
		if err != nil {
			add("aprobación de forks", false, err.Error())
			break
		}
		add("aprobación de forks: Require approval for all external contributors",
			f.ApprovalPolicy == "all_external_contributors", "valor actual "+f.ApprovalPolicy)
	}
	if w, err := gh.GetWorkflowPermissions(ctx); err != nil {
		add("permisos del token", false, err.Error())
	} else {
		add("permisos por defecto del GITHUB_TOKEN en solo lectura", w.DefaultWorkflowPermissions == "read",
			"valor actual "+w.DefaultWorkflowPermissions)
		add("Allow GitHub Actions to create and approve pull requests", w.CanApprovePullRequestReviews, "")
	}
	if rs, err := gh.ListRulesets(ctx); err != nil {
		add("ruleset de main", false, err.Error())
	} else {
		ok, info := false, "no hay un ruleset activo sobre main que exija PR y el check "+VerifyContext+", bloquee force push y borrado y no tenga bypass"
		for _, s := range rs {
			if s.Target != "branch" || s.Enforcement != "active" || len(s.BypassActors) > 0 {
				continue
			}
			inc := s.Conditions.RefName.Include
			if !slices.Contains(inc, "~DEFAULT_BRANCH") && !slices.Contains(inc, "refs/heads/main") && !slices.Contains(inc, "~ALL") {
				continue
			}
			var types []string
			verify := false
			for _, rule := range s.Rules {
				types = append(types, rule.Type)
				for _, c := range rule.Parameters.RequiredStatusChecks {
					verify = verify || (rule.Type == "required_status_checks" && c.Context == VerifyContext)
				}
			}
			if verify && slices.Contains(types, "pull_request") && slices.Contains(types, "non_fast_forward") && slices.Contains(types, "deletion") {
				ok, info = true, "ruleset "+s.Name
				break
			}
		}
		add("ruleset de main sin bypass", ok, info)
	}
	label := r.Cfg.CI.FailureLabel
	if _, err := gh.GetLabel(ctx, label); err == nil {
		add("etiqueta "+label, true, "")
	} else if fix {
		if err := gh.CreateLabel(ctx, label, "d73a4a", "Fallo del pipeline de debfactory"); err != nil {
			add("etiqueta "+label, false, err.Error())
		} else {
			add("etiqueta "+label, true, "creada")
		}
	} else {
		add("etiqueta "+label, false, "no existe; ci doctor --fix la crea")
	}
	cs = append(cs, r.runnerVersionCheck(ctx, gh))
	if !repoOK {
		repo = nil
	}
	cs = append(cs, r.watchChecks(ctx, gh, repo)...)
	return cs
}

// WatchWorkflow es el workflow del detector de versiones en el repositorio de
// paquetes (specs/005-upstream-release-watch, contracts/workflows.md).
const WatchWorkflow = ".github/workflows/watch.yml"

// watchInstalled indica si el repositorio de paquetes tiene el detector.
func (r *Runner) watchInstalled() bool {
	_, err := os.Stat(filepath.Join(r.Root, WatchWorkflow))
	return err == nil
}

// watchChecks comprueba la configuración que necesita el detector (contracts/
// cli.md § ci doctor): el environment watch limitado a main con la llave de
// la App, y "Allow auto-merge" si algún paquete lo usa. La instalación y los
// permisos de la App no se pueden leer con el token del mantenedor.
func (r *Runner) watchChecks(ctx context.Context, gh *forge.Client, repo *forge.RepoInfo) []Check {
	if !r.watchInstalled() {
		return nil
	}
	var cs []Check
	add := func(name string, ok bool, info string) { cs = append(cs, Check{Name: name, OK: ok, Info: info}) }
	env, err := gh.GetEnvironment(ctx, "watch")
	switch {
	case forge.IsNotFound(err):
		add("environment watch", false, "no existe: Settings → Environments → New environment (docs/operacion-ci.md)")
	case err != nil:
		add("environment watch", false, err.Error())
	default:
		p := env.DeploymentBranchPolicy
		ok, info := false, "Deployment branches: elige Selected branches con solo main"
		if p != nil && p.CustomBranchPolicies {
			branches, err := gh.EnvironmentBranches(ctx, "watch")
			switch {
			case err != nil:
				info = err.Error()
			case slices.Equal(branches, []string{"branch:main"}):
				ok, info = true, ""
			default:
				info = "ramas permitidas " + strings.Join(branches, ", ") + "; debe ser solo main"
			}
		}
		add("environment watch limitado a main", ok, info)
		secrets, err := gh.EnvironmentSecrets(ctx, "watch")
		if err != nil {
			add("secretos de la App en el environment watch", false, err.Error())
		} else {
			var missing []string
			for _, want := range []string{"DEBFACTORY_APP_CLIENT_ID", "DEBFACTORY_APP_KEY"} {
				if !slices.Contains(secrets, want) {
					missing = append(missing, want)
				}
			}
			info := ""
			if len(missing) > 0 {
				info = "faltan " + strings.Join(missing, ", ") + " (docs/operacion-ci.md)"
			}
			add("secretos de la App en el environment watch", len(missing) == 0, info)
		}
	}
	if pkgs := r.autoMergePackages(); len(pkgs) > 0 && repo != nil {
		add("Allow auto-merge (lo usan "+strings.Join(pkgs, ", ")+")", repo.AllowAutoMerge,
			map[bool]string{true: "", false: "Settings → General → Allow auto-merge"}[repo.AllowAutoMerge])
	}
	return cs
}

// autoMergePackages lista los paquetes con watch.auto_merge: true.
func (r *Runner) autoMergePackages() []string {
	names, _ := build.ListPackages(r.Root)
	var out []string
	for _, n := range names {
		if m, err := manifest.Load(r.Root, n); err == nil && m.Watch.AutoMerge && m.Watch.Watched() {
			out = append(out, n)
		}
	}
	return out
}

// runnerVersionCheck avisa si RUNNER_VERSION lleva más de 14 días por detrás
// del último release de actions/runner (GitHub rechaza los atrasados unos 30).
func (r *Runner) runnerVersionCheck(ctx context.Context, gh *forge.Client) Check {
	name := "versión del runner (ci/runner/runner.env)"
	pinned := parseEnv(runner.Env)["RUNNER_VERSION"]
	tag, published, err := gh.LatestRelease(ctx, "actions/runner")
	if err != nil {
		return Check{Name: name, Warn: true, Info: "no se pudo consultar el último release: " + err.Error()}
	}
	latest := strings.TrimPrefix(tag, "v")
	if pinned == latest {
		return Check{Name: name, OK: true, Info: pinned + " (la última)"}
	}
	days := int(time.Since(published).Hours() / 24)
	if days > 14 {
		return Check{Name: name, Info: fmt.Sprintf("%s fijada; %s publicada hace %d días: actualiza RUNNER_VERSION y RUNNER_SHA256 antes de 30", pinned, latest, days)}
	}
	return Check{Name: name, OK: true, Info: fmt.Sprintf("%s fijada; %s publicada hace %d días", pinned, latest, days)}
}

// parseEnv lee líneas CLAVE=valor, ignorando comentarios y líneas vacías.
func parseEnv(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			out[k] = v
		}
	}
	return out
}

// HostChecks comprueba la PC (contracts/runner-host.md).
func (r *Runner) HostChecks(h Host) []Check {
	var cs []Check
	add := func(name string, ok bool, info string) { cs = append(cs, Check{Name: name, OK: ok, Info: info}) }
	owner := func(fi fs.FileInfo) string {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			if u, err := user.LookupId(fmt.Sprint(st.Uid)); err == nil {
				return u.Username
			}
			return fmt.Sprint(st.Uid)
		}
		return "?"
	}
	for _, name := range []string{"dfci", "dfpub"} {
		u, err := h.LookupUser(name)
		if err != nil {
			add("usuario "+name, false, "no existe (sudo ci/host/setup.sh)")
			continue
		}
		groups, _ := h.GroupNames(u)
		bad := slices.ContainsFunc(groups, func(g string) bool { return g == "docker" || g == "sudo" })
		add("usuario "+name+" sin los grupos docker ni sudo", !bad, strings.Join(groups, " "))
	}
	if fi, err := h.Stat(h.Home); err == nil {
		add(h.Home+" con permisos 700", fi.Mode().Perm() == 0o700, fmt.Sprintf("%o", fi.Mode().Perm()))
	}
	dirs := []struct {
		path, owner string
		perm        fs.FileMode
	}{
		{"/srv/debfactory/ci/work", "dfci", 0o700},
		{"/srv/debfactory/pub/work", "dfpub", 0o700},
		{"/srv/debfactory/data", "dfpub", 0o700},
		{"/srv/debfactory/rclone", "dfpub", 0o700},
		{"/srv/debfactory/cache-pub", "dfpub", 0o700},
		{"/var/lib/dfci", "dfci", 0o700},
		{"/var/lib/dfpub", "dfpub", 0o700},
		{"/etc/credstore", "root", 0o700},
		{ToolsDir, "root", 0o755},
	}
	for _, d := range dirs {
		fi, err := h.Stat(d.path)
		if err != nil {
			add(d.path, false, "no existe")
			continue
		}
		add(fmt.Sprintf("%s (%s, %o)", d.path, d.owner, d.perm), owner(fi) == d.owner && fi.Mode().Perm() == d.perm,
			fmt.Sprintf("%s, %o", owner(fi), fi.Mode().Perm()))
	}
	cs = append(cs, r.toolChecks(h, owner)...)
	if _, err := h.Stat("/etc/credstore/debfactory-gh-runner"); err != nil {
		cs = append(cs, Check{Name: "PAT en /etc/credstore/debfactory-gh-runner", Warn: true,
			Info: "no se puede comprobar sin root (el directorio es root:700); sudo ls -l /etc/credstore"})
	}
	if _, err := h.Stat("/etc/nftables.d/debfactory-ci.nft"); err != nil {
		add("regla nftables de dfci", false, "falta /etc/nftables.d/debfactory-ci.nft")
	} else {
		add("regla nftables de dfci instalada", true, "comprueba que está cargada: sudo nft list table inet debfactory_ci")
	}
	units := []string{"debfactory-runner@ci.service", "debfactory-runner@pub.service", "debfactory-refresh.timer"}
	if r.watchInstalled() {
		units = append(units, "debfactory-watch.timer")
	}
	for _, unit := range units {
		err := h.Run("systemctl", "is-enabled", "--quiet", unit)
		add(unit+" habilitado", err == nil, "")
	}
	return cs
}

// toolChecks comprueba que la versión que fija <root>/debfactory.version está
// instalada como root y sin escritura para nadie más, y avisa de las
// instaladas que no fija (004 research D3, D12).
func (r *Runner) toolChecks(h Host, owner func(fs.FileInfo) string) []Check {
	var cs []Check
	pin, err := ReadTool(r.Root)
	if err != nil {
		return []Check{{Name: "versión fijada de la herramienta", Warn: true,
			Info: fmt.Sprintf("no hay %s válido en %s (¿--root apunta al repositorio de paquetes?)", ToolFile, r.Root)}}
	}
	name := "debfactory " + pin[:12] + " instalada"
	bin := ToolsDir + "/" + pin + "/debfactory"
	switch fi, err := h.Stat(bin); {
	case err != nil:
		cs = append(cs, Check{Name: name, Info: "falta " + bin + ": git checkout " + pin + " && just tool-install"})
	case owner(fi) != "root" || fi.Mode().Perm()&0o022 != 0:
		cs = append(cs, Check{Name: name, Info: fmt.Sprintf("%s es de %s con permisos %o; debe ser de root y sin escritura para grupo ni otros",
			bin, owner(fi), fi.Mode().Perm())})
	default:
		cs = append(cs, Check{Name: name, OK: true})
	}
	if h.ReadDir == nil {
		return cs
	}
	entries, _ := h.ReadDir(ToolsDir)
	var unused []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != pin {
			unused = append(unused, e.Name()[:min(12, len(e.Name()))])
		}
	}
	if len(unused) > 0 {
		cs = append(cs, Check{Name: "versiones de la herramienta sin fijar", Warn: true,
			Info: strings.Join(unused, " ") + ": no fijadas por main; revisa los PRs abiertos antes de borrarlas (sudo rm -r " + ToolsDir + "/<commit>)"})
	}
	return cs
}
