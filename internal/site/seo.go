package site

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
)

// homeSEO devuelve el título, la meta descripción y el JSON-LD de la portada
// (FR-025).
func homeSEO(m *Model, t Strings, lang string) (title, desc string, ld template.JS) {
	names := make([]string, 0, len(m.Packages))
	for _, p := range m.Packages {
		names = append(names, p.Name)
	}
	title = fmt.Sprintf(t["seo_home_title"], m.Name)
	desc = fmt.Sprintf(t["seo_home_desc"], strings.Join(names, ", "))
	ld = jsonLD(map[string]any{
		"@context":    "https://schema.org",
		"@type":       "WebSite",
		"name":        m.Name,
		"url":         m.URL + langRoot(lang),
		"inLanguage":  lang,
		"description": desc,
	})
	return title, desc, ld
}

// packageSEO devuelve el título, la meta descripción y el JSON-LD de la
// página de un paquete: SoftwareApplication y BreadcrumbList (FR-025).
func packageSEO(m *Model, p *Package, t Strings, lang string) (title, desc string, ld template.JS) {
	cur := p.Versions[0]
	title = fmt.Sprintf(t["seo_pkg_title"], p.Name, cur.Upstream, m.Name)
	summary := p.Summary.EN
	if lang == "es" && p.Summary.ES != nil {
		summary = *p.Summary.ES
	}
	if summary == "" {
		summary = p.Name
	}
	desc = fmt.Sprintf(t["seo_pkg_desc"], p.Name, summary, cur.Available)
	var oses []string
	for _, s := range m.Repo.Suites {
		for _, in := range cur.Suites {
			if s.Name == in {
				oses = append(oses, s.Label)
			}
		}
	}
	app := map[string]any{
		"@type":               "SoftwareApplication",
		"name":                p.Name,
		"softwareVersion":     cur.Upstream,
		"applicationCategory": "DeveloperApplication",
		"operatingSystem":     strings.Join(oses, ", "),
		"datePublished":       cur.Published,
		"url":                 m.URL + langRoot(lang) + "packages/" + p.Name + "/",
		"description":         summary,
		"offers":              map[string]any{"@type": "Offer", "price": "0", "priceCurrency": "USD"},
	}
	if p.License != "" {
		app["license"] = "https://spdx.org/licenses/" + p.License
	}
	if p.Homepage != "" {
		app["sameAs"] = p.Homepage
	}
	crumbs := map[string]any{
		"@type": "BreadcrumbList",
		"itemListElement": []map[string]any{
			{"@type": "ListItem", "position": 1, "name": m.Name, "item": m.URL + langRoot(lang)},
			{"@type": "ListItem", "position": 2, "name": p.Name, "item": app["url"]},
		},
	}
	ld = jsonLD(map[string]any{"@context": "https://schema.org", "@graph": []any{app, crumbs}})
	return title, desc, ld
}

// jsonLD serializa los datos estructurados. encoding/json escapa <, > y &,
// así que el resultado no puede cerrar el <script> que lo contiene.
func jsonLD(v any) template.JS {
	data, err := json.Marshal(v)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(data)
}

// sitemap lista todas las páginas en todos los idiomas, con sus alternativas
// y la fecha de publicación más reciente (FR-026).
func sitemap(m *Model) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	entry := func(rel, lastmod string) {
		for _, lang := range Langs {
			fmt.Fprintf(&b, "  <url>\n    <loc>%s</loc>\n", xmlEscape(m.URL+langRoot(lang)+rel))
			if lastmod != "" {
				fmt.Fprintf(&b, "    <lastmod>%s</lastmod>\n", lastmod)
			}
			for _, alt := range Langs {
				fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"%s\" href=\"%s\"/>\n", alt, xmlEscape(m.URL+langRoot(alt)+rel))
			}
			fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"x-default\" href=\"%s\"/>\n", xmlEscape(m.URL+langRoot(Langs[0])+rel))
			b.WriteString("  </url>\n")
		}
	}
	entry("", m.Updated)
	for _, p := range m.Packages {
		entry("packages/"+p.Name+"/", p.Versions[0].Published)
	}
	b.WriteString("</urlset>\n")
	return b.Bytes()
}

// robots permite todo el sitio, deja fuera del índice el repositorio APT y
// anuncia el sitemap.
func robots(m *Model) []byte {
	return []byte("User-agent: *\nDisallow: /dists/\nDisallow: /pool/\nAllow: /\n\nSitemap: " + m.URL + "/sitemap.xml\n")
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
