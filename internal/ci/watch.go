package ci

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/forge"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/redact"
	"debfactory/internal/report"
	"debfactory/internal/watch"
)

// Detector de versiones upstream: `ci watch` (specs/005-upstream-release-watch,
// contracts/cli.md). Corre en el runner ci, sin <data> ni llave (FR-012), con
// el token de la GitHub App para las lecturas de upstream, las ramas, los PR y
// el auto-merge, y con el GITHUB_TOKEN del job para las notificaciones. No
// tiene estado propio salvo la caché de Actions (research D7, D9).

// watchState es lo que se guarda entre ciclos en la caché de Actions
// ($RUNNER_TEMP/debfactory-watch/state.json). Si se pierde, el ciclo
// siguiente es completo y todas las consultas se hacen sin ETag: nada se
// rompe.
type watchState struct {
	LastCycle time.Time                 `json:"last_cycle"`         // inicio del último ciclo completo
	Pending   []string                  `json:"pending,omitempty"`  // paquetes sin revisar por el límite
	Stopped   int                       `json:"stopped,omitempty"`  // ciclos seguidos detenidos por el límite
	Releases  map[string]cachedReleases `json:"releases,omitempty"` // upstream -> última respuesta
}

type cachedReleases struct {
	ETag string          `json:"etag"`
	Body json.RawMessage `json:"body"`
}

// watchIssues son las notificaciones del detector: buscar (siempre), abrir y
// cerrar (que --dry-run solo describe).
type watchIssues interface {
	find(ctx context.Context, match func(Key) bool) (*forge.Issue, Key, bool)
	Fail(ctx context.Context, e *report.Error)
	close(ctx context.Context, match func(Key) bool, comment string)
}

// dryIssues lee los issues de verdad pero no escribe nada.
type dryIssues struct {
	gh  *GitHubNotifier
	out *report.Printer
}

func (d dryIssues) find(ctx context.Context, match func(Key) bool) (*forge.Issue, Key, bool) {
	return d.gh.find(ctx, match)
}

func (d dryIssues) Fail(_ context.Context, e *report.Error) {
	d.out.Line("watch: (dry-run) notificaría «%s»: %s", KeyOf(e).Title(), e.Cause)
}

func (d dryIssues) close(ctx context.Context, match func(Key) bool, comment string) {
	if is, k, ok := d.gh.find(ctx, match); ok {
		d.out.Line("watch: (dry-run) cerraría #%d (%s): %s", is.Number, k.Title(), comment)
	}
}

// watcher es el estado de un ciclo.
type watcher struct {
	*Runner
	dry       bool
	app       *forge.Client // token de la App
	issues    watchIssues
	state     *watchState
	statePath string // "" = no se guarda (--dry-run o fuera de un job)
	now       time.Time
	tmp       string
	prefix    string
	tokens    []string
	gitAuth   []string // entorno de git con el token de la App (nunca en argv ni en disco)
	botName   string
	botEmail  string

	queries, notModified int
	errs                 report.Errors
	table                []string // filas del resumen en Markdown
}

// Watch es `ci watch [--package P] [--force] [--dry-run]`.
func (r *Runner) Watch(ctx context.Context, o Options) error {
	only, force := o.Package, o.Force
	if r.Env.Actions {
		pkg, f, err := r.Env.WatchInputs()
		if err != nil {
			return err
		}
		if only == "" {
			only = pkg
		}
		force = force || f
	}
	if only != "" && !pkgNameRe.MatchString(only) {
		return &InputError{fmt.Sprintf("--package %q: nombre de paquete inválido", only)}
	}
	if o.DryRun {
		if err := r.Cfg.CI.RequireRepository(); err != nil {
			return err
		}
	} else if err := r.Env.RequireWatcher(r.Cfg); err != nil {
		return err
	}
	w, err := r.newWatcher(o.DryRun)
	if err != nil {
		return err
	}
	defer os.RemoveAll(w.tmp)
	return w.cycle(ctx, only, force)
}

func (r *Runner) newWatcher(dry bool) (*watcher, error) {
	appToken, issuesToken := r.Env.AppToken, r.Env.Token
	if dry {
		// En local basta un token de lectura (GH_TOKEN); sin token, la API
		// pública permite 60 peticiones por hora.
		for _, t := range []string{r.Env.AppToken, os.Getenv("GH_TOKEN"), r.Env.Token} {
			if t != "" {
				appToken = t
				break
			}
		}
		issuesToken = appToken
	}
	app, err := forge.New(r.Env.APIURL, r.Cfg.CI.Repository, appToken)
	if err != nil {
		return nil, &report.EnvError{Cause: err.Error()}
	}
	issuesGH, err := forge.New(r.Env.APIURL, r.Cfg.CI.Repository, issuesToken)
	if err != nil {
		return nil, &report.EnvError{Cause: err.Error()}
	}
	tmp, err := os.MkdirTemp(r.Env.RunnerTemp, "debfactory-watch-*")
	if err != nil {
		return nil, err
	}
	w := &watcher{Runner: r, dry: dry, app: app, now: r.Repo.NowTime().UTC(), tmp: tmp,
		prefix: r.Cfg.Watch.Prefix(), tokens: []string{appToken, issuesToken}}
	gh := &GitHubNotifier{GH: issuesGH, Label: r.Cfg.CI.FailureLabel, RunURL: r.Env.RunURL(), Commit: r.Env.SHA,
		Out: r.Out, Token: issuesToken}
	if dry {
		w.issues = dryIssues{gh: gh, out: r.Out}
	} else {
		w.issues = gh
		if r.Env.RunnerTemp != "" {
			w.statePath = filepath.Join(r.Env.RunnerTemp, "debfactory-watch", "state.json")
		}
		w.botName = r.Env.AppSlug + "[bot]"
		header := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + r.Env.AppToken))
		w.gitAuth = []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + header}
		w.tokens = append(w.tokens, header)
	}
	w.state = w.loadState()
	return w, nil
}

