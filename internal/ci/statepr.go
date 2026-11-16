package ci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/forge"
	"debfactory/internal/repo"
	"debfactory/internal/report"
)

// StateFile es el estado del repositorio en git (fase 002).
const StateFile = "repo/state.yaml"

// Autor de los commits de la rama de estado.
const (
	botName  = "github-actions[bot]"
	botEmail = "41898282+github-actions[bot]@users.noreply.github.com"
)

// Pushed es <data>/ci/pushed.json (data-model.md §3).
type Pushed struct {
	StateSHA256  string    `json:"state_sha256"`
	AuditSHA256  string    `json:"audit_sha256"`
	BranchCommit string    `json:"branch_commit"`
	PRNumber     int       `json:"pr_number"`
	PushedAt     time.Time `json:"pushed_at"`
	LastMainSHA  string    `json:"last_main_sha"`
}

// StatePR gestiona la copia del pipeline en <data>/ci, la rama de estado y
// su PR (research D8).
type StatePR struct {
	Root   string // checkout con el remoto origin
	Cfg    *config.Config
	Dir    string // <data>/ci
	GH     *forge.Client
	Out    *report.Printer
	Now    func() time.Time
	Remote string // "origin" por defecto

	branchSHA string // punta remota de la rama al conciliar ("" si no existe)
}

// Ref es el estado de referencia de una ejecución que escribe.
type Ref struct {
	Source string // main | state-pr
	State  []byte
	Audit  []byte
	PR     *forge.PR
}

// SHA256 del state.yaml de referencia.
func (r *Ref) SHA256() string { return fetch.SHA256(r.State) }

// ParseState lee el estado de referencia.
func (r *Ref) ParseState() (*repo.State, error) {
	dir, err := os.MkdirTemp("", "debfactory-ref-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	os.MkdirAll(filepath.Join(dir, "repo"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, StateFile), r.State, 0o644); err != nil {
		return nil, err
	}
	return repo.ReadState(dir)
}

func (s *StatePR) remote() string {
	if s.Remote == "" {
		return "origin"
	}
	return s.Remote
}

func (s *StatePR) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *StatePR) stateErr(format string, args ...any) error {
	return &report.Error{Package: "pipeline", Version: "-", Stage: report.State, Cause: fmt.Sprintf(format, args...)}
}

// --- copia local ---

func (s *StatePR) readCopy() (state, audit []byte, p *Pushed, err error) {
	state, err = os.ReadFile(filepath.Join(s.Dir, "state.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	audit, err = os.ReadFile(filepath.Join(s.Dir, "audit.jsonl"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, err
	}
	p = &Pushed{}
	if data, err := os.ReadFile(filepath.Join(s.Dir, "pushed.json")); err == nil {
		if err := json.Unmarshal(data, p); err != nil {
			return nil, nil, nil, fmt.Errorf("%s/pushed.json: %v", s.Dir, err)
		}
	}
	return state, audit, p, nil
}

// ReadPushed lee pushed.json (vacío si no existe).
func (s *StatePR) ReadPushed() *Pushed {
	p := &Pushed{}
	if data, err := os.ReadFile(filepath.Join(s.Dir, "pushed.json")); err == nil {
		json.Unmarshal(data, p)
	}
	return p
}

func (s *StatePR) writePushed(p *Pushed) error {
	data, _ := json.MarshalIndent(p, "", "  ")
	return writeFileAtomic(filepath.Join(s.Dir, "pushed.json"), append(data, '\n'))
}

// SaveCopy guarda la copia del pipeline: el state.yaml y el audit.jsonl que
// acaban de quedar en el checkout tras publicar o retirar. Se llama justo
// después del swap (contracts/cli.md, ci main paso 5).
func (s *StatePR) SaveCopy() error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	for _, f := range []struct{ src, dst string }{{StateFile, "state.yaml"}, {AuditFile, "audit.jsonl"}} {
		data, err := os.ReadFile(filepath.Join(s.Root, f.src))
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(s.Dir, f.dst), data); err != nil {
			return err
		}
	}
	return nil
}

// SetLastMain guarda el último commit de main procesado entero (paso 3b).
func (s *StatePR) SetLastMain(sha string) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	p := s.ReadPushed()
	p.LastMainSHA = sha
	return s.writePushed(p)
}

