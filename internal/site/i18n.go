package site

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Langs son los idiomas del sitio, en orden: el primero va en la raíz y los
// demás bajo /<código>/ (FR-016).
var Langs = []string{"en", "es"}

// Strings son los textos fijos de un idioma (i18n/<código>.yaml).
type Strings map[string]string

// LoadI18n lee las traducciones embebidas. Falla si los idiomas no tienen
// exactamente las mismas claves o si alguna está vacía (research D7).
func LoadI18n() (map[string]Strings, error) {
	out := map[string]Strings{}
	for _, l := range Langs {
		data, err := i18nFS.ReadFile("i18n/" + l + ".yaml")
		if err != nil {
			return nil, siteErr("i18n/%s.yaml: %v", l, err)
		}
		s := Strings{}
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&s); err != nil {
			return nil, siteErr("i18n/%s.yaml: %v", l, err)
		}
		for k, v := range s {
			if strings.TrimSpace(v) == "" {
				return nil, siteErr("i18n/%s.yaml: %s está vacía", l, k)
			}
		}
		out[l] = s
	}
	base := keys(out[Langs[0]])
	for _, l := range Langs[1:] {
		for k, v := range out[Langs[0]] {
			if w, ok := out[l][k]; ok && verbs(v) != verbs(w) {
				return nil, siteErr("i18n/%s.yaml: %s no tiene los mismos marcadores %%s/%%d que i18n/%s.yaml", l, k, Langs[0])
			}
		}
	}
	for _, l := range Langs[1:] {
		if got := keys(out[l]); !slices.Equal(got, base) {
			return nil, siteErr("i18n/%s.yaml no tiene las mismas claves que i18n/%s.yaml: %s",
				l, Langs[0], diff(base, got))
		}
	}
	return out, nil
}

// verbs devuelve los marcadores de formato de un texto, en orden.
func verbs(s string) string {
	var out []byte
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' {
			out = append(out, s[i+1])
			i++
		}
	}
	return string(out)
}

func keys(s Strings) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func diff(a, b []string) string {
	var out []string
	for _, k := range a {
		if !slices.Contains(b, k) {
			out = append(out, "falta "+k)
		}
	}
	for _, k := range b {
		if !slices.Contains(a, k) {
			out = append(out, "sobra "+k)
		}
	}
	return fmt.Sprint(strings.Join(out, ", "))
}
