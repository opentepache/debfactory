// Package zon lee el subconjunto de ZON (Zig Object Notation) que usan los
// build.zig.zon: literales anónimos .{ … } con campos .nombre = valor,
// cadenas, enums .literal, números y booleanos. No evalúa código
// (specs/009-hosted-hardened-builds, research D2).
package zon

import (
	"fmt"
	"strings"
	"unicode"
)

// Dependency es una dependencia de build.zig.zon: remota (URL y hash) o
// local (Path).
type Dependency struct {
	Name string
	URL  string
	Hash string
	Path string
	Lazy bool
}

// Dependencies devuelve las dependencias de un build.zig.zon en su orden.
func Dependencies(src []byte) ([]Dependency, error) {
	p := &parser{s: string(src)}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(p.s) {
		return nil, p.errf("texto sobrante")
	}
	root, ok := v.(*object)
	if !ok {
		return nil, fmt.Errorf("build.zig.zon: la raíz no es un literal .{…}")
	}
	deps, ok := root.get("dependencies")
	if !ok {
		return nil, nil
	}
	dobj, ok := deps.(*object)
	if !ok {
		return nil, fmt.Errorf("build.zig.zon: dependencies no es un literal .{…}")
	}
	var out []Dependency
	for _, name := range dobj.keys {
		d, ok := dobj.fields[name].(*object)
		if !ok {
			return nil, fmt.Errorf("build.zig.zon: dependencies.%s no es un literal .{…}", name)
		}
		dep := Dependency{Name: name}
		dep.URL, _ = d.str("url")
		dep.Hash, _ = d.str("hash")
		dep.Path, _ = d.str("path")
		if v, ok := d.get("lazy"); ok {
			dep.Lazy, _ = v.(bool)
		}
		switch {
		case dep.Path != "" && (dep.URL != "" || dep.Hash != ""):
			return nil, fmt.Errorf("build.zig.zon: dependencies.%s tiene path y url a la vez", name)
		case dep.Path == "" && (dep.URL == "" || dep.Hash == ""):
			return nil, fmt.Errorf("build.zig.zon: dependencies.%s sin url o sin hash", name)
		}
		out = append(out, dep)
	}
	return out, nil
}

type object struct {
	keys   []string
	fields map[string]any
}

func (o *object) get(k string) (any, bool) { v, ok := o.fields[k]; return v, ok }

func (o *object) str(k string) (string, bool) {
	v, ok := o.fields[k]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

type enumLit string

type parser struct {
	s string
	i int
}

func (p *parser) errf(format string, args ...any) error {
	line := 1 + strings.Count(p.s[:p.i], "\n")
	return fmt.Errorf("build.zig.zon:%d: %s", line, fmt.Sprintf(format, args...))
}

// skip salta espacios y comentarios de línea.
func (p *parser) skip() {
	for p.i < len(p.s) {
		switch {
		case unicode.IsSpace(rune(p.s[p.i])):
			p.i++
		case strings.HasPrefix(p.s[p.i:], "//"):
			if j := strings.IndexByte(p.s[p.i:], '\n'); j >= 0 {
				p.i += j + 1
			} else {
				p.i = len(p.s)
			}
		default:
			return
		}
	}
}

func (p *parser) peek(prefix string) bool { p.skip(); return strings.HasPrefix(p.s[p.i:], prefix) }

func (p *parser) expect(tok string) error {
	if !p.peek(tok) {
		return p.errf("se esperaba %q", tok)
	}
	p.i += len(tok)
	return nil
}

func (p *parser) value() (any, error) {
	p.skip()
	switch {
	case p.peek(".{"):
		return p.literal()
	case p.peek("\""):
		return p.str()
	case p.peek("."):
		p.i++
		id, err := p.ident()
		return enumLit(id), err
	case p.peek("true"):
		p.i += 4
		return true, nil
	case p.peek("false"):
		p.i += 5
		return false, nil
	case p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '-'):
		j := p.i + 1
		for j < len(p.s) && (unicode.IsLetter(rune(p.s[j])) || unicode.IsDigit(rune(p.s[j])) || p.s[j] == '_' || p.s[j] == '.') {
			j++
		}
		n := p.s[p.i:j]
		p.i = j
		return n, nil
	}
	return nil, p.errf("valor inesperado")
}

// literal lee .{ .campo = valor, … } o una tupla .{ valor, … }.
func (p *parser) literal() (any, error) {
	if err := p.expect(".{"); err != nil {
		return nil, err
	}
	obj := &object{fields: map[string]any{}}
	var tuple []any
	for {
		if p.peek("}") {
			p.i++
			break
		}
		if p.peek(".") && !p.peek(".{") && p.isField() {
			p.i++
			key, err := p.ident()
			if err != nil {
				return nil, err
			}
			if err := p.expect("="); err != nil {
				return nil, err
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			if _, dup := obj.fields[key]; dup {
				return nil, p.errf("campo .%s repetido", key)
			}
			obj.keys = append(obj.keys, key)
			obj.fields[key] = v
		} else {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			tuple = append(tuple, v)
		}
		if p.peek(",") {
			p.i++
			continue
		}
		if err := p.expect("}"); err != nil {
			return nil, err
		}
		break
	}
	if len(tuple) > 0 && len(obj.keys) > 0 {
		return nil, p.errf("literal mezcla campos y valores")
	}
	if len(tuple) > 0 {
		return tuple, nil
	}
	return obj, nil
}

// isField indica si en la posición actual hay ".nombre =".
func (p *parser) isField() bool {
	save := p.i
	defer func() { p.i = save }()
	p.i++
	if _, err := p.ident(); err != nil {
		return false
	}
	return p.peek("=")
}

func (p *parser) ident() (string, error) {
	if strings.HasPrefix(p.s[p.i:], "@\"") {
		p.i++
		return p.str()
	}
	j := p.i
	for j < len(p.s) && (p.s[j] == '_' || unicode.IsLetter(rune(p.s[j])) || j > p.i && unicode.IsDigit(rune(p.s[j]))) {
		j++
	}
	if j == p.i {
		return "", p.errf("se esperaba un identificador")
	}
	id := p.s[p.i:j]
	p.i = j
	return id, nil
}

// str lee una cadena entre comillas con los escapes habituales.
func (p *parser) str() (string, error) {
	if err := p.expect("\""); err != nil {
		return "", err
	}
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch c {
		case '"':
			p.i++
			return b.String(), nil
		case '\\':
			if p.i+1 >= len(p.s) {
				return "", p.errf("escape incompleto")
			}
			switch e := p.s[p.i+1]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\', '"', '\'':
				b.WriteByte(e)
			default:
				return "", p.errf("escape \\%c no admitido", e)
			}
			p.i += 2
		case '\n':
			return "", p.errf("cadena sin cerrar")
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return "", p.errf("cadena sin cerrar")
}
