package manifest

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/license"
	"debfactory/internal/lock"
	"debfactory/internal/report"
)

// Check aplica las reglas de data-model.md que el schema no expresa (FR-010).
// lk es el lockfile del paquete; puede ser nil si no se quiere comprobar la
// regla de revision.
func Check(m *Manifest, cfg *config.Config, lk *lock.File) error {
	c := &checker{m: m, cfg: cfg}
	c.name()
	tagOK := c.templates()
	if tagOK {
		c.urls()
	}
	c.paths()
	c.summary()
	c.excludes()
	if m.Level != 1 {
		c.add("level", "solo se admite 1 en esta fase")
	}
	if _, err := license.Parse(m.License.SPDX); err != nil {
		c.add("license.spdx", "%q no es SPDX (%v)", m.License.SPDX, err)
	}
	c.overrides()
	if tagOK {
		c.destinations()
	}
	c.commands()
	if m.Artifact.Format == "binary" && len(m.Install) != 1 {
		c.add("install", "con artifact.format binary debe haber exactamente un elemento")
	}
	if lk != nil {
		c.revision(lk)
	}
	return c.errs.Err()
}

type checker struct {
	m    *Manifest
	cfg  *config.Config
	errs report.Errors
}

func (c *checker) add(field, format string, args ...any) {
	c.errs = append(c.errs, validateErr(c.m.Name, c.m.Version, c.m.File, field, fmt.Sprintf(format, args...)))
}

func (c *checker) name() {
	if f := FileName(c.m.File); f != c.m.Name {
		c.add("name", "%q no coincide con el archivo %q", c.m.Name, f)
	}
	if slices.Contains(ReservedNames, c.m.Name) {
		c.add("name", "%q está reservado por el sitio web", c.m.Name)
	}
}

// field es un campo con plantilla y las variables que admite.
type field struct {
	name, value string
	vars        []string
}

var urlVars = []string{"version", "tag", "arch"}

func (c *checker) fields() []field {
	m := c.m
	fs := []field{
		{"tag", m.Tag, []string{"version"}},
		{"artifact.url", m.Artifact.URL, urlVars},
		{"license.file.archive", m.License.File.Archive, urlVars},
		{"license.file.repo", m.License.File.Repo, urlVars},
		{"test.expect", m.Test.Expect, urlVars},
		{"test.command", m.Test.Command, nil},
	}
	if m.Checksums != nil {
		fs = append(fs, field{"checksums.url", m.Checksums.URL, urlVars})
	}
	srcVars := urlVars
	if m.Artifact.Format == "binary" {
		srcVars = append([]string{"name"}, urlVars...)
	}
	for i, it := range m.Install {
		fs = append(fs,
			field{fmt.Sprintf("install[%d].src", i), it.Src, srcVars},
			field{fmt.Sprintf("install[%d].dst", i), it.Dst, nil})
	}
	for _, sc := range m.Completions.List() {
		fs = append(fs,
			field{"completions." + sc.Shell + ".file", sc.File, urlVars},
			field{"completions." + sc.Shell + ".generate", sc.Generate, nil})
	}
	for i, g := range m.Man {
		fs = append(fs, field{fmt.Sprintf("man[%d]", i), g, urlVars})
	}
	return fs
}

// templates comprueba que solo se usan variables conocidas. Devuelve false si
// el tag no se puede renderizar o no es válido (el resto de comprobaciones de
// URL dependen de él).
func (c *checker) templates() bool {
	tagOK := true
	for _, f := range c.fields() {
		vars := Vars{}
		for _, v := range f.vars {
			vars[v] = "x"
		}
		if _, err := Render(f.value, vars); err != nil {
			c.add(f.name, "%v", err)
			if f.name == "tag" {
				tagOK = false
			}
		}
	}
	if tagOK {
		tag, _ := c.m.RenderedTag()
		if !safeSegment(tag) {
			c.add("tag", "%q no es un tag válido", tag)
			tagOK = false
		}
	}
	return tagOK
}

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// safeSegment: un solo segmento de URL, sin "/" ni "..".
func safeSegment(s string) bool {
	return segment.MatchString(s) && !strings.Contains(s, "..")
}

// urls exige que artifact.url y checksums.url apunten a un asset de
// https://github.com/<upstream>/releases/download/<tag>/ (constitución III).
func (c *checker) urls() {
	type u struct{ name, tmpl string }
	list := []u{{"artifact.url", c.m.Artifact.URL}}
	if c.m.Checksums != nil {
		list = append(list, u{"checksums.url", c.m.Checksums.URL})
	}
	tag, _ := c.m.RenderedTag()
	prefix := "https://github.com/" + c.m.Upstream + "/releases/download/" + tag + "/"
	for _, x := range list {
		for _, arch := range c.cfg.Arches {
			url, err := c.m.RenderURL(x.tmpl, arch)
			if err != nil {
				break // ya informado en templates()
			}
			asset, ok := strings.CutPrefix(url, prefix)
			if !ok || !safeSegment(asset) {
				c.add(x.name, "fuera de los releases de %s", c.m.Upstream)
				break
			}
		}
	}
}