func (w *watcher) loadState() *watchState {
	st := &watchState{Releases: map[string]cachedReleases{}}
	p := w.statePath
	if p == "" && w.Env.RunnerTemp != "" {
		p = filepath.Join(w.Env.RunnerTemp, "debfactory-watch", "state.json") // --dry-run en un job: solo lectura
	}
	if p == "" {
		return st
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return st
	}
	if json.Unmarshal(data, st) != nil {
		w.Out.Line("watch: la caché %s es ilegible; se empieza de cero", p)
		return &watchState{Releases: map[string]cachedReleases{}}
	}
	if st.Releases == nil {
		st.Releases = map[string]cachedReleases{}
	}
	return st
}

func (w *watcher) saveState() {
	if w.statePath == "" {
		return
	}
	data, err := json.Marshal(w.state)
	if err == nil {
		if err = os.MkdirAll(filepath.Dir(w.statePath), 0o755); err == nil {
			err = os.WriteFile(w.statePath, data, 0o644)
		}
	}
	if err != nil {
		w.Out.Line("watch: no se pudo guardar la caché: %v", err)
	}
}

// clean quita los tokens de un texto que va a un issue o al log.
func (w *watcher) clean(s string) string { return redact.String(s, w.tokens...) }

// cycleErr es un fallo del propio ciclo (no de un paquete): sale con código
// 1 y se notifica como pipeline/watch.
func (w *watcher) cycleErr(pkg string, err error) {
	e := report.Wrap(err, report.Watch, pkg, "", "")
	e.Cause = w.clean(e.Cause)
	w.Out.Fail(e)
	w.errs = append(w.errs, e)
}

// say imprime la decisión de un paquete (data-model.md §7) y la apunta para
// el resumen del job.
func (w *watcher) say(pkg, from, to, decision string) {
	ver := from
	if to != "" {
		ver += " → " + to
	}
	w.Out.Event("watch", map[string]any{"package": pkg, "from": from, "to": to, "decision": decision},
		fmt.Sprintf("watch: %s %s: %s", pkg, ver, decision))
	w.table = append(w.table, fmt.Sprintf("| %s | %s | %s | %s |", pkg, from, to, mdEscape(decision)))
}

func fmtTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

// fmtDuration escribe "48 h" para horas exactas.
func fmtDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d h", d/time.Hour)
	}
	return d.String()
}

// cycle revisa los paquetes (contracts/cli.md § Pasos por ciclo).
func (w *watcher) cycle(ctx context.Context, only string, force bool) error {
	full := only == ""
	if full && !force && !w.state.LastCycle.IsZero() {
		if next := w.state.LastCycle.Add(w.Cfg.Watch.IntervalDuration()); w.now.Before(next) {
			w.Out.Line("watch: ciclo no vencido (último ciclo completo %s; el siguiente a partir de %s)",
				fmtTime(w.state.LastCycle), fmtTime(next))
			w.saveState()
			return nil
		}
	}
	names, err := build.ListPackages(w.Root)
	if err != nil {
		return err
	}
	if !full {
		if !slices.Contains(names, only) {
			return &InputError{fmt.Sprintf("--package %q: no existe %s", only, manifest.PathIn(w.Root, only))}
		}
		names = []string{only}
	}
	names = pendingFirst(names, w.state.Pending)
	if err := w.fetch(ctx); err != nil {
		if !w.dry {
			w.cycleErr("", err)
			return w.finish(ctx, full, nil, false, false)
		}
		w.Out.Line("watch: (dry-run) sin git fetch (%v); se usan las ramas locales", firstLine(err.Error()))
	}
	prs, err := w.app.ListOpenPRs(ctx, "main")
	if err != nil {
		if forge.IsRateLimit(err) {
			return w.finish(ctx, full, names, true, false)
		}
		w.cycleErr("", err)
		return w.finish(ctx, full, nil, false, false)
	}
	byBranch := map[string]*forge.PR{}
	for i := range prs {
		if strings.HasPrefix(prs[i].Head.Ref, w.prefix) {
			byBranch[prs[i].Head.Ref] = &prs[i]
		}
	}
	for i, name := range names {
		if rate := w.app.Rate(); rate.Known && rate.Remaining <= w.Cfg.Watch.Reserve() {
			return w.finish(ctx, full, names[i:], true, true)
		}
		if limited := w.watchPackage(ctx, name, byBranch[w.prefix+name]); limited {
			return w.finish(ctx, full, names[i:], true, true)
		}
	}
	return w.finish(ctx, full, nil, false, true)
}

// pendingFirst ordena los paquetes con los pendientes del ciclo anterior
// delante (research D9).
func pendingFirst(names, pending []string) []string {
	var first, rest []string
	for _, n := range names {
		if slices.Contains(pending, n) {
			first = append(first, n)
		} else {
			rest = append(rest, n)
		}
	}
	return append(first, rest...)
}

