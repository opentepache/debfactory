package ci

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"debfactory/internal/forge"
	"debfactory/internal/redact"
	"debfactory/internal/repo"
	"debfactory/internal/report"
)

// Key identifica un issue de fallo (contracts/notificaciones.md).
type Key struct{ Package, Version, Stage string }

// KeyOf normaliza un error a su clave.
func KeyOf(e *report.Error) Key {
	k := Key{Package: e.Package, Version: e.Version, Stage: string(e.Stage)}
	if k.Package == "" {
		k.Package = "pipeline"
	}
	if k.Package == "pipeline" || k.Version == "" {
		k.Version = "-"
	}
	return k
}

// Marker es la primera línea del cuerpo.
func (k Key) Marker() string {
	return fmt.Sprintf("<!-- debfactory:failure package=%s version=%s stage=%s -->", k.Package, k.Version, k.Stage)
}

// Title es el título del issue.
func (k Key) Title() string { return fmt.Sprintf("fallo: %s %s en %s", k.Package, k.Version, k.Stage) }

var markerRe = regexp.MustCompile(`^<!-- debfactory:failure package=(\S+) version=(\S+) stage=(\S+) -->`)

func keyOfBody(body string) (Key, bool) {
	m := markerRe.FindStringSubmatch(body)
	if m == nil {
		return Key{}, false
	}
	return Key{m[1], m[2], m[3]}, true
}

// GitHubNotifier abre, comenta y cierra issues con el GITHUB_TOKEN del job.
// Si la API falla, lo dice en el log y no cambia el resultado.
type GitHubNotifier struct {
	GH     *forge.Client
	Label  string
	RunURL string
	Commit string
	Out    *report.Printer
	Token  string // solo para filtrarlo de los textos
	open   []forge.Issue
	loaded bool
}

func (n *GitHubNotifier) load(ctx context.Context) bool {
	if n.loaded {
		return true
	}
	issues, err := n.GH.ListOpenIssues(ctx, n.Label)
	if err != nil {
		n.Out.Line("notify: no se pudieron leer los issues: %v", err)
		return false
	}
	n.open, n.loaded = issues, true
	return true
}

// Fail comenta el issue abierto de la clave o abre uno nuevo (FR-020, FR-021).
func (n *GitHubNotifier) Fail(ctx context.Context, e *report.Error) {
	k := KeyOf(e)
	if !n.load(ctx) {
		return
	}
	text := n.body(k, e)
	for _, is := range n.open {
		if got, ok := keyOfBody(is.Body); ok && got == k {
			if err := n.GH.CommentIssue(ctx, is.Number, strings.SplitN(text, "\n", 2)[1]); err != nil {
				n.Out.Line("notify: no se pudo comentar #%d: %v", is.Number, err)
				return
			}
			n.Out.Event("issue", map[string]any{"number": is.Number, "action": "comment"}, fmt.Sprintf("issues: comentado #%d (%s)", is.Number, k.Title()))
			return
		}
	}
	is, err := n.GH.CreateIssue(ctx, k.Title(), text, []string{n.Label})
	if err != nil {
		n.Out.Line("notify: no se pudo abrir el issue %q: %v", k.Title(), err)
		return
	}
	is.Body = text
	n.open = append(n.open, *is)
	n.Out.Event("issue", map[string]any{"number": is.Number, "action": "open"}, fmt.Sprintf("issues: abierto #%d (%s)", is.Number, k.Title()))
}

func (n *GitHubNotifier) body(k Key, e *report.Error) string {
	var b strings.Builder
	b.WriteString(k.Marker() + "\n")
	fmt.Fprintf(&b, "**Paquete:** %s · **Versión:** %s · **Etapa:** %s\n\n", k.Package, k.Version, k.Stage)
	where := ""
	if e.Suite != "" || e.Arch != "" {
		where = " en " + strings.Trim(e.Suite+"/"+e.Arch, "/")
	}
	cause := e.Cause
	if len(e.Detail) > 0 {
		cause += "\n" + strings.Join(e.Detail, "\n")
	}
	cause = redact.String(cause, n.Token)
	fmt.Fprintf(&b, "**Motivo%s:** %s\n\n", where, cause)
	commit := n.Commit
	if len(commit) > 7 {
		commit = commit[:7]
	}
	fmt.Fprintf(&b, "**Log:** %s · **Commit:** %s\n", n.RunURL, commit)
	return b.String()
}

// close cierra los issues abiertos que cumplen match, con un comentario.
func (n *GitHubNotifier) close(ctx context.Context, match func(Key) bool, comment string) {
	if !n.load(ctx) {
		return
	}
	var keep []forge.Issue
	for _, is := range n.open {
		k, ok := keyOfBody(is.Body)
		if !ok || !match(k) {
			keep = append(keep, is)
			continue
		}
		if err := n.GH.CommentIssue(ctx, is.Number, comment); err != nil {
			n.Out.Line("notify: no se pudo comentar #%d: %v", is.Number, err)
		}
		if err := n.GH.CloseIssue(ctx, is.Number); err != nil {
			n.Out.Line("notify: no se pudo cerrar #%d: %v", is.Number, err)
			keep = append(keep, is)
			continue
		}
		n.Out.Event("issue", map[string]any{"number": is.Number, "action": "close"}, fmt.Sprintf("issues: cerrado #%d (%s)", is.Number, k.Title()))
	}
	n.open = keep
}

