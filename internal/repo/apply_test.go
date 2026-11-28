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

// TestApplyExtend: publicar otra vez la versión actual con los mismos .deb y
// suites nuevas las agrega (specs/007-configurable-suites, research D2).
func TestApplyExtend(t *testing.T) {
	cfg := loadCfg(t)
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	st, _, err := Empty().Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", "trixie", "sid")})
	if err != nil {
		t.Fatal(err)
	}
	// Mismos .deb, forky nueva: se agrega en el orden de la configuración.
	next, ch, err := st.Apply(cfg, now.Add(time.Hour), Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", "trixie", "forky", "sid")})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := next.Find("fnm", "1.0.0-0~tepache1")
	if next.Serial != st.Serial+1 || strings.Join(p.Suites, ",") != "trixie,forky,sid" || !p.Published.Equal(now.Truncate(time.Second)) {
		t.Errorf("ampliada = %+v (serial %d)", p, next.Serial)
	}
	if len(ch) != 1 || ch[0].Kind != KindAdd || strings.Join(ch[0].Suites, ",") != "forky" {
		t.Errorf("cambios = %+v", ch)
	}
	// Mismas suites, o menos: sin cambios; nunca se quitan suites.
	for _, suites := range [][]string{{"trixie", "forky", "sid"}, {"trixie"}} {
		same, ch, err := next.Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", suites...)})
		if err != nil || len(ch) != 0 || same.Serial != next.Serial {
			t.Errorf("%v: %v %+v", suites, err, ch)
		}
	}
	// Otro sha256: como hoy.
	if _, _, err := next.Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "9", "2", "trixie", "forky", "sid", "noble")}); err == nil ||
		!strings.Contains(err.Error(), "ya publicada con otro sha256") {
		t.Errorf("otro sha: %v", err)
	}
	// Una versión que ya no es la más alta no se amplía (FR-007).
	newer, _, err := next.Apply(cfg, now, Add{"fnm", pub("1.1.0-0~tepache1", "3", "4", "trixie", "forky", "sid")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := newer.Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", "trixie", "forky", "sid", "noble")}); err == nil ||
		!strings.Contains(err.Error(), "fnm 1.0.0-0~tepache1 -: state: no es la versión más alta; no se amplía") {
		t.Errorf("versión anterior: %v", err)
	}
}

// TestApplyExtendRetention: después de ampliar, la retención se aplica en la
// suite nueva como en las demás (FR-007).
func TestApplyExtendRetention(t *testing.T) {
	cfg := loadCfg(t)
	cfg.Repo.Keep = 1
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	st, _, err := Empty().Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", "trixie")})
	if err != nil {
		t.Fatal(err)
	}
	st, _, err = st.Apply(cfg, now, Add{"fnm", pub("1.1.0-0~tepache1", "3", "4", "trixie")})
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := st.Apply(cfg, now, Add{"fnm", pub("1.1.0-0~tepache1", "3", "4", "trixie", "forky")})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Packages["fnm"]) != 1 || strings.Join(next.Packages["fnm"][0].Suites, ",") != "trixie,forky" {
		t.Errorf("fnm = %+v", next.Packages["fnm"])
	}
}

// TestApplyExtendArches (specs/009, T042): una versión publicada solo en
// amd64 gana arm64 sin tocar amd64; un amd64 distinto se rechaza.
func TestApplyExtendArches(t *testing.T) {
	cfg := loadCfg(t)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	only := pub("1.0.0-0~tepache1", "1", "2", "trixie")
	delete(only.Debs, "arm64")
	only.Arches = []string{"amd64"}
	st, _, err := Empty().Apply(cfg, now, Add{"fnm", only})
	if err != nil {
		t.Fatal(err)
	}
	next, ch, err := st.Apply(cfg, now.Add(time.Hour), Add{"fnm", pub("1.0.0-0~tepache1", "1", "2", "trixie")})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := next.Find("fnm", "1.0.0-0~tepache1")
	if p.Arches != nil || len(p.Debs) != 2 || next.Serial != st.Serial+1 {
		t.Errorf("ampliada = %+v", p)
	}
	if len(ch) != 1 || strings.Join(ch[0].Arches, ",") != "arm64" || ch[0].Reason != "ampliada a arquitecturas nuevas" {
		t.Errorf("cambios = %+v", ch)
	}
	if _, _, err := st.Apply(cfg, now, Add{"fnm", pub("1.0.0-0~tepache1", "9", "2", "trixie")}); err == nil ||
		!strings.Contains(err.Error(), "ya publicada con otro sha256") {
		t.Errorf("amd64 distinto: %v", err)
	}
}