// finish cierra el ciclo: estado, avisos de límite y de fallo del ciclo,
// resumen y código de salida. ran indica que se llegó a revisar paquetes: si
// falló algo global (git, la lista de PRs), el ciclo no cuenta como completo y
// se repite en el disparo siguiente.
func (w *watcher) finish(ctx context.Context, full bool, pending []string, stopped, ran bool) error {
	for _, n := range pending {
		w.say(n, "", "", "pendiente-límite")
	}
	if full {
		if stopped {
			w.state.Stopped++
			w.state.Pending = pending
		} else if ran {
			// Un ciclo con errores de paquetes también cuenta como completo:
			// se notifican y se reintentan al vencer watch.interval, no cada
			// hora (un upstream que da 404 no debe comentar el issue cada hora).
			w.state.Stopped, w.state.Pending, w.state.LastCycle = 0, nil, w.now
		}
		rlKey := func(k Key) bool { return k.Package == "pipeline" && k.Stage == string(report.WatchRateLimit) }
		switch {
		case stopped && w.state.Stopped >= 3:
			if _, _, open := w.issues.find(ctx, rlKey); !open {
				w.issues.Fail(ctx, &report.Error{Package: "pipeline", Version: "-", Stage: report.WatchRateLimit,
					Cause: fmt.Sprintf("%d ciclos seguidos detenidos por el límite de peticiones de la API (watch.rate_reserve = %d)",
						w.state.Stopped, w.Cfg.Watch.Reserve()),
					Detail: []string{"pendientes: " + strings.Join(pending, ", ")}})
			}
		case !stopped && ran:
			w.issues.close(ctx, rlKey, fmt.Sprintf("resuelto por %s: ciclo completo sin detenerse", w.Env.RunURL()))
		}
	}
	pipeKey := func(k Key) bool { return k.Package == "pipeline" && k.Stage == string(report.Watch) }
	if len(w.errs) > 0 {
		var causes []string
		for _, e := range w.errs {
			causes = append(causes, strings.TrimPrefix(e.Error(), "debfactory: "))
		}
		w.issues.Fail(ctx, &report.Error{Package: "pipeline", Version: "-", Stage: report.Watch,
			Cause: "el ciclo del detector de versiones falló", Detail: causes})
	} else if full && !stopped && ran {
		w.issues.close(ctx, pipeKey, fmt.Sprintf("resuelto por %s: ciclo completo", w.Env.RunURL()))
	}
	rate := w.app.Rate()
	tail := fmt.Sprintf("watch: consultas de releases %d (%d × 304)", w.queries, w.notModified)
	if rate.Known {
		tail += fmt.Sprintf("; restantes %d", rate.Remaining)
	}
	if stopped {
		tail += fmt.Sprintf("; ciclo detenido por el límite (reserva %d), %d paquetes pendientes", w.Cfg.Watch.Reserve(), len(pending))
	}
	w.Out.Line("%s", tail)
	var b strings.Builder
	b.WriteString("### debfactory ci watch\n\n| paquete | publicada | nueva | decisión |\n|---|---|---|---|\n")
	for _, row := range w.table {
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + strings.TrimPrefix(tail, "watch: ") + "\n")
	w.summary(b.String())
	w.saveState()
	return w.errs.Err()
}

// watchPackage decide qué hacer con un paquete. Devuelve true si la API
// rechazó la consulta por el límite (el paquete queda pendiente).
func (w *watcher) watchPackage(ctx context.Context, name string, pr *forge.PR) (rateLimited bool) {
	m, err := manifest.Load(w.Root, name)
	if err != nil {
		w.cycleErr(name, err)
		w.say(name, "", "", "error: manifiesto inválido en main")
		return false
	}
	if !m.Watch.Watched() {
		w.say(m.Name, m.Version, "", "omitido (watch.enabled: false)")
		return false
	}
	if m.Level2() {
		// El detector no maneja todavía el nivel 2 (specs/008-source-builds,
		// spec: fuera de alcance): sus versiones se suben a mano.
		w.say(m.Name, m.Version, "", "omitido (nivel 2: el detector no lo maneja todavía)")
		return false
	}
	rels, err := w.releases(ctx, m.Upstream)
	if forge.IsRateLimit(err) {
		return true
	}
	if err != nil {
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, "", "error al consultar los releases")
		return false
	}
	cand, waiting := watch.Pick(m.Version, m.Tag, m.Watch.IgnorePrereleaseFlag(), rels, w.now, w.Cfg.Watch.CooldownDuration())
	if w.dry {
		w.explain(m, rels)
	}
	if cand == nil {
		w.resolve(ctx, m.Name, "", "ya no hay ninguna versión más nueva que la publicada")
		if pr != nil {
			w.closeStale(ctx, m, pr)
			return false
		}
		if waiting != nil {
			w.say(m.Name, m.Version, waiting.Version, "en-espera (elegible "+fmtTime(waiting.Eligible)+")")
		} else {
			w.say(m.Name, m.Version, "", "sin-cambios")
		}
		return false
	}
	// Las notificaciones de otras versiones ya no aplican (FR-019).
	w.resolve(ctx, m.Name, cand.To, cand.To+" es ahora la versión candidata")
	if is, k, ok := w.issues.find(ctx, func(k Key) bool {
		return k.Package == m.Name && k.Version == cand.To && strings.HasPrefix(k.Stage, "watch-")
	}); ok {
		if k.Stage == string(report.WatchTampered) || !w.manifestChangedSince(ctx, m.Name, is.CreatedAt) {
			w.say(m.Name, m.Version, cand.To, fmt.Sprintf("ya-notificado #%d (%s)", is.Number, k.Stage))
			return false
		}
	}
	branch := w.prefix + m.Name
	var branchVersion string
	if pr != nil {
		own, err := w.ownBranch(ctx, branch)
		if err != nil {
			w.cycleErr(m.Name, err)
			w.say(m.Name, m.Version, cand.To, "error al leer la rama "+branch)
			return false
		}
		if !own {
			w.commentNewer(ctx, pr, cand.To)
			w.say(m.Name, m.Version, cand.To, fmt.Sprintf("comentado #%d (la rama tiene commits ajenos)", pr.Number))
			return false
		}
		branchVersion = w.branchVersion(ctx, branch, m.Name)
		if branchVersion == cand.To && w.basedOnMain(ctx, branch) {
			if w.tamperedByDigest(ctx, m, cand, branch) {
				return false
			}
			w.say(m.Name, m.Version, cand.To, fmt.Sprintf("pr-al-día #%d", pr.Number))
			return false
		}
	}
	if w.dry {
		action := "abriría un PR"
		if pr != nil {
			action = fmt.Sprintf("actualizaría el PR #%d", pr.Number)
		}
		w.say(m.Name, m.Version, cand.To, "(dry-run) "+action)
		return false
	}
	w.propose(ctx, m, cand, pr, branch, branchVersion)
	return false
}

