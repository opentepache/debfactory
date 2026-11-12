package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Printer escribe resultados en Out y errores en Err, como texto o como una
// línea JSON por evento (--json).
type Printer struct {
	JSON bool
	Out  io.Writer
	Err  io.Writer
}

// Result imprime "<pkg> <version> <arch>: <key>: <value>".
func (p *Printer) Result(pkg, version, arch, key, value string) {
	if p.JSON {
		p.json(p.Out, map[string]string{
			"event": key, "package": pkg, "version": version, "arch": arch, "value": value,
		})
		return
	}
	fmt.Fprintf(p.Out, "%s %s %s: %s: %s\n", dash(pkg), dash(version), dash(arch), key, value)
}

// Line imprime una línea libre (tablas, resúmenes). En modo JSON se omite.
func (p *Printer) Line(format string, args ...any) {
	if p.JSON {
		return
	}
	fmt.Fprintf(p.Out, format+"\n", args...)
}

// Fail imprime un error; si es una lista, un error por línea.
func (p *Printer) Fail(err error) {
	var list Errors
	if errors.As(err, &list) {
		for _, e := range list {
			p.Fail(e)
		}
		return
	}
	if p.JSON {
		var e *Error
		var env *EnvError
		switch {
		case errors.As(err, &e):
			p.json(p.Err, e)
		case errors.As(err, &env):
			p.json(p.Err, env)
		default:
			p.json(p.Err, map[string]string{"event": "error", "cause": err.Error()})
		}
		return
	}
	fmt.Fprintln(p.Err, err.Error())
}

func (p *Printer) json(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(w, "{\"event\":\"error\",\"cause\":%q}\n", err.Error())
		return
	}
	fmt.Fprintln(w, string(b))
}