// paths exige rutas relativas sin ".." dentro del artefacto y destinos limpios.
func (c *checker) paths() {
	rel := func(name, p string) {
		if p == "" {
			return
		}
		if path.IsAbs(p) || strings.Contains(p, "\\") || hasDotDot(p) {
			c.add(name, "ruta inválida %q (debe ser relativa y sin \"..\")", p)
		}
	}
	m := c.m
	rel("license.file.archive", m.License.File.Archive)
	rel("license.file.repo", m.License.File.Repo)
	for i, it := range m.Install {
		rel(fmt.Sprintf("install[%d].src", i), it.Src)
		if hasDotDot(it.Dst) || strings.Contains(it.Dst, "//") {
			c.add(fmt.Sprintf("install[%d].dst", i), "ruta inválida %q", it.Dst)
		}
	}
	for _, sc := range m.Completions.List() {
		rel("completions."+sc.Shell+".file", sc.File)
	}
	for i, g := range m.Man {
		rel(fmt.Sprintf("man[%d]", i), g)
	}
	for i, o := range m.LintianOverrides {
		if hasDotDot(o.Path) {
			c.add(fmt.Sprintf("lintian_overrides[%d].path", i), "ruta inválida %q", o.Path)
		}
	}
}

func hasDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

var article = regexp.MustCompile(`(?i)^(a|an|the)\s`)

func (c *checker) summary() {
	if article.MatchString(c.m.Summary) {
		c.add("summary", "no debe empezar con artículo")
	}
}

func (c *checker) excludes() {
	keys := make([]string, 0, len(c.m.Exclude.Suites))
	for k := range c.m.Exclude.Suites {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, s := range keys {
		if _, ok := c.cfg.Suite(s); !ok {
			c.add("exclude.suites."+s, "suite no soportada")
		}
	}
}

func (c *checker) overrides() {
	for i, o := range c.m.LintianOverrides {
		if !c.cfg.Lintian.Overridable(o.Tag) {
			c.add(fmt.Sprintf("lintian_overrides[%d].tag", i), "%q no se puede silenciar", o.Tag)
		}
	}
}

// destinations exige destinos literales únicos; los que salen de globs se
// comprueban al ensamblar.
func (c *checker) destinations() {
	reported := map[string]bool{}
	for _, arch := range c.cfg.Arches {
		vars, err := c.m.SrcVars(arch)
		if err != nil {
			return
		}
		dsts := map[string]bool{}
		for i, it := range c.m.Install {
			src, err := Render(it.Src, vars)
			if err != nil || IsGlob(src) {
				continue
			}
			dst := InstallDst(it.Dst, src)
			if dsts[dst] && !reported[dst] {
				c.add(fmt.Sprintf("install[%d].dst", i), "%s duplicado", dst)
				reported[dst] = true
			}
			dsts[dst] = true
		}
	}
}

// IsGlob indica si una ruta tiene metacaracteres de glob.
func IsGlob(p string) bool { return strings.ContainsAny(p, "*?[") }

// InstallDst es la ruta final: si dst termina en "/", se conserva el nombre
// de origen.
func InstallDst(dst, src string) string {
	if strings.HasSuffix(dst, "/") {
		return dst + path.Base(src)
	}
	return dst
}

const shellMeta = ";|&$`<>()'\"\\\n"

// commands: test.command y completions.*.generate se ejecutan sin shell.
func (c *checker) commands() {
	check := func(name, cmd string) {
		if strings.ContainsAny(cmd, shellMeta) {
			c.add(name, "no se admiten metacaracteres de shell (la orden se ejecuta sin shell)")
		}
	}
	check("test.command", c.m.Test.Command)
	for _, sc := range c.m.Completions.List() {
		check("completions."+sc.Shell+".generate", sc.Generate)
	}
}

// revision: si la versión es nueva y el lockfile tiene otras, N vuelve a 1
// (constitución VI).
func (c *checker) revision(lk *lock.File) {
	if _, ok := lk.Versions[c.m.Version]; ok || len(lk.Versions) == 0 {
		return
	}
	if c.m.Revision != 1 {
		c.add("revision", "debe ser 1 para una versión upstream nueva")
	}
}

// Validate es la validación completa de `debfactory validate`: schema, reglas
// semánticas y regla de revision con el lockfile del paquete.
func Validate(root string, cfg *config.Config, name string) (*Manifest, error) {
	m, err := Load(root, name)
	if err != nil {
		return nil, err
	}
	lk, err := lock.Read(root, m.Name)
	if err != nil {
		return nil, &report.Error{Package: m.Name, Version: m.Version, Stage: report.Validate, Cause: err.Error()}
	}
	if err := Check(m, cfg, lk); err != nil {
		return nil, err
	}
	return m, nil
}
