package site

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"debfactory/internal/manifest"
)

// MaxPageBytes es el presupuesto de cada página con su CSS (FR-022).
const MaxPageBytes = 100 * 1024

// ModelFile es el modelo publicado junto a las páginas (research D1).
const ModelFile = "site.json"

// reserved son las rutas del repositorio APT que el sitio nunca pisa
// (FR-023): directorios por prefijo y archivos de la raíz por nombre.
var (
	reservedDirs  = []string{"dists/", "pool/"}
	reservedFiles = []string{KeyringFile}
)

// page es lo que recibe cada plantilla.
type page struct {
	M      *Model
	P      *Package  // nil en la portada
	V      *Version  // página de procedencia
	Prov   *provView // página de procedencia
	Lang   string
	T      Strings
	Home   string // ruta de la portada en este idioma: "/" o "/es/"
	Self   string // ruta de esta página
	Alt    []alt  // la misma página en cada idioma
	CSS    string // ruta de la hoja de estilos
	JS     string // ruta del script opcional (FR-018)
	OG     string // URL absoluta de la imagen Open Graph (FR-026)
	Title  string // <title> (FR-025)
	Desc   string // meta descripción (FR-025)
	JSONLD template.JS
}

type alt struct {
	Lang, Name, Href string
	Current          bool
}

// Localized es un texto con el idioma en que está, si difiere del de la
// página (un paquete sin traducción muestra el inglés con lang="en").
type Localized struct {
	Text string
	Lang string
}

// LocalizedParas es una descripción en párrafos con su idioma.
type LocalizedParas struct {
	Paras []string
	Lang  string
}

// langRoot es la ruta de un idioma: el primero en la raíz, los demás bajo
// /<código>/.
func langRoot(lang string) string {
	if lang == Langs[0] {
		return "/"
	}
	return "/" + lang + "/"
}

// hashed es la ruta de un recurso con el hash de su contenido: inmutable
// (research D6).
func hashed(name, ext string, data []byte) string {
	sum := sha256.Sum256(data)
	return "assets/" + name + "." + hex.EncodeToString(sum[:])[:12] + ext
}

// Build genera el árbol completo del sitio desde m en out, que no debe
// existir o estar vacío (data-model §2). Escribe en un directorio temporal
// junto a out y lo renombra al terminar: nunca queda un árbol a medias. Con
// el mismo modelo produce los mismos bytes (FR-014).
func Build(m *Model, out string) (err error) {
	if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		return siteErr("%s no está vacío", out)
	}
	strs, err := LoadI18n()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return siteErr("%v", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(out), "."+filepath.Base(out)+".tmp-")
	if err != nil {
		return siteErr("%v", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmp)
		}
	}()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return siteErr("%v", err)
	}

	cssPath, jsPath, ogPath := hashed("site", ".css", styleCSS), hashed("site", ".js", scriptJS), hashed("og", ".png", ogPNG)
	files := map[string][]byte{cssPath: styleCSS, jsPath: scriptJS, ogPath: ogPNG}

	index, err := parse("index.html")
	if err != nil {
		return err
	}
	pkgTmpl, err := parse("package.html")
	if err != nil {
		return err
	}
	provTmpl, err := parse("provenance.html")
	if err != nil {
		return err
	}
	for _, lang := range Langs {
		t := strs[lang]
		base := page{M: m, Lang: lang, T: t, Home: langRoot(lang), CSS: "/" + cssPath, JS: "/" + jsPath, OG: m.URL + "/" + ogPath}
		home := base
		home.Self, home.Alt = langRoot(lang), alts(strs, lang, "")
		home.Title, home.Desc, home.JSONLD = homeSEO(m, t, lang)
		if err := render(files, index, home); err != nil {
			return err
		}
		for i := range m.Packages {
			p := &m.Packages[i]
			if slices.Contains(manifest.ReservedNames, p.Name) || strings.ContainsAny(p.Name, "/\\") || p.Name == "" {
				return siteErr("nombre de paquete %q no válido para una ruta del sitio", p.Name)
			}
			pg := base
			rel := "packages/" + p.Name + "/"
			pg.P, pg.Self, pg.Alt = p, langRoot(lang)+rel, alts(strs, lang, rel)
			pg.Title, pg.Desc, pg.JSONLD = packageSEO(m, p, t, lang)
			if err := render(files, pkgTmpl, pg); err != nil {
				return err
			}
			// Procedencia de cada versión (specs/009, T036).
			for j := range p.Versions {
				v := &p.Versions[j]
				if len(v.ProvenanceFile) == 0 {
					continue
				}
				var pv provView
				if err := json.Unmarshal(v.ProvenanceFile, &pv); err != nil {
					return siteErr("provenance.json de %s %s: %v", p.Name, v.Version, err)
				}
				vrel := rel + v.Version + "/"
				pp := pg
				pp.V, pp.Prov, pp.Self, pp.Alt = v, &pv, langRoot(lang)+vrel, alts(strs, lang, vrel)
				pp.Title = fmt.Sprintf(t["prov_title"], p.Name, v.Version)
				pp.Desc, pp.JSONLD = t["prov_intro"], ""
				if err := render(files, provTmpl, pp); err != nil {
					return err
				}
				if lang == Langs[0] {
					files[vrel+"provenance.json"] = v.ProvenanceFile // byte a byte (su sha256 está en la auditoría)
				}
			}
		}
	}
	files["sitemap.xml"] = sitemap(m)
	files["robots.txt"] = robots(m)
	js, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return siteErr("%v", err)
	}
	files[ModelFile] = append(js, '\n')

	assets := len(styleCSS) + len(scriptJS)
	for name, data := range files {
		if err := checkPath(name); err != nil {
			return err
		}
		if strings.HasSuffix(name, ".html") && len(data)+assets > MaxPageBytes {
			return siteErr("%s pesa %d bytes con su CSS y su script; el máximo es %d (FR-022)", name, len(data)+assets, MaxPageBytes)
		}
		dst := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return siteErr("%v", err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return siteErr("%v", err)
		}
	}
	if err := os.Remove(out); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return siteErr("%v", err)
	}
	if err := os.Rename(tmp, out); err != nil {
		return siteErr("%v", err)
	}
	return nil
}