// --- git ---

func (s *StatePR) git(ctx context.Context, args ...string) (string, error) {
	return gitOut(ctx, s.Root, args...)
}

// show devuelve el contenido de path en ref, o nil si no existe.
func (s *StatePR) show(ctx context.Context, ref, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", s.Root, "show", ref+":"+path)
	cmd.Env = gitEnv()
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := errb.String()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") {
			return nil, nil
		}
		return nil, fmt.Errorf("git show %s:%s: %s", ref, path, firstLine(strings.TrimSpace(msg)))
	}
	return out.Bytes(), nil
}

// fetch trae main y, si existe, la rama de estado.
func (s *StatePR) fetch(ctx context.Context) error {
	br := s.Cfg.CI.StateBranch
	if _, err := s.git(ctx, "fetch", "-q", s.remote(), "+refs/heads/main:refs/remotes/"+s.remote()+"/main"); err != nil {
		return err
	}
	out, err := s.git(ctx, "ls-remote", s.remote(), "refs/heads/"+br)
	if err != nil {
		return err
	}
	s.branchSHA = ""
	if f := strings.Fields(out); len(f) > 0 {
		s.branchSHA = f[0]
		if _, err := s.git(ctx, "fetch", "-q", s.remote(), "+refs/heads/"+br+":refs/remotes/"+s.remote()+"/"+br); err != nil {
			return err
		}
	}
	return nil
}

// MainHead devuelve la punta remota de main.
func (s *StatePR) MainHead(ctx context.Context) (string, error) {
	out, err := s.git(ctx, "ls-remote", s.remote(), "refs/heads/main")
	if err != nil {
		return "", err
	}
	if f := strings.Fields(out); len(f) > 0 {
		return f[0], nil
	}
	return "", fmt.Errorf("el remoto no tiene main")
}

// --- conciliación (tabla de data-model.md §3) ---

// Reconcile compara la copia del pipeline, la rama de estado y main, y
// devuelve el estado de referencia. Completa un push pendiente (FR-029) y
// falla con la etapa state ante cualquier otra discrepancia (FR-007b).
func (s *StatePR) Reconcile(ctx context.Context) (*Ref, error) {
	if err := s.fetch(ctx); err != nil {
		return nil, s.stateErr("%v", err)
	}
	r, br := s.remote(), s.Cfg.CI.StateBranch
	mainState, err := s.show(ctx, r+"/main", StateFile)
	if err != nil {
		return nil, s.stateErr("%v", err)
	}
	mainAudit, err := s.show(ctx, r+"/main", AuditFile)
	if err != nil {
		return nil, s.stateErr("%v", err)
	}
	copyState, copyAudit, pushed, err := s.readCopy()
	if err != nil {
		return nil, s.stateErr("%v", err)
	}
	mainRef := &Ref{Source: "main", State: mainState, Audit: mainAudit}
	if copyState == nil {
		if s.branchSHA != "" {
			return nil, s.stateErr("existe la rama %s pero no hay copia del pipeline en %s; revisa la rama y bórrala si sobra", br, s.Dir)
		}
		return mainRef, nil
	}
	// main ya contiene la copia: el PR se integró.
	if bytes.Equal(mainState, copyState) && bytes.Equal(mainAudit, copyAudit) {
		if s.branchSHA != "" {
			if _, err := s.git(ctx, "push", "-q", r, "--delete", "refs/heads/"+br); err != nil {
				s.Out.Line("estado: no se pudo borrar la rama %s: %v", br, err)
			}
		}
		return mainRef, nil
	}
	copyRef := &Ref{Source: "state-pr", State: copyState, Audit: copyAudit}
	if !IsPrefix(mainAudit, copyAudit) {
		return nil, s.stateErr("main tiene un %s que no es prefijo de la copia del pipeline; el estado de main cambió fuera del pipeline", AuditFile)
	}
	var branchState, branchAudit []byte
	if s.branchSHA != "" {
		if branchState, err = s.show(ctx, r+"/"+br, StateFile); err != nil {
			return nil, s.stateErr("%v", err)
		}
		if branchAudit, err = s.show(ctx, r+"/"+br, AuditFile); err != nil {
			return nil, s.stateErr("%v", err)
		}
	}
	switch {
	case s.branchSHA != "" && bytes.Equal(branchState, copyState) && bytes.Equal(branchAudit, copyAudit):
		pr, err := s.GH.FindOpenPR(ctx, br, "main")
		if err != nil {
			return nil, s.stateErr("%v", err)
		}
		if pr == nil && pushed.PRNumber != 0 {
			old, err := s.GH.GetPR(ctx, pushed.PRNumber)
			if err != nil {
				return nil, s.stateErr("%v", err)
			}
			return nil, s.stateErr("el PR de estado #%d está %s sin integrar y main no tiene el estado publicado; ejecuta restore-state-pr", old.Number, old.State)
		}
		if pr == nil {
			// El push llegó pero el PR no se abrió (ejecución interrumpida).
			if pr, err = s.openPR(ctx, copyState, copyAudit, mainAudit, pushed); err != nil {
				return nil, s.stateErr("%v", err)
			}
		}
		copyRef.PR = pr
		return copyRef, nil
	case s.branchSHA == "" || (IsPrefix(branchAudit, copyAudit) && !bytes.Equal(branchAudit, copyAudit)):
		// El último push no llegó: se completa (FR-029).
		s.Out.Line("estado: la rama %s va por detrás de lo publicado; se vuelve a empujar", br)
		pr, err := s.Push(ctx)
		if err != nil {
			return nil, s.stateErr("%v", err)
		}
		copyRef.PR = pr
		return copyRef, nil
	}
	return nil, s.stateErr("la rama %s no coincide con lo publicado (editada a mano o en conflicto); ejecuta restore-state-pr", br)
}

