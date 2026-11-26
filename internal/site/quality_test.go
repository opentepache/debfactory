package site

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestQuality revisa la estructura de cada página generada (research D10):
// idioma, un solo h1 y encabezados sin saltos, alt en imágenes, solo el script
// propio y el JSON-LD, ningún manejador on* (FR-018), recursos solo del propio
// sitio (FR-021) y el presupuesto de peso con CSS y script (FR-022).
func TestQuality(t *testing.T) {
	m := newFixture(t).load(t)
	out := build(t, m)
	files, _ := Files(out)
	css := 0
	for _, n := range files {
		if strings.HasPrefix(n, "assets/") && !strings.HasSuffix(n, ".png") { // la imagen OG no la carga la página
			css += len(read(t, out, n))
		}
	}
	pages := 0
	for _, n := range files {
		if !strings.HasSuffix(n, ".html") {
			continue
		}
		pages++
		data, _ := os.ReadFile(filepath.Join(out, n))
		if len(data)+css > MaxPageBytes {
			t.Errorf("%s: %d bytes con CSS", n, len(data)+css)
		}
		doc, err := html.Parse(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		checkPage(t, n, doc, m.URL)
	}
	if pages != 2*(1+len(m.Packages)) {
		t.Errorf("%d páginas", pages)
	}
}

func checkPage(t *testing.T, name string, doc *html.Node, siteURL string) {
	t.Helper()
	h1, last, scripts, ld := 0, 0, 0, 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attr := map[string]string{}
			for _, a := range n.Attr {
				attr[a.Key] = a.Val
				if strings.HasPrefix(a.Key, "on") {
					t.Errorf("%s: atributo %s", name, a.Key)
				}
			}
			switch n.Data {
			case "html":
				if attr["lang"] == "" {
					t.Errorf("%s: <html> sin lang", name)
				}
			case "script":
				switch {
				case attr["type"] == "application/ld+json":
					ld++
				case attr["src"] != "" && n.FirstChild == nil:
					scripts++
					checkResource(t, name, attr["src"], siteURL)
				default:
					t.Errorf("%s: script en línea no permitido", name)
				}
			case "iframe", "object", "embed":
				t.Errorf("%s: <%s>", name, n.Data)
			case "img":
				if _, ok := attr["alt"]; !ok {
					t.Errorf("%s: <img> sin alt", name)
				}
				checkResource(t, name, attr["src"], siteURL)
			case "link":
				if attr["rel"] == "alternate" {
					if !strings.HasPrefix(attr["href"], siteURL+"/") {
						t.Errorf("%s: alternate %q", name, attr["href"])
					}
				} else {
					checkResource(t, name, attr["href"], siteURL)
				}
			case "h1", "h2", "h3", "h4", "h5", "h6":
				level := int(n.Data[1] - '0')
				if level == 1 {
					h1++
				}
				if level > last+1 {
					t.Errorf("%s: salto de encabezado h%d → h%d", name, last, level)
				}
				last = level
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if scripts != 1 || ld != 1 {
		t.Errorf("%s: %d scripts propios y %d JSON-LD; se esperaba uno de cada", name, scripts, ld)
	}
	if h1 != 1 {
		t.Errorf("%s: %d h1", name, h1)
	}
}

// checkResource: lo que la página carga sale del propio sitio (FR-021).
func checkResource(t *testing.T, name, ref, siteURL string) {
	t.Helper()
	if !strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "//") {
		if !strings.HasPrefix(ref, siteURL+"/") {
			t.Errorf("%s: recurso externo %q", name, ref)
		}
	}
}

// TestContrast: los pares de color del CSS cumplen AA (4.5:1) en claro y en
// oscuro (FR-019).
func TestContrast(t *testing.T) {
	css := string(styleCSS)
	light := vars(css[:strings.Index(css, "@media (prefers-color-scheme: dark)")])
	darkBlock := css[strings.Index(css, "@media (prefers-color-scheme: dark)"):]
	dark := vars(darkBlock[:strings.Index(darkBlock, "\n}\n")])
	pairs := [][2]string{
		{"--text", "--bg"}, {"--dim", "--bg"}, {"--link", "--bg"}, {"--ok", "--bg"},
		{"--text", "--surface"}, {"--dim", "--surface"}, {"--link", "--surface"},
		{"--code", "--code-bg"}, {"--dim", "--code-bg"}, {"--text", "--code-bg"}, {"--on-accent", "--accent"},
	}
	for mode, v := range map[string]map[string]string{"claro": light, "oscuro": dark} {
		for _, p := range pairs {
			fg, bg := v[p[0]], v[p[1]]
			if fg == "" || bg == "" {
				t.Fatalf("%s: falta %s o %s", mode, p[0], p[1])
			}
			if r := contrast(fg, bg); r < 4.5 {
				t.Errorf("%s: %s %s sobre %s %s = %.2f:1", mode, p[0], fg, p[1], bg, r)
			}
		}
		// El foco debe distinguirse del fondo (WCAG 1.4.11: 3:1).
		if r := contrast(v["--focus"], v["--bg"]); r < 3 {
			t.Errorf("%s: foco %.2f:1", mode, r)
		}
	}
}

var varRe = regexp.MustCompile(`(--[a-z-]+):\s*(#[0-9a-fA-F]{6})`)

func vars(block string) map[string]string {
	out := map[string]string{}
	for _, m := range varRe.FindAllStringSubmatch(block, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(hex string) float64 {
	var c [3]float64
	for i := range c {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}