// releases consulta los releases de upstream con la caché de ETag.
func (w *watcher) releases(ctx context.Context, upstream string) ([]watch.Release, error) {
	cached := w.state.Releases[upstream]
	etag := cached.ETag
	if len(cached.Body) == 0 {
		etag = ""
	}
	body, newETag, notModified, err := w.app.ListReleases(ctx, upstream, etag)
	w.queries++
	if err != nil {
		return nil, err
	}
	if notModified {
		w.notModified++
		body = cached.Body
	} else if newETag != "" {
		w.state.Releases[upstream] = cachedReleases{ETag: newETag, Body: body}
	}
	rs, err := forge.ParseReleases(body)
	if err != nil {
		delete(w.state.Releases, upstream)
		return nil, err
	}
	out := make([]watch.Release, 0, len(rs))
	for _, r := range rs {
		wr := watch.Release{Tag: r.TagName, Draft: r.Draft, Prerelease: r.Prerelease, PublishedAt: r.PublishedAt,
			URL: r.HTMLURL, Digests: map[string]string{}}
		for _, a := range r.Assets {
			wr.Assets = append(wr.Assets, a.Name)
			if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
				wr.Digests[a.Name] = d
			}
		}
		out = append(out, wr)
	}
	return out, nil
}

// explain detalla en --dry-run por qué se descarta cada release reciente
// (plan, riesgo 3: comprobar la plantilla tag y el filtro con datos reales).
func (w *watcher) explain(m *manifest.Manifest, rels []watch.Release) {
	for i, r := range rels {
		if i == 5 {
			break
		}
		v, reason := watch.Stable(r, m.Tag, m.Watch.IgnorePrereleaseFlag())
		if reason == "" {
			reason = "estable " + v
		}
		w.Out.Line("watch:   %s %s (%s): %s", m.Name, r.Tag, r.PublishedAt.UTC().Format(time.DateOnly), reason)
	}
}

// resolve cierra las notificaciones watch-* del paquete salvo las de la
// versión except y las de posible manipulación (FR-019).
func (w *watcher) resolve(ctx context.Context, pkg, except, note string) {
	w.issues.close(ctx, func(k Key) bool {
		return k.Package == pkg && k.Version != except && strings.HasPrefix(k.Stage, "watch-") &&
			k.Stage != string(report.WatchTampered)
	}, fmt.Sprintf("resuelto por %s: %s", w.Env.RunURL(), note))
}

// notify abre la notificación de un paquete (FR-016, FR-017).
func (w *watcher) notify(ctx context.Context, m *manifest.Manifest, to string, stage report.Stage, cause string, detail ...string) {
	for i := range detail {
		detail[i] = w.clean(detail[i])
	}
	w.issues.Fail(ctx, &report.Error{Package: m.Name, Version: to, Stage: stage, Cause: w.clean(cause), Detail: detail})
	n := ""
	if is, _, ok := w.issues.find(ctx, func(k Key) bool { return k.Package == m.Name && k.Version == to && k.Stage == string(stage) }); ok {
		n = fmt.Sprintf(" #%d", is.Number)
	}
	w.say(m.Name, m.Version, to, "notificado "+string(stage)+n)
}

// --- git ---

