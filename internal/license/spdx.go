// Package license implementa el gate de licencias (constitución IV): parser de
// expresiones SPDX, detección sobre el texto con licensecheck y allowlist.
package license

import (
	"fmt"
	"strings"
	"unicode"
)

// Expr es una expresión SPDX ya analizada.
type Expr interface {
	isExpr()
}

// Term es una licencia: un identificador, quizá con "+" y una excepción WITH.
type Term struct {
	ID        string
	Plus      bool
	Exception string
}

// And exige todas sus partes.
type And []Expr

// Or admite cualquiera de sus partes.
type Or []Expr

func (Term) isExpr() {}
func (And) isExpr()  {}
func (Or) isExpr()   {}

// String devuelve el término como en SPDX ("GPL-2.0-only WITH X", "Foo+").
func (t Term) String() string {
	s := t.ID
	if t.Plus {
		s += "+"
	}
	if t.Exception != "" {
		s += " WITH " + t.Exception
	}
	return s
}

// Parse analiza una expresión SPDX (identificadores, OR, AND, WITH y
// paréntesis; los operadores en mayúsculas). AND tiene más precedencia que OR.
func Parse(s string) (Expr, error) {
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	e, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("sobra %q", p.toks[p.pos])
	}
	return e, nil
}

// Terms devuelve los términos de la expresión en orden de aparición.
func Terms(e Expr) []Term {
	switch v := e.(type) {
	case Term:
		return []Term{v}
	case And:
		return termsOf(v)
	case Or:
		return termsOf(v)
	}
	return nil
}

func termsOf(parts []Expr) []Term {
	var out []Term
	for _, p := range parts {
		out = append(out, Terms(p)...)
	}
	return out
}

// Allowed evalúa la expresión contra la allowlist: con OR basta una
// alternativa y con AND hacen falta todas. Si no se cumple, devuelve el primer
// término que falla.
func Allowed(e Expr, allowed func(term string) bool) (bool, string) {
	switch v := e.(type) {
	case Term:
		if allowed(v.String()) {
			return true, ""
		}
		return false, v.String()
	case And:
		for _, p := range v {
			if ok, bad := Allowed(p, allowed); !ok {
				return false, bad
			}
		}
		return true, ""
	case Or:
		first := ""
		for _, p := range v {
			ok, bad := Allowed(p, allowed)
			if ok {
				return true, ""
			}
			if first == "" {
				first = bad
			}
		}
		return false, first
	}
	return false, ""
}

type parser struct {
	toks []string
	pos  int
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *parser) or() (Expr, error) {
	left, err := p.and()
	if err != nil {
		return nil, err
	}
	parts := []Expr{left}
	for p.peek() == "OR" {
		p.pos++
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		parts = append(parts, right)
	}
	if len(parts) == 1 {
		return left, nil
	}
	return Or(parts), nil
}

func (p *parser) and() (Expr, error) {
	left, err := p.with()
	if err != nil {
		return nil, err
	}
	parts := []Expr{left}
	for p.peek() == "AND" {
		p.pos++
		right, err := p.with()
		if err != nil {
			return nil, err
		}
		parts = append(parts, right)
	}
	if len(parts) == 1 {
		return left, nil
	}
	return And(parts), nil
}

func (p *parser) with() (Expr, error) {
	e, err := p.primary()
	if err != nil {
		return nil, err
	}
	if p.peek() != "WITH" {
		return e, nil
	}
	p.pos++
	t, ok := e.(Term)
	if !ok {
		return nil, fmt.Errorf("WITH solo puede seguir a un identificador")
	}
	exc := p.peek()
	if !isID(exc) {
		return nil, fmt.Errorf("falta la excepción tras WITH")
	}
	p.pos++
	t.Exception = exc
	return t, nil
}

func (p *parser) primary() (Expr, error) {
	tok := p.peek()
	switch {
	case tok == "":
		return nil, fmt.Errorf("expresión incompleta")
	case tok == "(":
		p.pos++
		e, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("falta \")\"")
		}
		p.pos++
		return e, nil
	case isID(tok):
		p.pos++
		t := Term{ID: tok}
		if strings.HasSuffix(tok, "+") {
			t.ID, t.Plus = strings.TrimSuffix(tok, "+"), true
		}
		return t, nil
	}
	return nil, fmt.Errorf("se esperaba un identificador y hay %q", tok)
}

func isID(tok string) bool {
	if tok == "" || tok == "AND" || tok == "OR" || tok == "WITH" {
		return false
	}
	body := strings.TrimSuffix(tok, "+")
	if body == "" {
		return false
	}
	for _, r := range body {
		if !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '.' || r == '-' || r == ':') {
			return false
		}
	}
	return true
}

func tokenize(s string) ([]string, error) {
	var toks []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '(' || r == ')':
			flush()
			toks = append(toks, string(r))
		case r == ' ' || r == '\t':
			flush()
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '.' || r == '-' || r == '+' || r == ':':
			cur.WriteRune(r)
		default:
			return nil, fmt.Errorf("carácter no válido %q", r)
		}
	}
	flush()
	if len(toks) == 0 {
		return nil, fmt.Errorf("expresión vacía")
	}
	return toks, nil
}
