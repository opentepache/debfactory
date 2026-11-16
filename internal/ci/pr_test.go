//go:build integration

package ci

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"debfactory/internal/build"
	"debfactory/internal/fetch"
)

// prRunner prepara un PR: una rama pr en el checkout con los cambios de edit.
func (f *mainFixture) prRunner(t *testing.T, edit func(dir string)) (*Runner, string) {
	t.Helper()
	f.g.Git("checkout", "-q", "-b", "pr", "origin/main")
	edit(f.g.Work)
	f.g.Commit("cambio del PR")
	r := f.runner(f.g.Git("rev-parse", "HEAD"))
	r.Env.Trust, r.Env.EventName, r.Env.Ref, r.Env.Token = "ci", "pull_request", "refs/pull/1/merge", ""
	summary := filepath.Join(t.TempDir(), "summary.md")
	r.Env.StepSummary = summary
	// FR-009: el check no tiene <data>; si lo abriera, este directorio cambiaría.
	r.Repo.DataDir = t.TempDir()
	return r, summary
}

func dirEmpty(t *testing.T, dir string) bool {
	entries, _ := os.ReadDir(dir)
	return len(entries) == 0
}

// TestCIPR: un PR que sube un paquete construye y prueba solo ese paquete,
// escribe la tabla por suite y arquitectura y no toca <data> ni la forja.
func TestCIPR(t *testing.T) {
	f := newMainFixture(t)
	r, summary := f.prRunner(t, func(dir string) {
		editFile(t, filepath.Join(dir, "packages/fnm.yaml"), "revision: 1", "revision: 2")
	})
	if err := r.PR(context.Background(), Options{Base: "origin/main", Jobs: 2}); err != nil {
		t.Fatalf("ci pr: %v\n%s", err, f.out.String())
	}
	if strings.Join(f.built, " ") != "fnm" {
		t.Errorf("construidos: %v", f.built)
	}
	md, _ := os.ReadFile(summary)
	for _, want := range []string{"| fnm | 1.39.0-0~tepache2 | bookworm | ✅ | ✅ |", "| fnm | 1.39.0-0~tepache2 | resolute | ✅ | ✅ |", "Un PR nunca firma ni publica"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("resumen sin %q:\n%s", want, md)
		}
	}
	if !dirEmpty(t, r.Repo.DataDir) || len(f.gh.Requests) != 0 {
		t.Errorf("el PR escribió en <data> o habló con la forja: %v", f.gh.Requests)
	}
	// En el runner confiable, ci pr se niega (código 3).
	r.Env.Trust = "pub"
	if err := r.PR(context.Background(), Options{Base: "origin/main"}); err == nil || !strings.Contains(err.Error(), "entorno") {
		t.Errorf("ci pr en pub: %v", err)
	}
}

// TestCIPRBadChecksum: un sha256 cambiado en el lockfile falla en verify,
// antes de construir, nombrando el artefacto y los dos hashes (US2
// escenario 1, SC-002). Usa el build real (descarga el artefacto de fnm).
func TestCIPRBadChecksum(t *testing.T) {
	f := newMainFixture(t)
	var bad, good string
	r, _ := f.prRunner(t, func(dir string) {
		p := filepath.Join(dir, "packages/fnm.lock.yaml")
		data, _ := os.ReadFile(p)
		m := regexp.MustCompile(`amd64: ([0-9a-f]{64})`).FindSubmatch(data)
		if m == nil {
			t.Fatalf("no hay sha de amd64 en %s", p)
		}
		good = string(m[1])
		bad = strings.Repeat("0", 64)
		os.WriteFile(p, bytes.Replace(data, m[1], []byte(bad), 1), 0o644)
	})
	r.Gates = nil
	cache := t.TempDir()
	r.Build = func() *build.Env {
		e := &build.Env{Cfg: f.cfg, Workdir: t.TempDir(), Fetcher: fetch.New(cache, fetch.DefaultTimeout, nil),
			GitHub: &fetch.GitHub{}, Out: r.Out}
		e.Defaults()
		return e
	}
	err := r.PR(context.Background(), Options{Base: "origin/main", Jobs: 2})
	if err == nil {
		t.Fatal("se esperaba un fallo")
	}
	msg := err.Error()
	if !strings.Contains(msg, ": verify: ") || !strings.Contains(msg, bad) || !strings.Contains(msg, good) {
		t.Errorf("el fallo no es de verify con los dos hashes:\n%s", msg)
	}
	if _, err := os.Stat(filepath.Join(f.g.Work, "dist")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(f.g.Work, "dist"))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "fnm_") {
				t.Errorf("se construyó algo: %s", e.Name())
			}
		}
	}
	if !dirEmpty(t, r.Repo.DataDir) {
		t.Error("el PR escribió en <data>")
	}
}