func (w *watcher) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Nunca pide credenciales por terminal (en local, un remoto por SSH con
	// passphrase falla en vez de quedarse esperando).
	cmd.Env = append(append(gitEnv(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"), w.gitAuth...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], w.clean(firstLine(msg)))
	}
	return strings.TrimSpace(out.String()), nil
}

// fetch trae main y las ramas del detector (con --prune, para olvidar las
// borradas).
func (w *watcher) fetch(ctx context.Context) error {
	_, err := w.git(ctx, w.Root, "fetch", "-q", "--prune", "origin",
		"+refs/heads/main:refs/remotes/origin/main",
		"+refs/heads/"+w.prefix+"*:refs/remotes/origin/"+w.prefix+"*")
	return err
}

func (w *watcher) remoteSHA(ctx context.Context, branch string) string {
	sha, _ := w.git(ctx, w.Root, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+branch)
	return sha
}

// ownBranch indica si todos los commits de la rama que no están en main son
// del detector (FR-009).
func (w *watcher) ownBranch(ctx context.Context, branch string) (bool, error) {
	if w.remoteSHA(ctx, branch) == "" {
		return true, nil // el PR existe pero la rama no: se vuelve a crear
	}
	out, err := w.git(ctx, w.Root, "log", "--format=%an", "origin/main..origin/"+branch)
	if err != nil {
		return false, err
	}
	bot := w.botName
	if w.dry {
		bot = w.Env.AppSlug + "[bot]"
		if w.Env.AppSlug == "" {
			return true, nil // en local no se sabe el nombre de la App
		}
	}
	for _, a := range strings.Split(out, "\n") {
		if a != "" && a != bot {
			return false, nil
		}
	}
	return true, nil
}

// branchVersion es la versión del manifiesto en la rama del PR ("" si no se
// puede leer).
func (w *watcher) branchVersion(ctx context.Context, branch, name string) string {
	data, err := (&StatePR{Root: w.Root}).show(ctx, "origin/"+branch, manifest.PathIn(w.Root, name))
	if err != nil || data == nil {
		return ""
	}
	m, err := manifest.Parse(data, manifest.PathIn(w.Root, name))
	if err != nil {
		return ""
	}
	return m.Version
}

// basedOnMain indica si la rama parte de la punta actual de main (si no, se
// regenera: spec, Edge Cases).
func (w *watcher) basedOnMain(ctx context.Context, branch string) bool {
	_, err := w.git(ctx, w.Root, "merge-base", "--is-ancestor", "origin/main", "origin/"+branch)
	return err == nil
}

// manifestChangedSince indica si el manifiesto del paquete cambió en main
// después de t (FR-018: una notificación deja de suprimir los reintentos).
func (w *watcher) manifestChangedSince(ctx context.Context, name string, t time.Time) bool {
	out, err := w.git(ctx, w.Root, "log", "-1", "--first-parent", "--format=%cI", "origin/main", "--", manifest.PathIn(w.Root, name))
	if err != nil || out == "" {
		return false
	}
	changed, err := time.Parse(time.RFC3339, out)
	return err == nil && changed.After(t)
}

// --- propuesta ---

// proposal es una versión nueva preparada en un worktree de main.
type proposal struct {
	dir     string
	entry   *lock.Entry
	license watch.LicenseResult
}

// propose prepara la versión nueva, la empuja a la rama del detector y abre o
// actualiza el PR (research D7, D11).
func (w *watcher) propose(ctx context.Context, m *manifest.Manifest, cand *watch.Candidate, pr *forge.PR, branch, branchVersion string) {
	if missing, expected := w.missingAssets(m, cand); len(missing) > 0 {
		w.notify(ctx, m, cand.To, report.WatchAssets, "faltan assets que espera el manifiesto: "+strings.Join(missing, ", "),
			"esperados:   "+strings.Join(expected, ", "), "encontrados: "+strings.Join(cand.Assets, ", "))
		return
	}
	p := w.prepare(ctx, m, cand)
	if p == nil {
		return
	}
	defer w.git(context.Background(), w.Root, "worktree", "remove", "--force", p.dir)
	if pr != nil && branchVersion == cand.To {
		if old := w.branchLock(ctx, branch, m.Name, cand.To); old != nil && !old.Equal(p.entry) {
			w.notify(ctx, m, cand.To, report.WatchTampered,
				fmt.Sprintf("los hashes de %s no son los del PR #%d: upstream resubió assets con la misma etiqueta (posible manipulación)", cand.To, pr.Number),
				lockDiff(old, p.entry)...)
			return
		}
	}
	major := watch.IsMajor(cand.From, cand.To)
	auto, why := w.autoMerge(m, major, p.license)
	title := fmt.Sprintf("%s: %s → %s", m.Name, cand.From, cand.To)
	body := w.prBody(m, cand, major, p.license, why)
	if err := w.commitAndPush(ctx, p.dir, m.Name, branch, title); err != nil {
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, cand.To, "error al empujar la rama")
		return
	}
	decision := ""
	if pr == nil {
		created, err := w.app.CreatePR(ctx, branch, "main", title, body)
		if err != nil {
			w.cycleErr(m.Name, err)
			w.say(m.Name, m.Version, cand.To, "error al abrir el PR")
			return
		}
		pr, decision = created, fmt.Sprintf("pr-abierto #%d", created.Number)
	} else {
		if err := w.app.UpdatePR(ctx, pr.Number, title, body); err != nil {
			w.cycleErr(m.Name, err)
			w.say(m.Name, m.Version, cand.To, "error al actualizar el PR")
			return
		}
		decision = fmt.Sprintf("pr-actualizado #%d", pr.Number)
	}
	switch {
	case auto && pr.AutoMerge != nil:
		decision += " (auto-merge)"
	case auto:
		if err := w.app.EnableAutoMerge(ctx, pr.NodeID); err != nil {
			w.cycleErr(m.Name, fmt.Errorf("no se pudo activar el auto-merge del PR #%d (¿Allow auto-merge desactivado en el repositorio?): %v", pr.Number, err))
		} else {
			decision += " (auto-merge)"
		}
	case pr.AutoMerge != nil:
		if err := w.app.DisableAutoMerge(ctx, pr.NodeID); err != nil {
			w.cycleErr(m.Name, fmt.Errorf("no se pudo desactivar el auto-merge del PR #%d: %v", pr.Number, err))
		}
	}
	w.resolve(ctx, m.Name, "", fmt.Sprintf("el PR #%d propone %s", pr.Number, cand.To))
	w.say(m.Name, m.Version, cand.To, decision)
}

// missingAssets comprueba, sin descargar, que el release tiene los assets que
// el manifiesto espera para la versión nueva (FR-016 b).
func (w *watcher) missingAssets(m *manifest.Manifest, cand *watch.Candidate) (missing, expected []string) {
	mm := *m
	mm.Version = cand.To
	want := func(tmpl, arch string) {
		u, err := mm.RenderURL(tmpl, arch)
		if err != nil {
			return
		}
		a := path.Base(u)
		if !slices.Contains(expected, a) {
			expected = append(expected, a)
		}
	}
	for _, arch := range w.Cfg.Arches {
		want(m.Artifact.URL, arch)
	}
	if m.Checksums != nil {
		want(m.Checksums.URL, "")
	}
	for _, a := range expected {
		if !slices.Contains(cand.Assets, a) {
			missing = append(missing, a)
		}
	}
	return missing, expected
}

// prepare crea un worktree de main con la versión nueva y genera su entrada
// del lockfile con la orden existente (build.Env.Lock), con un gate de
// licencias que clasifica el cambio (research D8). Si algo falla, notifica o
// apunta un fallo del ciclo y devuelve nil.
func (w *watcher) prepare(ctx context.Context, m *manifest.Manifest, cand *watch.Candidate) *proposal {
	dir := filepath.Join(w.tmp, "wt-"+m.Name)
	if _, err := w.git(ctx, w.Root, "worktree", "add", "-q", "--detach", dir, "origin/main"); err != nil {
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, cand.To, "error al preparar el worktree")
		return nil
	}
	p := &proposal{dir: dir}
	fail := func() *proposal {
		w.git(context.Background(), w.Root, "worktree", "remove", "--force", dir)
		return nil
	}
	rel := manifest.PathIn(w.Root, m.Name)
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err == nil {
		data, err = setVersion(data, rel, cand.To)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, rel), data, 0o644)
	}
	if err != nil {
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, cand.To, "error al cambiar la versión del manifiesto")
		return fail()
	}
	b := w.Build()
	b.Root = dir
	b.Workdir = filepath.Join(w.tmp, "work")
	var quiet bytes.Buffer
	b.Out = &report.Printer{Out: &quiet, Err: &quiet}
	b.LicenseCheck = func(text []byte, declared string, cfg *config.Config) error {
		p.license = watch.LicenseChange(text, declared, cfg)
		if p.license.Kind == watch.LicenseBlocked {
			return report.New(report.License, p.license.Reason)
		}
		return nil
	}
	if err := b.Lock(ctx, m.Name, false); err != nil {
		w.lockFailed(ctx, m, cand, p.license, err)
		return fail()
	}
	lk, err := lock.Read(dir, m.Name)
	if err == nil && lk.Versions[cand.To] == nil {
		err = fmt.Errorf("%s: falta la entrada de %s", lock.PathIn(w.Root, m.Name), cand.To)
	}
	if err != nil {
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, cand.To, "error al leer el lockfile nuevo")
		return fail()
	}
	p.entry = lk.Versions[cand.To]
	if p.license.Kind == watch.LicenseChanged {
		data, err := setLicense(data, rel, p.license.SPDX)
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, rel), data, 0o644)
		}
		if err != nil {
			w.cycleErr(m.Name, err)
			w.say(m.Name, m.Version, cand.To, "error al cambiar license.spdx")
			return fail()
		}
	}
	return p
}