// checkPath rechaza las rutas que pisarían el repositorio APT (FR-023).
func checkPath(name string) error {
	clean := path.Clean(name)
	if clean != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") {
		return siteErr("ruta del sitio no válida: %q", name)
	}
	for _, d := range reservedDirs {
		if strings.HasPrefix(name, d) {
			return siteErr("la ruta %s pisaría el repositorio APT (%s)", name, d)
		}
	}
	if slices.Contains(reservedFiles, name) {
		return siteErr("la ruta %s pisaría el repositorio APT", name)
	}
	return nil
}

// alts devuelve la página rel (relativa a la raíz de cada idioma) en todos
// los idiomas.
func alts(strs map[string]Strings, cur, rel string) []alt {
	var out []alt
	for _, l := range Langs {
		out = append(out, alt{Lang: l, Name: strs[l]["lang_name"], Href: langRoot(l) + rel, Current: l == cur})
	}
	return out
}

func parse(name string) (*template.Template, error) {
	t, err := template.New(name).Option("missingkey=error").Funcs(funcs).
		ParseFS(templatesFS, "templates/base.html", "templates/"+name)
	if err != nil {
		return nil, siteErr("plantilla %s: %v", name, err)
	}
	return t, nil
}

var funcs = template.FuncMap{
	// t devuelve un texto fijo; una clave inexistente es un error de render.
	"t": func(s Strings, key string) (string, error) {
		v, ok := s[key]
		if !ok {
			return "", fmt.Errorf("falta la traducción %q", key)
		}
		return v, nil
	},
	"summary": func(p *Package, lang string) Localized {
		switch {
		case lang == "en":
			return Localized{Text: p.Summary.EN}
		case lang == "es" && p.Summary.ES != nil:
			return Localized{Text: *p.Summary.ES}
		}
		return Localized{Text: p.Summary.EN, Lang: "en"}
	},
	"description": func(p *Package, lang string) LocalizedParas {
		if lang == "es" && len(p.Description.ES) > 0 {
			return LocalizedParas{Paras: p.Description.ES}
		}
		if lang == "en" {
			return LocalizedParas{Paras: p.Description.EN}
		}
		return LocalizedParas{Paras: p.Description.EN, Lang: "en"}
	},
	"suitesOf": func(m *Model, distro string) []string {
		var out []string
		for _, s := range m.Repo.Suites {
			if s.Distro == distro {
				out = append(out, s.Name)
			}
		}
		return out
	},
	"has":   func(list []string, v string) bool { return slices.Contains(list, v) },
	"join":  strings.Join,
	"upper": strings.ToUpper,
	// suiteLabel es el nombre legible de una suite ("Debian 12").
	"suiteLabel": func(m *Model, name string) string {
		for _, s := range m.Repo.Suites {
			if s.Name == name {
				return s.Label
			}
		}
		return name
	},
	// tf es t con argumentos: el texto fijo es un formato de fmt (%s).
	"tf": func(s Strings, key string, args ...any) (string, error) {
		v, ok := s[key]
		if !ok {
			return "", fmt.Errorf("falta la traducción %q", key)
		}
		return fmt.Sprintf(v, args...), nil
	},
	"older": func(p *Package) []Version {
		if len(p.Versions) < 2 {
			return nil
		}
		return p.Versions[1:]
	},
}

