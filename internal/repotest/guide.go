// Package repotest ejecuta las pruebas E2E del repositorio APT: sirve un
// árbol publicado con Caddy en contenedor y ejecuta los bloques de
// docs/usuario.md en contenedores limpios de cada suite
// (specs/002-signed-apt-repo, FR-032).
package repotest

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// GuideFile es la guía de usuario, relativa a la raíz del repositorio.
const GuideFile = "docs/usuario.md"

// PlaceholderURL es la URL de la guía mientras repo.url esté vacía.
const PlaceholderURL = "https://apt.example.org"

var fence = regexp.MustCompile("^```sh debfactory:([a-z]+)\\s*$")

// Blocks extrae los bloques ejecutables (```sh debfactory:<paso>) de la
// guía, cada uno como script entero.
func Blocks(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blocks := map[string]string{}
	var step string
	var cur []string
	for _, line := range strings.Split(string(data), "\n") {
		if step == "" {
			if m := fence.FindStringSubmatch(line); m != nil {
				step, cur = m[1], nil
			}
			continue
		}
		if strings.TrimSpace(line) == "```" {
			if _, dup := blocks[step]; dup {
				return nil, fmt.Errorf("%s: bloque %q repetido", path, step)
			}
			blocks[step] = strings.Join(cur, "\n") + "\n"
			step = ""
			continue
		}
		cur = append(cur, line)
	}
	if step != "" {
		return nil, fmt.Errorf("%s: bloque %q sin cerrar", path, step)
	}
	return blocks, nil
}

// SiteBlocks extrae los bloques ejecutables de una página del sitio web
// (specs/006-repo-website, research D8): el texto de cada
// <code data-debfactory-step="<paso>">, sin escapes de HTML, tal como lo
// copia el visitante.
func SiteBlocks(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc, err := html.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	blocks := map[string]string{}
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "code" {
			for _, a := range n.Attr {
				if a.Key != "data-debfactory-step" {
					continue
				}
				if _, dup := blocks[a.Val]; dup {
					return fmt.Errorf("%s: bloque %q repetido", path, a.Val)
				}
				var b strings.Builder
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.TextNode {
						return fmt.Errorf("%s: el bloque %q tiene marcado dentro", path, a.Val)
					}
					b.WriteString(c.Data)
				}
				blocks[a.Val] = b.String() + "\n"
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(doc); err != nil {
		return nil, err
	}
	return blocks, nil
}

// Render prepara un bloque para ejecutarlo como root en un contenedor:
// quita "sudo " al principio de las líneas y tras "&& ", y sustituye la URL pública por
// testURL y los marcadores <suite>, <version>, etc. por vars. Si vars tiene
// "packages", sustituye la lista de paquetes de "apt-get install -y".
func Render(block, publicURL, testURL string, vars map[string]string) string {
	var out []string
	for _, l := range strings.Split(block, "\n") {
		// Los clientes de prueba son root: sin sudo, también en líneas con
		// sangría (bloques if).
		body := strings.TrimLeft(l, " \t")
		indent := l[:len(l)-len(body)]
		out = append(out, indent+strings.ReplaceAll(strings.TrimPrefix(body, "sudo "), "&& sudo ", "&& "))
	}
	s := strings.Join(out, "\n")
	if publicURL == "" {
		publicURL = PlaceholderURL
	}
	s = strings.ReplaceAll(s, strings.TrimSuffix(publicURL, "/"), strings.TrimSuffix(testURL, "/"))
	for k, v := range vars {
		if k == "packages" {
			s = installList.ReplaceAllString(s, "${1} "+v)
			continue
		}
		s = strings.ReplaceAll(s, "<"+k+">", v)
	}
	return s
}

var installList = regexp.MustCompile(`(?m)^(apt-get install -y) [a-z0-9 .+-]+$`)

// CountCommands cuenta los comandos de un bloque; un heredoc cuenta como
// parte del comando que lo abre (FR-031).
func CountCommands(block string) int {
	n := 0
	end := ""
	for _, l := range strings.Split(block, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case end != "":
			if t == end {
				end = ""
			}
		case t == "" || strings.HasPrefix(t, "#"):
		default:
			n++
			if i := strings.Index(t, "<<"); i >= 0 {
				end = strings.Trim(strings.TrimSpace(t[i+2:]), `'"-`)
			}
		}
	}
	return n
}
