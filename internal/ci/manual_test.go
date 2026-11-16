//go:build integration

package ci

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/repo"
	"debfactory/internal/version"
)

func (f *mainFixture) dispatch(action string, o Options, now time.Time) error {
	f.t.Helper()
	f.out.Reset()
	f.built = nil
	sha := f.checkout()
	r := f.runner(sha)
	r.Env.EventName = "workflow_dispatch"
	r.Env.vars["INPUT_ACTION"] = action
	r.Env.vars["INPUT_PACKAGE"], r.Env.vars["INPUT_VERSION"], r.Env.vars["INPUT_REASON"] = o.Package, o.Version, o.Reason
	if o.Last {
		r.Env.vars["INPUT_LAST"] = "true"
	}
	if !now.IsZero() {
		r.Repo.Now = func() time.Time { return now }
	}
	return r.Dispatch(context.Background(), "main", Options{Jobs: 2})
}

func (f *mainFixture) validUntil() time.Time {
	f.t.Helper()
	cur, err := repo.Current(f.data)
	if err != nil || cur == nil {
		f.t.Fatalf("current: %v", err)
	}
	vu, err := repo.TreeValidUntil(f.cfg, cur.Dir(f.data))
	if err != nil {
		f.t.Fatal(err)
	}
	return vu
}

// TestCIRefresh: refresh firma de nuevo con un Valid-Until posterior sin
// tocar state.yaml ni la auditoría (FR-031); el aviso de caducidad abre y
// cierra su issue (FR-032); el PR de estado antiguo se notifica (FR-007c).
func TestCIRefresh(t *testing.T) {
	f := newMainFixture(t)
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	vu := f.validUntil()
	state, _ := os.ReadFile(filepath.Join(f.data, "ci", "state.yaml"))
	audit, _ := os.ReadFile(filepath.Join(f.data, "ci", "audit.jsonl"))

	// 24 días después: quedan 6 → issue expiry y otro por el PR de estado (8 > 7 días no; 24 sí).
	later := time.Now().AddDate(0, 0, 24)
	if err := f.dispatchMain(later); err != nil {
		t.Fatalf("main a los 24 días: %v\n%s", err, f.out.String())
	}
	titles := issueTitles(f)
	if !strings.Contains(titles, "fallo: pipeline - en expiry") || !strings.Contains(titles, "fallo: pipeline - en state-pr-age") {
		t.Fatalf("issues = %s\n%s", titles, f.out.String())
	}

	if err := f.dispatch("refresh", Options{}, later); err != nil {
		t.Fatalf("refresh: %v\n%s", err, f.out.String())
	}
	if nv := f.validUntil(); !nv.After(vu) {
		t.Errorf("Valid-Until %v no es posterior a %v", nv, vu)
	}
	state2, _ := os.ReadFile(filepath.Join(f.data, "ci", "state.yaml"))
	audit2, _ := os.ReadFile(filepath.Join(f.data, "ci", "audit.jsonl"))
	if !bytes.Equal(state, state2) || !bytes.Equal(audit, audit2) {
		t.Error("refresh cambió state.yaml o la auditoría")
	}
	if titles := issueTitles(f); strings.Contains(titles, "expiry") {
		t.Errorf("el issue de caducidad no se cerró: %s", titles)
	}
}

func (f *mainFixture) dispatchMain(now time.Time) error {
	f.t.Helper()
	f.out.Reset()
	sha := f.checkout()
	r := f.runner(sha)
	r.Repo.Now = func() time.Time { return now }
	return r.Main(context.Background(), Options{Jobs: 2})
}

func issueTitles(f *mainFixture) string {
	var s []string
	for _, is := range f.gh.OpenIssues() {
		s = append(s, is.Title)
	}
	return strings.Join(s, "; ")
}