func render(files map[string][]byte, t *template.Template, pg page) error {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", pg); err != nil {
		return siteErr("render de %s: %v", pg.Self, err)
	}
	name := strings.TrimPrefix(pg.Self, "/") + "index.html"
	files[name] = buf.Bytes()
	return nil
}

// Digest es el SHA-256 de un árbol generado: rutas y contenido, en orden
// (data-model §3). Nombra <data>/site/<digest>/ y decide si hay algo que
// subir.
func Digest(dir string) (string, error) {
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.Sort(names)
	h := sha256.New()
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(n)))
		if err != nil {
			return "", err
		}
		fs := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%x\n", n, fs)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// BuildDigest genera el sitio de m en parent/<digest>/ (research D5): lo
// construye en un temporal, calcula su digest y lo renombra. Si ya existe un
// árbol con ese digest, lo reutiliza. Conserva solo el resultante y el más
// reciente de los anteriores. Devuelve el directorio.
func BuildDigest(m *Model, parent string) (string, error) {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", siteErr("%v", err)
	}
	tmp, err := os.MkdirTemp(parent, ".build-")
	if err != nil {
		return "", siteErr("%v", err)
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "site")
	if err := Build(m, out); err != nil {
		return "", err
	}
	digest, err := Digest(out)
	if err != nil {
		return "", siteErr("%v", err)
	}
	dir := filepath.Join(parent, digest)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := os.Rename(out, dir); err != nil {
			return "", siteErr("%v", err)
		}
	}
	now := time.Now()
	os.Chtimes(dir, now, now)
	prune(parent, digest)
	return dir, nil
}

// prune borra los árboles de parent salvo keep y el más reciente de los demás.
func prune(parent, keep string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	type gen struct {
		name string
		mod  time.Time
	}
	var others []gen
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			others = append(others, gen{e.Name(), info.ModTime()})
		}
	}
	slices.SortFunc(others, func(a, b gen) int { return b.mod.Compare(a.mod) })
	for i, g := range others {
		if i > 0 {
			os.RemoveAll(filepath.Join(parent, g.name))
		}
	}
}

// Files lista los archivos de un árbol generado, relativos y en orden.
func Files(dir string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	slices.Sort(names)
	return names, err
}

// provView es lo que la página de procedencia lee de provenance.json
// (data-model §8); el archivo se publica tal cual.
type provView struct {
	Upstream struct {
		Files map[string]string `json:"files"`
	} `json:"upstream"`
	ManifestCommit string `json:"manifest_commit"`
	RunURL         string `json:"run_url"`
	Attestations   []struct {
		Subject  string `json:"subject"`
		Digest   string `json:"digest"`
		RekorURL string `json:"rekor_url"`
	} `json:"attestations"`
	Tests []struct {
		Suite  string `json:"suite"`
		Arch   string `json:"arch"`
		Status string `json:"status"`
	} `json:"tests"`
	AuditSeq     int `json:"audit_seq"`
	VerifiedByPC struct {
		Mode          string `json:"mode"`
		Attestations  bool   `json:"attestations"`
		Lintian       bool   `json:"lintian"`
		RebuildLevel1 bool   `json:"rebuild_level1"`
		TwoVariants   bool   `json:"two_variants"`
		SmokeAMD64    bool   `json:"smoke_amd64"`
	} `json:"verified_by_pc"`
	Supply []struct {
		Arch      string `json:"arch"`
		Signature struct {
			Type string `json:"type"`
			OK   bool   `json:"ok"`
		} `json:"signature"`
		Provenance struct {
			Present bool `json:"present"`
			OK      bool `json:"ok"`
		} `json:"provenance"`
		TarballVsTag *struct {
			Extra []string `json:"extra"`
		} `json:"tarball_vs_tag"`
		Toolchain *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"toolchain"`
		Deps *struct {
			Count int `json:"count"`
		} `json:"deps"`
	} `json:"supply"`
}
