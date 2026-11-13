package repo

import (
	"strings"
	"testing"
	"time"
)

func pub(version, a, b string, suites ...string) Published {
	return Published{Version: version, Suites: suites, Debs: map[string]string{
		"amd64": strings.Repeat(a, 64), "arm64": strings.Repeat(b, 64)}}
}

func TestApplyAdd(t *testing.T) {
	cfg := loadCfg(t)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	st := Empty()
	next, ch, err := st.Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", "trixie")})
	if err != nil || len(ch) != 1 || next.Serial != 1 || st.Serial != 0 {
		t.Fatalf("add: %v %+v serial=%d", err, ch, next.Serial)
	}
	// mismo sha: sin cambios
	same, ch, err := next.Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", "trixie")})
	if err != nil || len(ch) != 0 || same.Serial != 1 {
		t.Fatalf("republicar: %v %+v", err, ch)
	}
	// otro sha con la misma versión
	_, _, err = next.Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "3", "2", "trixie")})
	if err == nil || !strings.Contains(err.Error(), "fnm 1.0.0-0~tepache1 amd64: state: ya publicada con otro sha256 (las versiones no se reescriben; sube revision)") {
		t.Fatalf("reescribir: %v", err)
	}
	// retirada
	next.Retired = append(next.Retired, Retired{Package: "fnm", Version: "1.1.0-0~tepache1", Date: now, Reason: "x"})
	_, _, err = next.Apply(cfg, now, Add{"fnm", pub("1.1.0-0~tepache1", "4", "5", "trixie")})
	if err == nil || !strings.Contains(err.Error(), "state: versión retirada el 2026-11-20 (las versiones no se reutilizan; sube revision)") {
		t.Fatalf("retirada: %v", err)
	}
	// todo o nada: una válida y una inválida no aplican ninguna
	after, _, err := next.Apply(cfg, now, Add{"bob", pub("4.3.0-0~tepache1", "6", "7", "trixie")},
		Add{"fnm", pub("1.1.0-0~tepache1", "4", "5", "trixie")})
	if err == nil || after != next {
		t.Fatalf("todo o nada: %v", err)
	}
}
