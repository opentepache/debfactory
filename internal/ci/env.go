// Package ci orquesta el pipeline de GitHub Actions (specs/003-ci-pipeline):
// plan de paquetes afectados, ejecuciones de main y de pull requests, PR de
// estado, auditoría y notificaciones. No reimplementa ninguna etapa: llama a
// las de las fases 001 y 002.
package ci

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/report"
)

// Env es el contexto del job, leído de las variables de GitHub
// (contracts/cli.md § Entorno). Token nunca se imprime.
type Env struct {
	EventName   string // GITHUB_EVENT_NAME
	Ref         string // GITHUB_REF
	SHA         string // GITHUB_SHA
	Repository  string // GITHUB_REPOSITORY
	RunID       string // GITHUB_RUN_ID
	ServerURL   string // GITHUB_SERVER_URL
	APIURL      string // GITHUB_API_URL
	StepSummary string // GITHUB_STEP_SUMMARY
	RunnerTemp  string // RUNNER_TEMP
	Trust       string // "ci", "pub" o "" según RUNNER_LABELS
	Actions     bool   // GITHUB_ACTIONS=true: corre dentro de un job

	Token string // GITHUB_TOKEN

	// Detector de versiones (specs/005-upstream-release-watch): token de
	// instalación de la GitHub App (nunca se imprime) y su slug, salidas de
	// actions/create-github-app-token en watch.yml.
	AppToken string // DEBFACTORY_APP_TOKEN
	AppSlug  string // DEBFACTORY_APP_SLUG

	vars map[string]string
}

// envKeys son las variables que se leen del entorno o de --env-file.
var envKeys = []string{
	"GITHUB_EVENT_NAME", "GITHUB_REF", "GITHUB_SHA", "GITHUB_REPOSITORY", "GITHUB_RUN_ID",
	"GITHUB_SERVER_URL", "GITHUB_API_URL", "GITHUB_STEP_SUMMARY", "RUNNER_TEMP", "RUNNER_LABELS",
	"GITHUB_TOKEN", "GITHUB_ACTIONS", "INPUT_ACTION", "INPUT_PACKAGE", "INPUT_VERSION", "INPUT_REASON", "INPUT_LAST",
	"INPUT_FORCE", "DEBFACTORY_APP_TOKEN", "DEBFACTORY_APP_SLUG",
}

