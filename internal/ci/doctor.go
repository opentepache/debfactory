package ci

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"debfactory/internal/forge"
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
	Home       string
}

// RealHost es la PC real.
func RealHost() Host {
	home, _ := os.UserHomeDir()
	return Host{
		Stat:       os.Stat,
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

// GitHubChecks comprueba la configuración del repositorio en GitHub.
func (r *Runner) GitHubChecks(ctx context.Context, gh *forge.Client, fix bool) []Check {
	var cs []Check
	add := func(name string, ok bool, info string) { cs = append(cs, Check{Name: name, OK: ok, Info: info}) }
	// Un repositorio privado no tiene aprobación de forks (GitHub responde
	// 422); en su lugar, los forks MUST NOT ejecutar workflows.
	repo, err := gh.GetRepo(ctx)
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
		ok, info := false, "no hay un ruleset activo sobre main que exija PR, bloquee force push y borrado y no tenga bypass"
		for _, s := range rs {
			if s.Target != "branch" || s.Enforcement != "active" || len(s.BypassActors) > 0 {
				continue
			}
			inc := s.Conditions.RefName.Include
			if !slices.Contains(inc, "~DEFAULT_BRANCH") && !slices.Contains(inc, "refs/heads/main") && !slices.Contains(inc, "~ALL") {
				continue
			}
			var types []string
			for _, rule := range s.Rules {
				types = append(types, rule.Type)
			}
			if slices.Contains(types, "pull_request") && slices.Contains(types, "non_fast_forward") && slices.Contains(types, "deletion") {
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
	return cs
}

// runnerVersionCheck avisa si RUNNER_VERSION lleva más de 14 días por detrás
// del último release de actions/runner (GitHub rechaza los atrasados unos 30).
func (r *Runner) runnerVersionCheck(ctx context.Context, gh *forge.Client) Check {
	name := "versión del runner (ci/runner/runner.env)"
	pinned := readEnvFile(filepath.Join(r.Root, "ci/runner/runner.env"))["RUNNER_VERSION"]
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

func readEnvFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
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
	if _, err := h.Stat("/etc/credstore/debfactory-gh-runner"); err != nil {
		cs = append(cs, Check{Name: "PAT en /etc/credstore/debfactory-gh-runner", Warn: true,
			Info: "no se puede comprobar sin root (el directorio es root:700); sudo ls -l /etc/credstore"})
	}
	if _, err := h.Stat("/etc/nftables.d/debfactory-ci.nft"); err != nil {
		add("regla nftables de dfci", false, "falta /etc/nftables.d/debfactory-ci.nft")
	} else {
		add("regla nftables de dfci instalada", true, "comprueba que está cargada: sudo nft list table inet debfactory_ci")
	}
	for _, unit := range []string{"debfactory-runner@ci.service", "debfactory-runner@pub.service", "debfactory-refresh.timer"} {
		err := h.Run("systemctl", "is-enabled", "--quiet", unit)
		add(unit+" habilitado", err == nil, "")
	}
	return cs
}