// lockFailed traduce un fallo de la orden de lockfile a su notificación
// (FR-016) o a un fallo del ciclo si puede ser pasajero.
func (w *watcher) lockFailed(ctx context.Context, m *manifest.Manifest, cand *watch.Candidate, lic watch.LicenseResult, err error) {
	var e *report.Error
	if !errors.As(err, &e) {
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, cand.To, "error al generar el lockfile")
		return
	}
	switch {
	case e.Stage == report.License:
		detail := []string{"declarada: " + m.License.SPDX}
		if lic.Kind == watch.LicenseBlocked {
			detail = append(detail, fmt.Sprintf("detectada: %s (cobertura %.0f %%)", dash(lic.Detected), lic.Coverage))
		}
		w.notify(ctx, m, cand.To, report.WatchLicense, "licencia de "+cand.To+": "+e.Cause, detail...)
	case e.Stage == report.Verify:
		w.notify(ctx, m, cand.To, report.WatchChecksums, e.Cause, e.Detail...)
	case e.Stage == report.Lock && strings.Contains(e.Cause, "cambiaron"):
		w.notify(ctx, m, cand.To, report.WatchTampered,
			"main ya tiene en el lockfile otros hashes para "+cand.To+" (posible manipulación)", e.Detail...)
	case e.Stage == report.Fetch && strings.Contains(e.Cause, "HTTP 404") && strings.Contains(e.Cause, "raw.githubusercontent.com"):
		w.notify(ctx, m, cand.To, report.WatchLicense, "no se encuentra el texto de licencia que declara el manifiesto en "+cand.Tag,
			e.Cause)
	case e.Stage == report.Fetch && strings.Contains(e.Cause, "HTTP 404"):
		w.notify(ctx, m, cand.To, report.WatchAssets, "un asset que espera el manifiesto no se puede descargar", e.Cause)
	default:
		// Descargas cortadas, la API caída…: se reintenta en el ciclo siguiente.
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, cand.To, "error al generar el lockfile (se reintenta en el próximo ciclo)")
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// branchLock lee la entrada de version del lockfile de la rama del PR.
func (w *watcher) branchLock(ctx context.Context, branch, name, version string) *lock.Entry {
	data, err := (&StatePR{Root: w.Root}).show(ctx, "origin/"+branch, lock.PathIn(w.Root, name))
	if err != nil || data == nil {
		return nil
	}
	f, err := lock.Parse(data, lock.PathIn(w.Root, name))
	if err != nil {
		return nil
	}
	return f.Versions[version]
}