// TestCIManual: rebuild de una versión publicada (iguales y distintos),
// rebuild sin commit, retire con y sin last, y restore-state-pr (US6).
func TestCIManual(t *testing.T) {
	f := newMainFixture(t)
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	f.mergeStatePR()

	if err := f.dispatch("rebuild", Options{Package: "fnm", Version: "1.39.0-0~tepache1"}, time.Time{}); err != nil {
		t.Fatalf("rebuild: %v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "sha256 iguales; nada que publicar") {
		t.Errorf("rebuild:\n%s", f.out.String())
	}
	// Un build que ya no reproduce lo publicado falla sin publicar.
	f.alter = map[string]bool{"fnm": true}
	err := f.dispatch("rebuild", Options{Package: "fnm", Version: "1.39.0-0~tepache1"}, time.Time{})
	f.alter = nil
	if err == nil || !strings.Contains(err.Error(), "no es el publicado") {
		t.Errorf("rebuild con hash distinto: %v", err)
	}
	err = f.dispatch("rebuild", Options{Package: "fnm", Version: "9.9.9-0~tepache1"}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "ningún commit") {
		t.Errorf("rebuild sin commit: %v", err)
	}
	// Retirar la única versión sin last falla sin cambios.
	err = f.dispatch("retire", Options{Package: "fnm", Version: "1.39.0-0~tepache1", Reason: "prueba"}, time.Time{})
	if err == nil || f.state().Serial != 1 {
		t.Errorf("retire sin last: %v (serial %d)", err, f.state().Serial)
	}
	// Con una versión nueva publicada, retirar la anterior.
	f.bump("fnm")
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	if err := f.dispatch("retire", Options{Package: "fnm", Version: "1.39.0-0~tepache1", Reason: "segfault en arm64"}, time.Time{}); err != nil {
		t.Fatalf("retire: %v\n%s", err, f.out.String())
	}
	audit := f.g.RemoteFile("debfactory/state", AuditFile)
	if !strings.Contains(audit, `"op":"retire","package":"fnm","version":"1.39.0-0~tepache1"`) || !strings.Contains(audit, `"reason":"segfault en arm64"`) {
		t.Errorf("auditoría sin el retiro:\n%s", audit)
	}
	// PR de estado cerrado sin integrar → etapa state; restore-state-pr lo arregla.
	f.gh.ClosePR(f.gh.OpenPRs()[0].Number, false)
	if err := f.main(); err == nil || !strings.Contains(err.Error(), ": state: ") {
		t.Fatalf("PR cerrado: %v", err)
	}
	if err := f.dispatch("restore-state-pr", Options{}, time.Time{}); err != nil {
		t.Fatalf("restore-state-pr: %v\n%s", err, f.out.String())
	}
	if err := f.main(); err != nil {
		t.Fatalf("tras restore-state-pr: %v\n%s", err, f.out.String())
	}
	if titles := issueTitles(f); strings.Contains(titles, "fallo: pipeline - en state") || strings.Contains(titles, "fnm 1.39.0-0~tepache1") {
		t.Errorf("el issue de state no se cerró: %s", titles)
	}
}

// TestCIRebuildAfterToolBump: rebuild de una versión declarada antes de subir
// la herramienta usa la de HEAD; el debfactory.version antiguo del worktree
// del commit que la declaró no cuenta (004 FR-005, caso límite de rebuild).
func TestCIRebuildAfterToolBump(t *testing.T) {
	f := newMainFixture(t)
	if err := f.main(); err != nil {
		t.Fatal(err)
	}
	f.mergeStatePR()
	next := strings.Repeat("7", 40)
	c := f.g.Clone()
	c.Write(ToolFile, next+"\n")
	c.Commit("subir la herramienta")
	c.Push()
	version.Commit = next
	if err := f.dispatch("rebuild", Options{Package: "fnm", Version: "1.39.0-0~tepache1"}, time.Time{}); err != nil {
		t.Fatalf("rebuild tras subir la herramienta: %v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "sha256 iguales; nada que publicar") {
		t.Errorf("rebuild:\n%s", f.out.String())
	}
}