// Push regenera la rama de estado como main + 1 commit con la copia del
// pipeline, la empuja con --force-with-lease y abre o actualiza el único PR
// de estado. Si main ya tiene la copia, borra la rama.
func (s *StatePR) Push(ctx context.Context) (*forge.PR, error) {
	r, br := s.remote(), s.Cfg.CI.StateBranch
	copyState, copyAudit, pushed, err := s.readCopy()
	if err != nil {
		return nil, err
	}
	if copyState == nil {
		return nil, fmt.Errorf("no hay copia del pipeline en %s", s.Dir)
	}
	if _, err := s.git(ctx, "fetch", "-q", r, "+refs/heads/main:refs/remotes/"+r+"/main"); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "debfactory-state-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	wt := filepath.Join(tmp, "wt")
	if _, err := s.git(ctx, "worktree", "add", "-q", "--detach", wt, r+"/main"); err != nil {
		return nil, err
	}
	defer s.git(context.Background(), "worktree", "remove", "--force", wt)
	os.MkdirAll(filepath.Join(wt, "repo"), 0o755)
	if err := os.WriteFile(filepath.Join(wt, StateFile), copyState, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(wt, AuditFile), copyAudit, 0o644); err != nil {
		return nil, err
	}
	wgit := func(args ...string) (string, error) { return gitOut(ctx, wt, args...) }
	if _, err := wgit("add", StateFile, AuditFile); err != nil {
		return nil, err
	}
	if out, _ := wgit("status", "--porcelain", "--", StateFile, AuditFile); out == "" {
		// main ya tiene la copia.
		if s.branchSHA != "" {
			s.git(ctx, "push", "-q", r, "--delete", "refs/heads/"+br)
		}
		return nil, nil
	}
	st, _ := (&Ref{State: copyState}).ParseState()
	title := "repo: estado tras serial ?"
	if st != nil {
		title = fmt.Sprintf("repo: estado tras serial %d", st.Serial)
	}
	mainAudit, _ := s.show(ctx, r+"/main", AuditFile)
	pending := pendingEntries(mainAudit, copyAudit)
	if pk := packagesOf(pending); pk != "" {
		title += " (" + pk + ")"
	}
	cmd := exec.CommandContext(ctx, "git", "-C", wt, "-c", "user.name="+botName, "-c", "user.email="+botEmail,
		"commit", "-q", "-m", title, "-m", "Generado por debfactory ci. Integrar sin editar.")
	cmd.Env = append(gitEnv(), "GIT_AUTHOR_NAME="+botName, "GIT_AUTHOR_EMAIL="+botEmail,
		"GIT_COMMITTER_NAME="+botName, "GIT_COMMITTER_EMAIL="+botEmail)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git commit: %s", firstLine(string(out)))
	}
	head, err := wgit("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	lease := "--force-with-lease=refs/heads/" + br + ":" + s.branchSHA
	if _, err := wgit("push", "-q", lease, r, "HEAD:refs/heads/"+br); err != nil {
		return nil, err
	}
	s.branchSHA = head
	pushed.StateSHA256, pushed.AuditSHA256 = fetch.SHA256(copyState), fetch.SHA256(copyAudit)
	pushed.BranchCommit, pushed.PushedAt = head, s.now().UTC()
	if err := s.writePushed(pushed); err != nil {
		return nil, err
	}
	return s.openPR(ctx, copyState, copyAudit, mainAudit, pushed)
}

