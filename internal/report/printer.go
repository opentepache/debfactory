package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Printer escribe resultados en Out y errores en Err, como texto o como una
// línea JSON por evento (--json).
type Printer struct {
	JSON bool
	Out  io.Writer
	Err  io.Writer
}

// outMu serializa las escrituras: con tareas en paralelo, cada línea (o cada
// bloque de Buffered) sale entera.
var outMu sync.Mutex

func write(w io.Writer, b []byte) {
	outMu.Lock()
	defer outMu.Unlock()
	w.Write(b)
}

// Buffered devuelve un Printer que acumula la salida de una tarea en paralelo
// y una función que la vuelca de una vez, sin mezclarse con la de otras.
func (p *Printer) Buffered() (*Printer, func()) {
	var out, errb bytes.Buffer
	return &Printer{JSON: p.JSON, Out: &out, Err: &errb}, func() {
		outMu.Lock()
		defer outMu.Unlock()
		p.Out.Write(out.Bytes())
		p.Err.Write(errb.Bytes())
	}
}

// Result imprime "<pkg> <version> <arch>: <key>: <value>".
func (p *Printer) Result(pkg, version, arch, key, value string) {
	if p.JSON {
		p.json(p.Out, map[string]string{
			"event": key, "package": pkg, "version": version, "arch": arch, "value": value,
		})
		return
	}
	write(p.Out, fmt.Appendf(nil, "%s %s %s: %s: %s\n", dash(pkg), dash(version), dash(arch), key, value))
}

// Line imprime una línea libre (tablas, resúmenes). En modo JSON se omite.
func (p *Printer) Line(format string, args ...any) {
	if p.JSON {
		return
	}
	write(p.Out, fmt.Appendf(nil, format+"\n", args...))
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
	write(p.Err, []byte(err.Error()+"\n"))
}

func (p *Printer) json(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		write(w, fmt.Appendf(nil, "{\"event\":\"error\",\"cause\":%q}\n", err.Error()))
		return
	}
	write(w, append(b, '\n'))
}

// Event imprime text en modo texto o el objeto ev (con "event": name) en
// modo JSON.
func (p *Printer) Event(name string, ev map[string]any, text string) {
	if p.JSON {
		out := map[string]any{"event": name}
		for k, v := range ev {
			out[k] = v
		}
		p.json(p.Out, out)
		return
	}
	write(p.Out, []byte(text+"\n"))
}
