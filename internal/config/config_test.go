package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/testutil"
)

func TestLoadRepoConfig(t *testing.T) {
	c, err := Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range c.Suites {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "bookworm,trixie,sid,noble,resolute" {
		t.Errorf("orden de suites = %s", got)
	}
	if s, _ := c.Suite("noble"); s.Image != "ubuntu:noble" || s.Level2Suffix != "u2404" {
		t.Errorf("noble = %+v", s)
	}
	if c.Lintian.Overridable("embedded-library") || c.Lintian.Overridable("copyright-foo") {
		t.Error("etiquetas protegidas marcadas como silenciables")
	}
	if !c.Lintian.Overridable("statically-linked-binary") {
		t.Error("statically-linked-binary debería poder silenciarse")
	}
	if !c.LicenseAllowed("MIT") || c.LicenseAllowed("BUSL-1.1") {
		t.Error("allowlist incorrecta")
	}
	if c.Repo.Validity != (Validity{Days: 30, WarnDays: 7}) {
		t.Errorf("repo.validity = %+v", c.Repo.Validity)
	}
	if c.CI.StatePRMaxAge != Duration(7*24*time.Hour) || c.CI.Labels.Pub != "debfactory-main" || c.CI.Destination != "r2" {
		t.Errorf("ci = %+v", c.CI)
	}
	w := c.Watch
	if w.IntervalDuration() != 24*time.Hour || w.CooldownDuration() != 48*time.Hour || w.Reserve() != 500 ||
		w.Prefix() != "debfactory/update/" {
		t.Errorf("watch = %v %v %d %q", w.IntervalDuration(), w.CooldownDuration(), w.Reserve(), w.Prefix())
	}
}

// TestWatchDefaults: sin bloque watch valen los defectos de data-model.md §2,
// y cooldown: 0h es un valor válido (no el defecto).
func TestWatchDefaults(t *testing.T) {
	base, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), MainFile))
	if err != nil {
		t.Fatal(err)
	}
	lic, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), LicensesFile))
	if err != nil {
		t.Fatal(err)
	}
	head, _, ok := strings.Cut(string(base), "\nwatch:")
	if !ok {
		t.Fatal("falta el bloque watch en la configuración de prueba")
	}
	load := func(extra string) *Config {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, MainFile), head+"\n"+extra)
		mustWrite(t, filepath.Join(root, LicensesFile), string(lic))
		c, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	w := load("").Watch
	if w.IntervalDuration() != DefaultWatchInterval || w.CooldownDuration() != DefaultWatchCooldown ||
		w.Reserve() != DefaultRateReserve || w.Prefix() != DefaultBranchPrefix {
		t.Errorf("defectos = %v %v %d %q", w.IntervalDuration(), w.CooldownDuration(), w.Reserve(), w.Prefix())
	}
	w = load("watch:\n  cooldown: 0h\n  rate_reserve: 0\n  interval: 2d\n").Watch
	if w.CooldownDuration() != 0 || w.Reserve() != 0 || w.IntervalDuration() != 48*time.Hour {
		t.Errorf("valores explícitos = %v %d %v", w.CooldownDuration(), w.Reserve(), w.IntervalDuration())
	}
}