// openPR abre el PR de estado o actualiza el abierto (nunca hay dos).
func (s *StatePR) openPR(ctx context.Context, state, audit, mainAudit []byte, pushed *Pushed) (*forge.PR, error) {
	br := s.Cfg.CI.StateBranch
	st, _ := (&Ref{State: state}).ParseState()
	pending := pendingEntries(mainAudit, audit)
	title := "repo: estado tras serial ?"
	if st != nil {
		title = fmt.Sprintf("repo: estado tras serial %d", st.Serial)
	}
	if pk := packagesOf(pending); pk != "" {
		title += " (" + pk + ")"
	}
	body := prBody(pending)
	pr, err := s.GH.FindOpenPR(ctx, br, "main")
	if err != nil {
		return nil, err
	}
	if pr != nil {
		if err := s.GH.UpdatePR(ctx, pr.Number, title, body); err != nil {
			return nil, err
		}
	} else if pr, err = s.GH.CreatePR(ctx, br, "main", title, body); err != nil {
		return nil, err
	}
	pushed.PRNumber = pr.Number
	if err := s.writePushed(pushed); err != nil {
		return nil, err
	}
	return pr, nil
}

// pendingEntries son las entradas de audit que aún no están en main.
func pendingEntries(mainAudit, audit []byte) []Entry {
	all, err := ParseAudit(audit)
	if err != nil {
		return nil
	}
	done, _ := ParseAudit(mainAudit)
	return all[len(done):]
}

func packagesOf(entries []Entry) string {
	var seen []string
	for _, e := range entries {
		s := e.Package + " " + e.Version
		if e.Op == OpRetire {
			s += " (retirada)"
		}
		if e.Op != OpImport {
			seen = append(seen, s)
		}
	}
	if len(seen) > 4 {
		seen = append(seen[:4], "…")
	}
	return strings.Join(seen, ", ")
}

func prBody(pending []Entry) string {
	var b strings.Builder
	b.WriteString("Estado del repositorio APT tras operaciones ya publicadas por el pipeline.\n\n")
	b.WriteString("**Integrar sin editar.** La disponibilidad vía apt no depende de este merge; ")
	b.WriteString("mientras esté abierto, las ejecuciones que escriben parten de este estado (FR-007a).\n\n")
	b.WriteString("| seq | op | paquete | versión | serial | ejecución |\n|---|---|---|---|---|---|\n")
	for _, e := range pending {
		run := "—"
		if e.Run != nil {
			run = *e.Run
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %d | %s |\n", e.Seq, e.Op, e.Package, e.Version, e.Serial, run)
	}
	return b.String()
}

// RestoreStatePR vuelve a crear la rama y el PR desde la copia del
// pipeline, tras comprobar que la copia es el estado de la generación current
// (D8).
func (s *StatePR) RestoreStatePR(ctx context.Context, dataDir string) (*forge.PR, error) {
	copyState, _, _, err := s.readCopy()
	if err != nil {
		return nil, err
	}
	if copyState == nil {
		return nil, s.stateErr("no hay copia del pipeline en %s", s.Dir)
	}
	cur, err := repo.Current(dataDir)
	if err != nil || cur == nil {
		return nil, s.stateErr("no hay generación current en %s", dataDir)
	}
	if cur.StateSHA256 != "" && cur.StateSHA256 != fetch.SHA256(copyState) {
		return nil, s.stateErr("la copia del pipeline no es el estado de la generación current (%s)", cur.Name)
	}
	if err := s.fetch(ctx); err != nil {
		return nil, s.stateErr("%v", err)
	}
	return s.Push(ctx)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
