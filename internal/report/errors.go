// Package report define los errores tipados de debfactory y cómo se muestran
// (una línea legible o una línea JSON por evento).
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Stage es la etapa del pipeline en la que ocurre un fallo.
type Stage string

// Etapas, en el orden del pipeline (constitución, Flujo; data-model.md).
const (
	Validate Stage = "validate"
	Lock     Stage = "lock"
	Fetch    Stage = "fetch"
	Verify   Stage = "verify"
	License  Stage = "license"
	Assemble Stage = "assemble"
	Package  Stage = "package"
	Lintian  Stage = "lintian"
	Test     Stage = "test"

	// Etapas de la publicación (specs/002-signed-apt-repo/contracts/cli.md).
	State      Stage = "state"
	Inputs     Stage = "inputs"
	Generate   Stage = "generate"
	Sign       Stage = "sign"
	VerifyRepo Stage = "verify-repo"
	Swap       Stage = "swap"
	Sync       Stage = "sync"
	LockRepo   Stage = "lock-repo"

	// Etapas del pipeline (specs/003-ci-pipeline/data-model.md §1).
	Plan       Stage = "plan"
	Audit      Stage = "audit"
	Notify     Stage = "notify"
	Expiry     Stage = "expiry"
	StatePRAge Stage = "state-pr-age"
	Config     Stage = "config"
	Code       Stage = "code"
	Compare    Stage = "compare"
)

// Stages lista las etapas en orden.
var Stages = []Stage{Validate, Lock, Fetch, Verify, License, Assemble, Package, Lintian, Test}

// Error es un fallo de una etapa. Se imprime como
//
//	debfactory: <pkg> <version> <arch>[ <suite>]: <stage>: <causa>
//	  <detalle>
//
// con "-" en los campos vacíos (contracts/cli.md). La suite solo aparece si
// el fallo es de una suite concreta (FR-033 de la fase 002).
type Error struct {
	Package string
	Version string
	Arch    string
	Suite   string
	Stage   Stage
	Cause   string
	Detail  []string
}

// New crea un error de etapa sin contexto de paquete.
func New(stage Stage, cause string, detail ...string) *Error {
	return &Error{Stage: stage, Cause: cause, Detail: detail}
}

// Newf es New con formato.
func Newf(stage Stage, format string, args ...any) *Error {
	return &Error{Stage: stage, Cause: fmt.Sprintf(format, args...)}
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "debfactory: %s %s %s", dash(e.Package), dash(e.Version), dash(e.Arch))
	if e.Suite != "" {
		b.WriteString(" " + e.Suite)
	}
	fmt.Fprintf(&b, ": %s: %s", e.Stage, e.Cause)
	for _, d := range e.Detail {
		b.WriteString("\n  ")
		b.WriteString(d)
	}
	return b.String()
}

// MarshalJSON da la forma de --json: un objeto por error.
func (e *Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Event   string   `json:"event"`
		Package string   `json:"package,omitempty"`
		Version string   `json:"version,omitempty"`
		Arch    string   `json:"arch,omitempty"`
		Suite   string   `json:"suite,omitempty"`
		Stage   Stage    `json:"stage"`
		Cause   string   `json:"cause"`
		Detail  []string `json:"detail,omitempty"`
	}{"error", e.Package, e.Version, e.Arch, e.Suite, e.Stage, e.Cause, e.Detail})
}

// Wrap convierte err en *Error de la etapa dada y completa el contexto que
// falte. Si err ya es un *Error conserva su etapa y su causa.
func Wrap(err error, stage Stage, pkg, version, arch string) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		c := *e
		if c.Package == "" {
			c.Package = pkg
		}
		if c.Version == "" {
			c.Version = version
		}
		if c.Arch == "" {
			c.Arch = arch
		}
		return &c
	}
	return &Error{Package: pkg, Version: version, Arch: arch, Stage: stage, Cause: err.Error()}
}

// Errors agrupa varios errores (por ejemplo, todos los de un manifiesto).
type Errors []*Error

func (es Errors) Error() string {
	lines := make([]string, len(es))
	for i, e := range es {
		lines[i] = e.Error()
	}
	return strings.Join(lines, "\n")
}

// Err devuelve nil si no hay errores; si no, la lista como error.
func (es Errors) Err() error {
	if len(es) == 0 {
		return nil
	}
	return es
}

// EnvError indica un entorno incompleto (falta Docker, binfmt, aptly, gpg,
// rclone, el llavero de firma o sus permisos), un entorno de GitHub incompleto
// o un nivel de confianza equivocado (debfactory ci). main lo traduce al
// código de salida 3.
type EnvError struct {
	Cause string
}

func (e *EnvError) Error() string { return "debfactory: entorno: " + e.Cause }

// MarshalJSON da la forma de --json.
func (e *EnvError) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Event string `json:"event"`
		Cause string `json:"cause"`
	}{"environment", e.Cause})
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