// Published cierra todos los issues de (package, version) (FR-021).
func (n *GitHubNotifier) Published(ctx context.Context, pkg, version string, serial int) {
	n.Resolve(ctx, pkg, version, fmt.Sprintf("publicado en serial %d", serial))
}

// Resolve cierra todos los issues de (package, version) con una nota.
func (n *GitHubNotifier) Resolve(ctx context.Context, pkg, version, note string) {
	n.close(ctx, func(k Key) bool { return k.Package == pkg && k.Version == version },
		fmt.Sprintf("resuelto por %s: %s", n.RunURL, note))
}

// StagePassed cierra los issues de una etapa global cuando una ejecución la
// pasa (state, audit, expiry, state-pr-age, pipeline, code, compare).
func (n *GitHubNotifier) StagePassed(ctx context.Context, stage report.Stage) {
	n.close(ctx, func(k Key) bool { return k.Package == "pipeline" && k.Stage == string(stage) },
		fmt.Sprintf("resuelto por %s: la etapa %s pasó", n.RunURL, stage))
}

// notifier es el de GitHub para las ejecuciones pub.
func (x *run) githubNotifier() Notifier {
	return &GitHubNotifier{GH: x.gh, Label: x.Cfg.CI.FailureLabel, RunURL: x.Env.RunURL(), Commit: x.Env.SHA,
		Out: x.Out, Token: x.Env.Token}
}

// checkStatePRAge notifica si el PR de estado lleva abierto más de
// ci.state_pr_max_age (FR-007c).
func (x *run) checkStatePRAge(ctx context.Context, ref *Ref) {
	if ref == nil || ref.PR == nil {
		x.notify.StagePassed(ctx, report.StatePRAge)
		return
	}
	age := x.now().Sub(ref.PR.CreatedAt)
	if age <= time.Duration(x.Cfg.CI.StatePRMaxAge) {
		return
	}
	// Es un aviso, no un fallo de la ejecución.
	e := &report.Error{Package: "pipeline", Version: "-", Stage: report.StatePRAge,
		Cause: fmt.Sprintf("el PR de estado #%d lleva %d días sin integrarse: %s", ref.PR.Number, int(age.Hours()/24), ref.PR.HTMLURL)}
	x.Out.Line("aviso: %s", e.Cause)
	x.notify.Fail(ctx, e)
}

func (x *run) now() time.Time { return x.Repo.NowTime() }

// checkExpiry notifica si a la generación servida le quedan menos de
// repo.validity.warn_days días (FR-032).
func (x *run) checkExpiry(ctx context.Context) {
	cur, err := repo.Current(x.dataDir())
	if err != nil || cur == nil {
		return
	}
	vu, err := repo.TreeValidUntil(x.Cfg, cur.Dir(x.dataDir()))
	if err != nil || vu.IsZero() {
		return
	}
	days := int(vu.Sub(x.now()).Hours() / 24)
	if days >= x.Cfg.Repo.Validity.WarnDays {
		x.notify.StagePassed(ctx, report.Expiry)
		return
	}
	x.notify.Fail(ctx, &report.Error{Package: "pipeline", Version: "-", Stage: report.Expiry,
		Cause: fmt.Sprintf("la generación servida caduca el %s (Valid-Until; quedan %d días); ejecuta refresh",
			vu.UTC().Format(time.DateOnly), days)})
}

// NotifyCmd es `ci notify`: lo usa el paso if: failure() cuando la
// ejecución murió antes de su propio informe.
func (r *Runner) NotifyCmd(ctx context.Context, o Options) error {
	if err := r.Env.RequireWriter(r.Cfg); err != nil {
		return err
	}
	stage := o.Stage
	if o.StageFile != "" {
		if data, err := os.ReadFile(o.StageFile); err == nil {
			stage = strings.TrimSpace(string(data))
		}
		if stage == "" {
			stage = "pipeline"
		}
	}
	if stage == "" || o.Package == "" || o.Reason == "" {
		return &InputError{"ci notify necesita --package, --version, --stage (o --stage-file) y --reason"}
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(stage) {
		return &InputError{fmt.Sprintf("etapa %q inválida", stage)}
	}
	x, err := r.newRun(ctx)
	if err != nil {
		return err
	}
	n := x.githubNotifier()
	n.Fail(ctx, &report.Error{Package: o.Package, Version: o.Version, Stage: report.Stage(stage), Cause: o.Reason})
	return nil
}
