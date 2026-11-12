package manifest

import (
	"fmt"
	"strings"
)

// Vars son las variables de plantilla permitidas en un campo y sus valores.
type Vars map[string]string

// Render sustituye literalmente cada {{clave}} de vars. Una {{x}} que no esté
// en vars es un error: las plantillas no tienen lógica (research D5).
func Render(s string, vars Vars) (string, error) {
	var b strings.Builder
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		j := strings.Index(s[i+2:], "}}")
		if j < 0 {
			return "", fmt.Errorf("plantilla sin cerrar en %q", s[i:])
		}
		name := s[i+2 : i+2+j]
		v, ok := vars[name]
		if !ok {
			return "", fmt.Errorf("variable desconocida {{%s}}", name)
		}
		b.WriteString(s[:i])
		b.WriteString(v)
		s = s[i+2+j+2:]
	}
}

// RenderedTag es el tag upstream (la plantilla solo admite {{version}}).
func (m *Manifest) RenderedTag() (string, error) {
	return Render(m.Tag, Vars{"version": m.Version})
}

// Vars devuelve {{version}}, {{tag}} y {{arch}} (token upstream de la
// arquitectura Debian dada; vacío si arch es "").
func (m *Manifest) Vars(arch string) (Vars, error) {
	tag, err := m.RenderedTag()
	if err != nil {
		return nil, err
	}
	v := Vars{"version": m.Version, "tag": tag}
	if arch != "" {
		token, ok := m.ArchMap[arch]
		if !ok {
			return nil, fmt.Errorf("arch_map: falta %s", arch)
		}
		v["arch"] = token
	}
	return v, nil
}

// SrcVars son las variables de install[].src: las de Vars más {{name}} cuando
// el artefacto es un binario suelto.
func (m *Manifest) SrcVars(arch string) (Vars, error) {
	v, err := m.Vars(arch)
	if err != nil {
		return nil, err
	}
	if m.Artifact.Format == "binary" {
		v["name"] = m.Name
	}
	return v, nil
}

// RenderURL renderiza artifact.url (o cualquier plantilla con las variables de
// la arquitectura).
func (m *Manifest) RenderURL(tmpl, arch string) (string, error) {
	v, err := m.Vars(arch)
	if err != nil {
		return "", err
	}
	return Render(tmpl, v)
}