// LoadEnv lee el contexto del entorno del proceso y, si envFile no está
// vacío, de ese archivo (CLAVE=valor por línea; el archivo manda).
func LoadEnv(cfg *config.Config, envFile string) (*Env, error) {
	vars := map[string]string{}
	for _, k := range envKeys {
		if v, ok := os.LookupEnv(k); ok {
			vars[k] = v
		}
	}
	if envFile != "" {
		f, err := os.Open(envFile)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for n := 1; sc.Scan(); n++ {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				return nil, fmt.Errorf("%s:%d: se esperaba CLAVE=valor", envFile, n)
			}
			vars[strings.TrimSpace(k)] = v
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return newEnv(cfg, vars), nil
}

func newEnv(cfg *config.Config, vars map[string]string) *Env {
	e := &Env{
		EventName:   vars["GITHUB_EVENT_NAME"],
		Ref:         vars["GITHUB_REF"],
		SHA:         vars["GITHUB_SHA"],
		Repository:  vars["GITHUB_REPOSITORY"],
		RunID:       vars["GITHUB_RUN_ID"],
		ServerURL:   vars["GITHUB_SERVER_URL"],
		APIURL:      vars["GITHUB_API_URL"],
		StepSummary: vars["GITHUB_STEP_SUMMARY"],
		RunnerTemp:  vars["RUNNER_TEMP"],
		Token:       vars["GITHUB_TOKEN"],
		AppToken:    vars["DEBFACTORY_APP_TOKEN"],
		AppSlug:     vars["DEBFACTORY_APP_SLUG"],
		Actions:     vars["GITHUB_ACTIONS"] == "true",
		vars:        vars,
	}
	if e.ServerURL == "" {
		e.ServerURL = "https://github.com"
	}
	if e.APIURL == "" {
		e.APIURL = "https://api.github.com"
	}
	labels := strings.FieldsFunc(vars["RUNNER_LABELS"], func(r rune) bool { return r == ',' || r == ' ' })
	switch {
	case slices.Contains(labels, cfg.CI.Labels.Pub):
		e.Trust = "pub"
	case slices.Contains(labels, cfg.CI.Labels.CI):
		e.Trust = "ci"
	}
	return e
}

// String describe el contexto sin el token.
func (e *Env) String() string {
	return fmt.Sprintf("%s %s@%s en %s (nivel %q, ejecución %s)",
		e.Repository, e.EventName, e.Ref, short(e.SHA), e.Trust, e.RunID)
}

// GoString evita que %#v imprima el token.
func (e *Env) GoString() string { return e.String() }

// Format hace que %v y %+v tampoco lo impriman.
func (e *Env) Format(f fmt.State, _ rune) { fmt.Fprint(f, e.String()) }

// RunURL es el enlace a la ejecución.
func (e *Env) RunURL() string {
	if e == nil || e.Repository == "" || e.RunID == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/actions/runs/%s", e.ServerURL, e.Repository, e.RunID)
}

// RequireWriter es la segunda barrera tras el hook del runner (research
// D3): solo el nivel pub, con push o workflow_dispatch sobre main del
// repositorio configurado, puede escribir. Si no, código 3.
func (e *Env) RequireWriter(cfg *config.Config) error {
	if cfg.CI.Repository == "" {
		return &report.EnvError{Cause: "ci.repository está vacío en " + config.MainFile}
	}
	switch {
	case e.Trust != "pub":
		return &report.EnvError{Cause: fmt.Sprintf("esta orden escribe y solo corre en el runner %s (nivel pub); RUNNER_LABELS no lo incluye", cfg.CI.Labels.Pub)}
	case e.EventName != "push" && e.EventName != "workflow_dispatch":
		return &report.EnvError{Cause: fmt.Sprintf("evento %q: solo push o workflow_dispatch pueden escribir", e.EventName)}
	case e.Ref != "refs/heads/main":
		return &report.EnvError{Cause: fmt.Sprintf("ref %q: solo refs/heads/main puede escribir", e.Ref)}
	case e.Repository != cfg.CI.Repository:
		return &report.EnvError{Cause: fmt.Sprintf("repositorio %q: se esperaba %q (ci.repository)", e.Repository, cfg.CI.Repository)}
	case e.Token == "":
		return &report.EnvError{Cause: "falta GITHUB_TOKEN"}
	}
	return nil
}

// RequireWatcher es la barrera de `ci watch` (specs/005-upstream-release-watch,
// contracts/cli.md): solo en un job del runner ci (nunca en el pub, que tiene
// <data> y la llave: FR-012), con workflow_dispatch sobre main del
// repositorio configurado y con el token de la App. Si no, código 3.
func (e *Env) RequireWatcher(cfg *config.Config) error {
	if cfg.CI.Repository == "" {
		return &report.EnvError{Cause: "ci.repository está vacío en " + config.MainFile}
	}
	switch {
	case !e.Actions:
		return &report.EnvError{Cause: "ci watch solo escribe dentro de un job de GitHub Actions; en local usa --dry-run"}
	case e.Trust != "ci":
		return &report.EnvError{Cause: fmt.Sprintf("ci watch solo corre en el runner %s (nivel ci), nunca en el que tiene la llave (FR-012); RUNNER_LABELS no lo cumple", cfg.CI.Labels.CI)}
	case e.EventName != "workflow_dispatch":
		return &report.EnvError{Cause: fmt.Sprintf("evento %q: ci watch solo corre con workflow_dispatch", e.EventName)}
	case e.Ref != "refs/heads/main":
		return &report.EnvError{Cause: fmt.Sprintf("ref %q: ci watch solo corre sobre refs/heads/main", e.Ref)}
	case e.Repository != cfg.CI.Repository:
		return &report.EnvError{Cause: fmt.Sprintf("repositorio %q: se esperaba %q (ci.repository)", e.Repository, cfg.CI.Repository)}
	case e.AppToken == "":
		return &report.EnvError{Cause: "falta DEBFACTORY_APP_TOKEN (token de la GitHub App; specs/005-upstream-release-watch/contracts/workflows.md)"}
	case e.AppSlug == "":
		return &report.EnvError{Cause: "falta DEBFACTORY_APP_SLUG (salida app-slug de actions/create-github-app-token)"}
	case e.Token == "":
		return &report.EnvError{Cause: "falta GITHUB_TOKEN"}
	}
	return nil
}

// WatchInputs lee INPUT_PACKAGE e INPUT_FORCE de watch.yml.
func (e *Env) WatchInputs() (pkg string, force bool, err error) {
	pkg, force = e.vars["INPUT_PACKAGE"], e.vars["INPUT_FORCE"] == "true"
	if pkg != "" && !pkgNameRe.MatchString(pkg) {
		return "", false, &InputError{fmt.Sprintf("package %q: nombre de paquete inválido", pkg)}
	}
	return pkg, force, nil
}

// Inputs son los parámetros de workflow_dispatch (contracts/workflows.md).
// Llegan por variables de entorno y nunca se interpolan en un script.
type Inputs struct {
	Action, Package, Version, Reason string
	Last                             bool
}

var (
	pkgNameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
	debVersion  = regexp.MustCompile(`^[0-9][A-Za-z0-9.+~-]*$`)
	validAction = []string{"rebuild", "retire", "refresh", "restore-state-pr"}
)

// Inputs lee y valida los INPUT_*. Una acción vacía es una ejecución de
// main. Un input inválido es un uso incorrecto (código 2): lo devuelve como
// *InputError.
func (e *Env) Inputs() (Inputs, error) {
	in := Inputs{
		Action:  e.vars["INPUT_ACTION"],
		Package: e.vars["INPUT_PACKAGE"],
		Version: e.vars["INPUT_VERSION"],
		Reason:  e.vars["INPUT_REASON"],
		Last:    e.vars["INPUT_LAST"] == "true",
	}
	if in.Action == "" || in.Action == "main" {
		in.Action = "main"
		return in, nil
	}
	if !slices.Contains(validAction, in.Action) {
		return in, &InputError{fmt.Sprintf("action %q: debe ser una de %v", in.Action, validAction)}
	}
	if err := ValidatePackageVersion(in.Action, in.Package, in.Version); err != nil {
		return in, err
	}
	if in.Action == "retire" {
		if err := ValidateReason(in.Reason); err != nil {
			return in, err
		}
	}
	return in, nil
}

// ValidatePackageVersion valida paquete y versión de rebuild y retire.
func ValidatePackageVersion(action, pkg, version string) error {
	if action != "rebuild" && action != "retire" {
		return nil
	}
	if !pkgNameRe.MatchString(pkg) {
		return &InputError{fmt.Sprintf("package %q: nombre de paquete inválido", pkg)}
	}
	if !debVersion.MatchString(version) || len(version) > 128 {
		return &InputError{fmt.Sprintf("version %q: versión Debian inválida", version)}
	}
	return nil
}

// ValidateReason exige un motivo de una línea, no vacío.
func ValidateReason(reason string) error {
	if strings.TrimSpace(reason) == "" || strings.ContainsAny(reason, "\n\r") || len(reason) > 500 {
		return &InputError{"reason: se necesita un motivo de una línea"}
	}
	return nil
}

// InputError es un parámetro inválido (código 2).
type InputError struct{ Msg string }

func (e *InputError) Error() string { return "debfactory: " + e.Msg }

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// sha12 abrevia un sha256 para los mensajes.
func sha12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
