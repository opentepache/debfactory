package repo

import (
	"strings"
	"testing"
	"time"
)

func versionsOf(st *State, pkg string) []string {
	var out []string
	for _, p := range st.Packages[pkg] {
		out = append(out, p.Version+":"+strings.Join(p.Suites, ","))
	}
	return out
}

func TestRetention(t *testing.T) {
	cfg := loadCfg(t)
	now := time.Now()
	all := []string{"bookworm", "trixie", "sid", "noble", "resolute"}
	st := Empty()
	var err error
	// SC-008: 5 versiones sucesivas → quedan 3.
	for i, v := range []string{"1.0.0-0~tepache1", "1.1.0-0~tepache1", "1.2.0-0~tepache1", "1.3.0-0~tepache1", "1.4.0-0~tepache1"} {
		var ch []Change
		st, ch, err = st.Apply(cfg, now, Add{"fnm", pub(v, string(rune('a'+i)), string(rune('0'+i)), all...)})
		if err != nil {
			t.Fatal(err)
		}
		if i >= 3 && (len(ch) != 2 || ch[1].Kind != KindRemove || ch[1].Reason != "retención (keep 3)") {
			t.Errorf("cambios en %s: %+v", v, ch)
		}
	}
	if got := strings.Join(versionsOf(st, "fnm"), " "); !strings.HasPrefix(got, "1.4.0-0~tepache1:") || len(st.Packages["fnm"]) != 3 {
		t.Errorf("tras 5 versiones: %s", got)
	}
	if err := st.Validate(cfg); err != nil {
		t.Fatal(err)
	}

	// ~tepache1 y ~tepache2 de la misma versión upstream cuentan como dos.
	st, _, err = st.Apply(cfg, now, Add{"fnm", pub("1.4.0-0~tepache2", "x", "y", all...)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Find("fnm", "1.2.0-0~tepache1"); ok || len(st.Packages["fnm"]) != 3 {
		t.Errorf("~tepache2: %v", versionsOf(st, "fnm"))
	}

	// Exclusión nueva: la versión nueva excluye bookworm; las anteriores siguen ahí.
	st, ch, err := st.Apply(cfg, now, Add{"fnm", pub("1.5.0-0~tepache1", "z", "w", all[1:]...)})
	if err != nil {
		t.Fatal(err)
	}
	var warned bool
	for _, c := range ch {
		if c.Kind == KindKeep && strings.Contains(c.Reason, "los usuarios de bookworm se quedan en 1.4.0-0~tepache2") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("sin aviso de exclusión: %+v", ch)
	}
	if p, _ := st.Find("fnm", "1.3.0-0~tepache1"); strings.Join(p.Suites, ",") != "bookworm" {
		t.Errorf("1.3.0 debería quedar solo en bookworm: %v", versionsOf(st, "fnm"))
	}
}

func TestRetire(t *testing.T) {
	cfg := loadCfg(t)
	now := time.Date(2026, 12, 3, 18, 30, 0, 0, time.UTC)
	st := Empty()
	st, _, _ = st.Apply(cfg, now, Add{"fnm", pub("1.39.0-0~tepache1", "1", "2", "trixie")})
	st, _, _ = st.Apply(cfg, now, Add{"fnm", pub("1.40.0-0~tepache1", "3", "4", "trixie")})

	next, ch, err := st.Apply(cfg, now, Retire{Package: "fnm", Version: "1.40.0-0~tepache1", Reason: "segfault"})
	if err != nil || len(ch) != 1 || next.Serial != st.Serial+1 {
		t.Fatalf("retirar: %v %+v", err, ch)
	}
	if v := next.Packages["fnm"]; len(v) != 1 || v[0].Version != "1.39.0-0~tepache1" {
		t.Errorf("vigente tras retirar: %v", versionsOf(next, "fnm"))
	}
	if _, _, err := next.Apply(cfg, now, Add{"fnm", pub("1.40.0-0~tepache1", "3", "4", "trixie")}); err == nil ||
		!strings.Contains(err.Error(), "state: versión retirada el 2026-12-03") {
		t.Errorf("republicar retirada: %v", err)
	}
	_, _, err = next.Apply(cfg, now, Retire{Package: "fnm", Version: "1.39.0-0~tepache1", Reason: "x"})
	if err == nil || !strings.Contains(err.Error(), "es la única versión publicada de fnm; usa --last para retirarla") {
		t.Errorf("única sin --last: %v", err)
	}
	last, _, err := next.Apply(cfg, now, Retire{Package: "fnm", Version: "1.39.0-0~tepache1", Reason: "x", Last: true})
	if err != nil || len(last.Packages) != 0 || len(last.Retired) != 2 {
		t.Errorf("única con --last: %v %+v", err, last)
	}
	if _, _, err := next.Apply(cfg, now, Retire{Package: "fnm", Version: "9.9.9-0~tepache1", Reason: "x"}); err == nil ||
		!strings.Contains(err.Error(), "no está publicada") {
		t.Errorf("inexistente: %v", err)
	}
	if err := last.Validate(cfg); err != nil {
		t.Errorf("validate: %v", err)
	}
}