// tamperedByDigest compara, sin descargar, el sha256 que la API publica de
// cada asset con el del lockfile del PR abierto (FR-016 d).
func (w *watcher) tamperedByDigest(ctx context.Context, m *manifest.Manifest, cand *watch.Candidate, branch string) bool {
	old := w.branchLock(ctx, branch, m.Name, cand.To)
	if old == nil || len(cand.Digests) == 0 {
		return false
	}
	mm := *m
	mm.Version = cand.To
	var diffs []string
	for _, arch := range w.Cfg.Arches {
		u, err := mm.RenderURL(m.Artifact.URL, arch)
		if err != nil {
			continue
		}
		d := cand.Digests[path.Base(u)]
		if d != "" && old.Artifacts[arch] != "" && d != old.Artifacts[arch] {
			diffs = append(diffs, fmt.Sprintf("artifacts.%s: %s → %s", arch, old.Artifacts[arch], d))
		}
	}
	if len(diffs) == 0 {
		return false
	}
	w.notify(ctx, m, cand.To, report.WatchTampered,
		fmt.Sprintf("upstream resubió assets de %s con la misma etiqueta (posible manipulación); el PR no se toca", cand.Tag), diffs...)
	return true
}

func lockDiff(old, new *lock.Entry) []string {
	var out []string
	add := func(field, a, b string) {
		if a != b {
			out = append(out, fmt.Sprintf("%s: %s → %s", field, a, b))
		}
	}
	add("source_date_epoch", strconv.FormatInt(old.SourceDateEpoch, 10), strconv.FormatInt(new.SourceDateEpoch, 10))
	add("license_sha256", old.LicenseSHA256, new.LicenseSHA256)
	add("checksums_sha256", old.ChecksumsSHA256, new.ChecksumsSHA256)
	for _, a := range []string{"amd64", "arm64"} {
		add("artifacts."+a, old.Artifacts[a], new.Artifacts[a])
	}
	return out
}

// autoMerge decide el auto-merge (FR-013–FR-015a, data-model.md §4).
func (w *watcher) autoMerge(m *manifest.Manifest, major bool, lic watch.LicenseResult) (bool, string) {
	switch {
	case !m.Watch.AutoMerge:
		return false, "no (desactivado en el manifiesto)"
	case major:
		return false, "no (cambio de versión mayor)"
	case lic.Kind == watch.LicenseChanged:
		return false, fmt.Sprintf("no (cambio de licencia %s → %s)", lic.Declared, lic.SPDX)
	}
	return true, "sí: se integra solo cuando `verify` pase"
}

// prMarker identifica los PR del detector (contracts/pr-y-notificaciones.md).
func prMarker(pkg, version string) string {
	return fmt.Sprintf("<!-- debfactory-watch pkg=%s version=%s -->", pkg, version)
}

func (w *watcher) prBody(m *manifest.Manifest, cand *watch.Candidate, major bool, lic watch.LicenseResult, why string) string {
	var b strings.Builder
	b.WriteString(prMarker(m.Name, cand.To) + "\n")
	fmt.Fprintf(&b, "Versión nueva de **%s** propuesta por el detector de versiones.\n\n|  |  |\n|---|---|\n", m.Name)
	fmt.Fprintf(&b, "| Anterior | %s (`%s`) |\n", cand.From, m.DebVersion())
	fmt.Fprintf(&b, "| Nueva | %s → `%s-0~tepache1` |\n", cand.To, cand.To)
	fmt.Fprintf(&b, "| Notas | %s |\n", cand.NotesURL)
	fmt.Fprintf(&b, "| Publicada upstream | %s (espera de %s cumplida) |\n", fmtTime(cand.PublishedAt),
		fmtDuration(w.Cfg.Watch.CooldownDuration()))
	licText := m.License.SPDX + ", sin cambios"
	if lic.Kind == watch.LicenseChanged {
		licText = fmt.Sprintf("**cambia**: %s → %s (license.spdx actualizado)", lic.Declared, lic.SPDX)
	}
	fmt.Fprintf(&b, "| Licencia | %s |\n", licText)
	majorText := "no"
	if major {
		majorText = "**sí**"
	}
	fmt.Fprintf(&b, "| Cambio mayor | %s |\n", majorText)
	fmt.Fprintf(&b, "| Auto-merge | %s |\n", why)
	b.WriteString("\nSolo cambian `version`, `revision` y el lockfile")
	if lic.Kind == watch.LicenseChanged {
		b.WriteString(" (y `license.spdx`)")
	}
	b.WriteString("; el check `verify` construye, pasa lintian y prueba la instalación en todas las suites.\n")
	return b.String()
}

// commitAndPush confirma el cambio en el worktree como la App y lo empuja a la
// rama del detector con --force-with-lease (nunca a main).
func (w *watcher) commitAndPush(ctx context.Context, dir, name, branch, title string) error {
	if w.botEmail == "" {
		email := w.botName + "@users.noreply.github.com"
		if id, err := w.app.UserID(ctx, w.botName); err == nil && id > 0 {
			email = fmt.Sprintf("%d+%s", id, email)
		}
		w.botEmail = email
	}
	if _, err := w.git(ctx, dir, "add", manifest.PathIn(w.Root, name), lock.PathIn(w.Root, name)); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "commit", "-q", "-m", title,
		"-m", "Propuesto por el detector de versiones de debfactory (specs/005-upstream-release-watch).")
	cmd.Env = append(gitEnv(), "GIT_AUTHOR_NAME="+w.botName, "GIT_AUTHOR_EMAIL="+w.botEmail,
		"GIT_COMMITTER_NAME="+w.botName, "GIT_COMMITTER_EMAIL="+w.botEmail)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s", firstLine(string(out)))
	}
	lease := "--force-with-lease=refs/heads/" + branch + ":" + w.remoteSHA(ctx, branch)
	_, err := w.git(ctx, dir, "push", "-q", lease, "origin", "HEAD:refs/heads/"+branch)
	return err
}

