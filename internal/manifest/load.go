package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"debfactory/internal/report"
	"debfactory/schema"
)

const schemaURL = "https://debfactory.invalid/schema/manifest.v1.schema.json"

var (
	schemaOnce     sync.Once
	compiledSchema *jsonschema.Schema
	rawSchema      any
	schemaErr      error
)

func manifestSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.ManifestV1))
		if err != nil {
			schemaErr = fmt.Errorf("schema embebido: %w", err)
			return
		}
		rawSchema = doc
		c := jsonschema.NewCompiler()
		c.AssertFormat()
		if err := c.AddResource(schemaURL, doc); err != nil {
			schemaErr = fmt.Errorf("schema embebido: %w", err)
			return
		}
		compiledSchema, schemaErr = c.Compile(schemaURL)
	})
	return compiledSchema, schemaErr
}

// Path devuelve la ruta relativa del manifiesto de un paquete.
func Path(name string) string { return filepath.Join("packages", name+".yaml") }

// Load lee packages/<name>.yaml bajo root, lo valida contra el schema y aplica
// los defaults. Las reglas semánticas se comprueban aparte (Check).
func Load(root, name string) (*Manifest, error) {
	return LoadFile(root, Path(name))
}

// LoadFile es Load con la ruta relativa del manifiesto.
func LoadFile(root, rel string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, validateErr(FileName(rel), "", rel, "", "no existe el manifiesto")
		}
		return nil, validateErr(FileName(rel), "", rel, "", err.Error())
	}
	return Parse(data, rel)
}

// Parse valida y decodifica el contenido de un manifiesto; rel solo se usa
// para los mensajes.
func Parse(data []byte, rel string) (*Manifest, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, validateErr(FileName(rel), "", rel, "", "YAML inválido: "+err.Error())
	}
	if len(doc.Content) == 0 {
		return nil, validateErr(FileName(rel), "", rel, "", "el archivo está vacío")
	}
	node := doc.Content[0]
	inst, err := toJSON(node)
	if err != nil {
		return nil, validateErr(FileName(rel), "", rel, "", err.Error())
	}
	pkg, version := identity(inst, rel)

	sch, err := manifestSchema()
	if err != nil {
		return nil, validateErr(pkg, version, rel, "", err.Error())
	}
	if err := sch.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if !errors.As(err, &ve) {
			return nil, validateErr(pkg, version, rel, "", err.Error())
		}
		return nil, translate(ve, inst, rel, pkg, version)
	}

	var m Manifest
	if err := node.Decode(&m); err != nil {
		return nil, validateErr(pkg, version, rel, "", err.Error())
	}
	m.File = rel
	applyDefaults(&m)
	return &m, nil
}

// applyDefaults aplica los defaults de data-model.md que no dependen del
// artefacto (mode y test.command se resuelven al ensamblar).
func applyDefaults(m *Manifest) {
	if m.Tag == "" {
		m.Tag = "v{{version}}"
	}
	if m.Section == "" {
		m.Section = "devel"
	}
	if m.Test.Expect == "" {
		m.Test.Expect = "{{version}}"
	}
	if m.Watch.PrereleaseFlag == "" {
		m.Watch.PrereleaseFlag = "trust"
	}
}

// toJSON convierte un nodo YAML en valores compatibles con JSON. Las claves
// duplicadas son un error y las fechas se tratan como texto.
func toJSON(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return toJSON(n.Content[0])
	case yaml.AliasNode:
		return toJSON(n.Alias)
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("línea %d: clave no escalar", k.Line)
			}
			if _, dup := m[k.Value]; dup {
				return nil, fmt.Errorf("línea %d: clave duplicada %q", k.Line, k.Value)
			}
			v, err := toJSON(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := toJSON(c)
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
		return s, nil
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			return nil, nil
		case "!!bool":
			var b bool
			err := n.Decode(&b)
			return b, err
		case "!!int":
			var i int64
			if err := n.Decode(&i); err != nil {
				return n.Value, nil
			}
			return i, nil
		case "!!float":
			var f float64
			err := n.Decode(&f)
			return f, err
		default:
			return n.Value, nil
		}
	}
	return nil, fmt.Errorf("línea %d: nodo YAML no soportado", n.Line)
}

// identity extrae nombre y versión para los mensajes, aunque el manifiesto
// sea inválido.
func identity(inst any, rel string) (pkg, version string) {
	pkg = FileName(rel)
	if m, ok := inst.(map[string]any); ok {
		if s, ok := m["name"].(string); ok && s != "" {
			pkg = s
		}
		if s, ok := m["version"].(string); ok {
			version = s
		}
	}
	return pkg, version
}

