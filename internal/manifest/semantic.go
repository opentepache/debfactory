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
		if m.Level2() {
			c.sourceURL()
		} else {
			c.urls()
		}
	}
	c.paths()
	c.summary()
	c.excludes()
	c.phase009()
	c.licenseExceptions()
	c.levelFields()
	if m.Level2() {
		c.level2()
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
	if m.Level2() {
		fs = append(fs, field{"source.url", m.Source.URL, []string{"version", "tag"}})
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

// sourceURL exige que source.url sea un asset de un release o el tarball del
// tag de https://github.com/<upstream>/ (constitución III, nivel 2).
func (c *checker) sourceURL() {
	url, err := c.m.RenderURL(c.m.Source.URL, "")
	if err != nil {
		return // ya informado en templates()
	}
	tag, _ := c.m.RenderedTag()
	base := "https://github.com/" + c.m.Upstream + "/"
	if asset, ok := strings.CutPrefix(url, base+"releases/download/"+tag+"/"); ok && safeSegment(asset) {
		return
	}
	if url == base+"archive/refs/tags/"+tag+".tar.gz" && c.m.Source.Format == "tar.gz" {
		return
	}
	// El dominio de releases propio de upstream (el de homepage) solo vale con
	// la firma de upstream fijada (specs/009, data-model §1).
	if h := homepageHost(c.m.Homepage); h != "" && h != "github.com" && (hostOf(url) == h || strings.HasSuffix(hostOf(url), "."+h)) {
		if c.m.Signing == nil {
			c.add("source.url", "fuera de GitHub: solo se admite con signing fijado")
		}
		return
	}
	c.add("source.url", "fuera de los releases y tags de %s", c.m.Upstream)
}

// homepageHost es el dominio de homepage sin "www.".
func homepageHost(u string) string {
	return strings.TrimPrefix(hostOf(u), "www.")
}

// hostOf extrae el host de una URL https.
func hostOf(u string) string {
	rest, ok := strings.CutPrefix(u, "https://")
	if !ok {
		return ""
	}
	h, _, _ := strings.Cut(rest, "/")
	return strings.ToLower(h)
}

// phase009 son las reglas de la fase 009 (specs/009-hosted-hardened-builds,
// data-model §1): arquitecturas excluidas, firma de upstream, toolchain,
// dependencias y archivos extra del tarball.
func (c *checker) phase009() {
	m := c.m
	if len(m.Exclude.Arches) > 0 {
		for a := range m.Exclude.Arches {
			if !slices.Contains(c.cfg.Arches, a) {
				c.add("exclude.arches."+a, "arquitectura no soportada")
			}
		}
		if len(m.Arches(c.cfg.Arches)) == 0 {
			c.add("exclude.arches", "debe quedar al menos una arquitectura")
		}
	}
	if m.Signing != nil {
		c.signingURL("signing", m.Signing, m.Upstream)
	}
	if t := m.Toolchain; t != nil {
		for _, arch := range c.cfg.Arches {
			if _, err := t.RenderURL(t.URL, arch); err != nil {
				c.add("toolchain.url", "%v", err)
				break
			}
		}
		if t.Signing != nil && hostOf(t.Signing.URL()) != hostOf(t.URL) {
			c.add("toolchain.signing", "la firma debe venir del mismo sitio que el toolchain (%s)", hostOf(t.URL))
		}
		if t.Signing != nil && t.Signing.GPG != nil && hasDotDot(t.Signing.GPG.KeyFile) {
			c.add("toolchain.signing.gpg.key_file", "ruta inválida")
		}
	}
	if m.Deps != nil {
		for i, p := range m.Deps.ExcludePaths {
			if path.IsAbs(p) || hasDotDot(p) {
				c.add(fmt.Sprintf("deps.exclude_paths[%d]", i), "ruta inválida %q", p)
			}
		}
	}
	seen := map[string]bool{}
	for i, f := range m.Source.ExtraFiles {
		at := fmt.Sprintf("source.extra_files[%d].path", i)
		switch {
		case path.IsAbs(f.Path) || hasDotDot(f.Path) || strings.Contains(f.Path, "\\"):
			c.add(at, "ruta inválida %q (relativa al árbol del fuente, sin \"..\")", f.Path)
		case IsGlob(f.Path):
			c.add(at, "sin comodines: cada archivo por su ruta exacta")
		case seen[f.Path]:
			c.add(at, "%s repetido", f.Path)
		}
		seen[f.Path] = true
	}
}

// signingURL exige que la firma venga del mismo origen que lo firmado: los
// releases del tag en GitHub o, en nivel 2, el sitio de releases de upstream.
func (c *checker) signingURL(field string, s *Signing, upstream string) {
	tmpl := s.URL()
	vars := Vars{"version": c.m.Version, "arch": "x"}
	if tag, err := c.m.RenderedTag(); err == nil {
		vars["tag"] = tag
	}
	url, err := Render(tmpl, vars)
	if err != nil {
		c.add(field, "%v", err)
		return
	}
	prefix := "https://github.com/" + upstream + "/releases/download/" + vars["tag"] + "/"
	if strings.HasPrefix(url, prefix) {
		return
	}
	if c.m.Level2() && hostOf(url) == hostOf(mustRender(c.m, c.m.Source.URL)) {
		return
	}
	c.add(field, "la firma debe venir del release de %s o del mismo sitio que el fuente", upstream)
	if s.GPG != nil && hasDotDot(s.GPG.KeyFile) {
		c.add(field+".gpg.key_file", "ruta inválida")
	}
}

func mustRender(m *Manifest, tmpl string) string {
	u, _ := m.RenderURL(tmpl, "")
	return u
}

// levelFields rechaza los campos del otro nivel: en nivel 2 la carpeta
// debian/ sustituye a artifact, install y compañía; en nivel 1 no hay código
// fuente que compilar (specs/008-source-builds, data-model §1).
func (c *checker) levelFields() {
	m := c.m
	type f struct {
		name string
		set  bool
	}
	var fs []f
	if m.Level2() {
		fs = []f{{"arch_map", len(m.ArchMap) > 0}, {"artifact", m.Artifact != (Artifact{})},
			{"checksums", m.Checksums != nil}, {"install", len(m.Install) > 0},
			{"completions", len(m.Completions.List()) > 0}, {"man", len(m.Man) > 0}}
		for _, x := range fs {
			if x.set {
				c.add(x.name, "no se admite en nivel 2 (lo describe la carpeta debian/)")
			}
		}
		return
	}
	fs = []f{{"justification", m.Justification != ""},
		{"source", m.Source.URL != "" || m.Source.Format != "" || len(m.Source.ExtraFiles) > 0},
		{"build", m.BuildOpts != (BuildOpts{})}, {"toolchain", m.Toolchain != nil}, {"deps", m.Deps != nil}}
	for _, x := range fs {
		if x.set {
			c.add(x.name, "solo se admite en nivel 2")
		}
	}
}

// level2 son las reglas del nivel 2 que dependen de la configuración
// (specs/008-source-builds, data-model §1).
func (c *checker) level2() {
	if c.m.License.File.Archive != "" && c.m.Source.Format != "tar.gz" {
		c.add("license.file.archive", "con source.format %s usa license.file.repo (solo se lee dentro de tar.gz)", c.m.Source.Format)
	}
	if c.m.BuildOpts.Tmpfs == "" {
		return
	}
	got, err := config.ParseSize(c.m.BuildOpts.Tmpfs)
	if err != nil {
		c.add("build.tmpfs", "%v", err)
		return
	}
	max, _ := config.ParseSize(c.cfg.Level2.TmpfsMaxOrDefault())
	if got > max {
		c.add("build.tmpfs", "%s supera level2.tmpfs_max (%s)", c.m.BuildOpts.Tmpfs, c.cfg.Level2.TmpfsMaxOrDefault())
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
	if m.Level2() {
		if err := CheckDebianDir(root, m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// licenseExceptions valida las excepciones de licencia del paquete
// (constitución IV, 1.12.0): cada licencia en allowed o exception_only, una
// sola excepción por dependencia, rutas relativas sin "..", y las de
// dependencias solo en paquetes con deps.
func (c *checker) licenseExceptions() {
	m := c.m
	seen := map[string]bool{}
	for i, x := range m.LicenseExceptions {
		at := fmt.Sprintf("license_exceptions[%d]", i)
		if seen[x.Dependency] {
			c.add(at+".dependency", "hay más de una excepción para %q", x.Dependency)
		}
		seen[x.Dependency] = true
		if x.Dependency == "" && (x.File != "" || x.RepoFile != "") {
			c.add(at, "una excepción del propio paquete solo lleva licenses y reason")
		}
		if x.Dependency != "" && m.Deps == nil {
			c.add(at+".dependency", "el paquete no declara deps")
		}
		if m.Deps != nil {
			if _, skipped := m.Deps.Skip[x.Dependency]; skipped && x.Dependency != "" {
				c.add(at+".dependency", "%q está en deps.skip: no va en el fuente", x.Dependency)
			}
		}
		if x.File != "" && x.RepoFile != "" {
			c.add(at, "file y repo_file son excluyentes")
		}
		for _, f := range []struct{ name, v string }{{"file", x.File}, {"repo_file", x.RepoFile}} {
			if f.v != "" && (path.IsAbs(f.v) || slices.Contains(strings.Split(f.v, "/"), "..")) {
				c.add(at+"."+f.name, "ruta relativa sin \"..\": %q", f.v)
			}
		}
		for _, id := range x.Licenses {
			if !c.cfg.LicenseExceptable(id) {
				c.add(at+".licenses", "%s no está en allowed ni en exception_only de %s", id, config.LicensesFile)
			}
		}
	}
}