// commentNewer avisa una sola vez por versión de que hay una más nueva y la
// rama tiene commits ajenos (FR-009).
func (w *watcher) commentNewer(ctx context.Context, pr *forge.PR, version string) {
	marker := fmt.Sprintf("<!-- debfactory-watch-newer version=%s -->", version)
	comments, err := w.app.ListComments(ctx, pr.Number)
	if err != nil {
		w.cycleErr("", err)
		return
	}
	for _, c := range comments {
		if strings.Contains(c.Body, marker) {
			return
		}
	}
	if w.dry {
		return
	}
	body := marker + "\n" + fmt.Sprintf("Hay una versión más nueva de upstream: **%s**. Esta rama tiene commits que no son del "+
		"detector, así que no la actualizo. Ciérrala para que el próximo ciclo proponga %s desde cero.", version, version)
	if err := w.app.CommentIssue(ctx, pr.Number, body); err != nil {
		w.cycleErr("", err)
	}
}

// closeStale cierra el PR del detector cuya versión ya no es candidata
// (retirada upstream o main ya la tiene) y borra la rama si es suya.
func (w *watcher) closeStale(ctx context.Context, m *manifest.Manifest, pr *forge.PR) {
	branch := w.prefix + m.Name
	proposed := w.branchVersion(ctx, branch, m.Name)
	if w.dry {
		w.say(m.Name, m.Version, proposed, fmt.Sprintf("(dry-run) cerraría el PR #%d", pr.Number))
		return
	}
	note := fmt.Sprintf("Cierro este PR: %s ya no es una versión candidata de %s (upstream retiró el release o main ya tiene %s).",
		dash(proposed), m.Name, m.Version)
	if err := w.app.CommentIssue(ctx, pr.Number, note); err != nil {
		w.cycleErr(m.Name, err)
	}
	if err := w.app.ClosePR(ctx, pr.Number); err != nil {
		w.cycleErr(m.Name, err)
		w.say(m.Name, m.Version, proposed, "error al cerrar el PR")
		return
	}
	if own, err := w.ownBranch(ctx, branch); err == nil && own && w.remoteSHA(ctx, branch) != "" {
		lease := "--force-with-lease=refs/heads/" + branch + ":" + w.remoteSHA(ctx, branch)
		if _, err := w.git(ctx, w.Root, "push", "-q", lease, "origin", ":refs/heads/"+branch); err != nil {
			w.cycleErr(m.Name, err)
		}
	}
	w.say(m.Name, m.Version, proposed, fmt.Sprintf("pr-cerrado #%d", pr.Number))
}

// --- edición de manifiestos (sin reescribir el YAML: se conservan comentarios y estilo) ---

var (
	versionLine  = regexp.MustCompile(`(?m)^version:[ \t]*[^ \t\n#]+`)
	revisionLine = regexp.MustCompile(`(?m)^revision:[ \t]*[^ \t\n#]+`)
)

// setVersion cambia version y pone revision: 1 (constitución VI). Si la
// versión se leería como un número en YAML (1.10), la escribe entre comillas.
func setVersion(data []byte, rel, version string) ([]byte, error) {
	if len(versionLine.FindAll(data, -1)) != 1 || len(revisionLine.FindAll(data, -1)) != 1 {
		return nil, fmt.Errorf("%s: no encuentro una sola línea version: y una sola revision: en la raíz", rel)
	}
	for _, v := range []string{version, strconv.Quote(version)} {
		out := versionLine.ReplaceAllLiteral(data, []byte("version: "+v))
		out = revisionLine.ReplaceAllLiteral(out, []byte("revision: 1"))
		if m, err := manifest.Parse(out, rel); err == nil && m.Version == version && m.Revision == 1 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s: la versión %q no da un manifiesto válido", rel, version)
}

var spdxLine = regexp.MustCompile(`(?m)^([ \t]+spdx:[ \t]*)[^ \t\n#]+`)

// setLicense cambia license.spdx (solo en la forma de bloque, la de los
// manifiestos de packages/).
func setLicense(data []byte, rel, spdx string) ([]byte, error) {
	i := bytes.Index(data, []byte("\nlicense:"))
	if i < 0 {
		return nil, fmt.Errorf("%s: no encuentro el bloque license:", rel)
	}
	block := data[i+1:]
	loc := spdxLine.FindSubmatchIndex(block)
	if loc == nil {
		return nil, fmt.Errorf("%s: no encuentro license.spdx en forma de bloque", rel)
	}
	out := append([]byte{}, data[:i+1]...)
	out = append(out, block[:loc[3]]...)
	out = append(out, spdx...)
	out = append(out, block[loc[1]:]...)
	if m, err := manifest.Parse(out, rel); err != nil || m.License.SPDX != spdx {
		return nil, fmt.Errorf("%s: no se pudo cambiar license.spdx a %s", rel, spdx)
	}
	return out, nil
}