// translate convierte los errores de jsonschema en mensajes en español con la
// forma "packages/<f>.yaml: <campo.con.puntos>: <mensaje>".
func translate(ve *jsonschema.ValidationError, inst any, rel, pkg, version string) error {
	var leaves []*jsonschema.ValidationError
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			leaves = append(leaves, e)
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)

	type item struct{ field, msg string }
	var items []item
	for _, l := range leaves {
		field := fieldPath(l.InstanceLocation, inst)
		for _, msg := range messages(l) {
			items = append(items, item{field, msg})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].field != items[j].field {
			return items[i].field < items[j].field
		}
		return items[i].msg < items[j].msg
	})
	items = slices.Compact(items)

	var errs report.Errors
	for _, it := range items {
		errs = append(errs, validateErr(pkg, version, rel, it.field, it.msg))
	}
	return errs
}

var englishPrinter = message.NewPrinter(language.English)

func messages(e *jsonschema.ValidationError) []string {
	switch k := e.ErrorKind.(type) {
	case *kind.Required:
		out := make([]string, len(k.Missing))
		for i, p := range k.Missing {
			out[i] = fmt.Sprintf("falta la propiedad %q", p)
		}
		return out
	case *kind.AdditionalProperties:
		out := make([]string, len(k.Properties))
		for i, p := range k.Properties {
			out[i] = fmt.Sprintf("propiedad desconocida %q", p)
		}
		return out
	case *kind.Enum:
		want := make([]string, len(k.Want))
		for i, w := range k.Want {
			want[i] = fmt.Sprint(w)
		}
		return []string{"valor no permitido (se admite: " + strings.Join(want, ", ") + ")"}
	case *kind.Const:
		return []string{fmt.Sprintf("valor no permitido (se admite: %v)", k.Want)}
	case *kind.Pattern:
		return []string{fmt.Sprintf("no cumple el formato (patrón %s)", k.Want)}
	case *kind.Type:
		return []string{fmt.Sprintf("tipo incorrecto (se esperaba %s, hay %s)",
			strings.Join(k.Want, " o "), k.Got)}
	case *kind.MinLength:
		return []string{fmt.Sprintf("demasiado corto (mínimo %d caracteres)", k.Want)}
	case *kind.MaxLength:
		return []string{fmt.Sprintf("demasiado largo (máximo %d caracteres)", k.Want)}
	case *kind.MinItems:
		return []string{fmt.Sprintf("demasiado pocos elementos (mínimo %d)", k.Want)}
	case *kind.Minimum:
		return []string{fmt.Sprintf("demasiado pequeño (mínimo %s)", k.Want.RatString())}
	case *kind.MinProperties:
		if keys := exactlyOneOf(e.SchemaURL); keys != "" {
			return []string{"debe tener exactamente una de: " + keys}
		}
		return []string{fmt.Sprintf("demasiado pocas propiedades (mínimo %d)", k.Want)}
	case *kind.MaxProperties:
		if keys := exactlyOneOf(e.SchemaURL); keys != "" {
			return []string{"debe tener exactamente una de: " + keys}
		}
		return []string{fmt.Sprintf("demasiadas propiedades (máximo %d)", k.Want)}
	case *kind.Format:
		return []string{fmt.Sprintf("formato inválido (%s)", k.Want)}
	case *kind.FalseSchema:
		return []string{"propiedad no permitida"}
	case *kind.Not:
		// Solo name usa not: nombres reservados por el sitio web (006 research D3).
		return []string{"valor reservado"}
	}
	return []string{e.ErrorKind.LocalizedString(englishPrinter)}
}

// exactlyOneOf devuelve las claves de un objeto del schema que exige
// exactamente una propiedad (minProperties = maxProperties = 1), o "".
func exactlyOneOf(url string) string {
	_, frag, _ := strings.Cut(url, "#")
	var node any = rawSchema
	for _, tok := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
		if tok == "" {
			continue
		}
		tok = strings.NewReplacer("~1", "/", "~0", "~").Replace(tok)
		m, ok := node.(map[string]any)
		if !ok {
			return ""
		}
		node = m[tok]
	}
	m, ok := node.(map[string]any)
	if !ok || !isOne(m["minProperties"]) || !isOne(m["maxProperties"]) {
		return ""
	}
	props, _ := m["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func isOne(v any) bool {
	return fmt.Sprint(v) == "1"
}

// fieldPath escribe la ubicación como "install[1].dst" (los índices de
// listas entre corchetes y las claves separadas por puntos).
func fieldPath(tokens []string, inst any) string {
	var b strings.Builder
	cur := inst
	for _, t := range tokens {
		if list, ok := cur.([]any); ok {
			b.WriteString("[" + t + "]")
			cur = nil
			if i, err := strconv.Atoi(t); err == nil && i < len(list) {
				cur = list[i]
			}
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(t)
		if m, ok := cur.(map[string]any); ok {
			cur = m[t]
		} else {
			cur = nil
		}
	}
	return b.String()
}

func validateErr(pkg, version, rel, field, msg string) *report.Error {
	cause := rel + ": "
	if field != "" {
		cause += field + ": "
	}
	return &report.Error{Package: pkg, Version: version, Stage: report.Validate, Cause: cause + msg}
}