func TestLoadRejects(t *testing.T) {
	base, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), MainFile))
	if err != nil {
		t.Fatal(err)
	}
	lic, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), LicensesFile))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ old, new, want string }{
		"regex inválida":     {`"^dep5-"`, `"^dep5-("`, "never_override_patterns[2]: regex inválida"},
		"marcador":           {`libgcc-s1`, `libgcc-s1 (>= {gcc})`, "marcador desconocido {gcc}"},
		"campo desconocido":  {`completions_image:`, "colour: red\ncompletions_image:", "field colour not found"},
		"arquitecturas":      {`arches: [amd64, arm64]`, `arches: [amd64]`, "arches: debe ser [amd64, arm64]"},
		"imagen vacía":       {`image: "debian:sid"`, `image: ""`, "suites.sid.image: no puede estar vacío"},
		"suite desconocida":  {`level2_suffix: deb12}`, `level2_suffix: deb12, extra: x}`, `suites.bookworm: propiedad desconocida "extra"`},
		"fail_on incorrecto": {`fail_on: error`, `fail_on: always`, "lintian.fail_on"},
		"keep":               {`keep: 3 `, `keep: 0 `, "repo.keep: mínimo 1"},
		"fingerprint":        {`masters: [72671CD221684BF7FDCDFB3965B278975D4FE04B]`, `masters: [abc123]`, "repo.signing.masters[0]: fingerprint inválido"},
		"subkey":             {`subkey: 84983CE038A7C73647AF0C9A15DDD1383F3A75D5`, `subkey: "84983ce038a7c73647af0c9a15ddd1383f3a75d5"`, "repo.signing.subkey: fingerprint inválido"},
		"nombre de destino":  {`    r2:`, `    R2:`, `repo.destinations: nombre "R2" inválido`},
		"credenciales":       {`"r2:debfactory-apt"`, `"user:secret@host:/srv"`, "repo.destinations.r2.remote: no puede contener credenciales"},
		// Fase 003 (specs/003-ci-pipeline/contracts/config.md).
		"validity.days":                 {`days: 30 `, `days: 7 `, "repo.validity.days debe estar entre 8 y 90"},
		"validity.days alto":            {`days: 30 `, `days: 91 `, "repo.validity.days debe estar entre 8 y 90"},
		"validity.warn_days":            {`warn_days: 7 `, `warn_days: 23 `, "repo.validity.warn_days deja menos de una semana entre refirmados"},
		"ci.repository":                 {`repository: "opentepache/debfactory"`, `repository: "solo-nombre"`, "ci.repository inválido"},
		"ci.tool_repository":            {`tool_repository: "opentepache/debfactory"`, `tool_repository: "x"`, "ci.tool_repository inválido"},
		"ci.destination":                {`destination: r2 `, `destination: r3 `, `ci.destination "r3" no existe`},
		"ci.destination sin public_url": {`destination: r2 `, `destination: local `, `ci.destination "local" no existe`},
		"ci.labels":                     {`pub: debfactory-main`, `pub: debfactory-pr`, "las etiquetas de los dos niveles deben ser distintas"},
		"ci.state_branch":               {`state_branch: debfactory/state`, `state_branch: main`, "ci.state_branch no puede ser main"},
		"ci.state_pr_max_age":           {`state_pr_max_age: 7d`, `state_pr_max_age: siete`, `duración "siete" inválida`},
		// Fase 005 (specs/005-upstream-release-watch/data-model.md §2).
		"watch.interval":            {`interval: 24h`, `interval: 30m`, "watch.interval: mínimo 1h"},
		"watch.cooldown":            {`cooldown: 48h`, `cooldown: -1h`, "watch.cooldown: no puede ser negativo"},
		"watch.rate_reserve":        {`rate_reserve: 500`, `rate_reserve: -1`, "watch.rate_reserve: no puede ser negativo"},
		"watch.branch_prefix":       {`branch_prefix: debfactory/update/`, `branch_prefix: debfactory/update`, "watch.branch_prefix"},
		"watch.branch_prefix main":  {`branch_prefix: debfactory/update/`, `branch_prefix: main/`, "choca con main"},
		"watch.branch_prefix state": {`branch_prefix: debfactory/update/`, `branch_prefix: debfactory/`, "choca con main o con ci.state_branch"},
		"watch desconocido":         {`rate_reserve: 500`, "rate_reserve: 500\n  app: x", "field app not found"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(string(base), tc.old) {
				t.Fatalf("el caso no aplica: falta %q", tc.old)
			}
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, MainFile), strings.Replace(string(base), tc.old, tc.new, 1))
			mustWrite(t, filepath.Join(root, LicensesFile), string(lic))
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; se esperaba que contuviera %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), ": validate: config/debfactory.yaml: ") {
				t.Errorf("formato inesperado: %v", err)
			}
		})
	}
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
