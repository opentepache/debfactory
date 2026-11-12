// Package pkg convierte el BuildPlan en un .deb con nfpm y genera los archivos
// de metadatos (copyright DEP-5, changelog y overrides de lintian).
package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/assemble"
	"debfactory/internal/manifest"
)

// commonLicenses son las licencias de la allowlist cuyo texto instala
// base-files en /usr/share/common-licenses. La política de Debian (§12.5)
// pide remitir a ese archivo en lugar de copiar el texto, y lintian 2.122 da
// error (copyright-file-contains-full-gpl-license, no silenciable) si se copia.
var commonLicenses = map[string]struct{ file, name string }{
	"Apache-2.0":        {"Apache-2.0", "Apache License, Version 2.0"},
	"GPL-2.0-only":      {"GPL-2", "GNU General Public License version 2"},
	"GPL-2.0-or-later":  {"GPL-2", "GNU General Public License version 2"},
	"GPL-3.0-only":      {"GPL-3", "GNU General Public License version 3"},
	"GPL-3.0-or-later":  {"GPL-3", "GNU General Public License version 3"},
	"LGPL-2.1-only":     {"LGPL-2.1", "GNU Lesser General Public License version 2.1"},
	"LGPL-2.1-or-later": {"LGPL-2.1", "GNU Lesser General Public License version 2.1"},
	"LGPL-3.0-only":     {"LGPL-3", "GNU Lesser General Public License version 3"},
	"LGPL-3.0-or-later": {"LGPL-3", "GNU Lesser General Public License version 3"},
	"MPL-2.0":           {"MPL-2.0", "Mozilla Public License, version 2.0"},
}

// Copyright genera /usr/share/doc/<name>/copyright en formato DEP-5. Las
// licencias de commonLicenses remiten a /usr/share/common-licenses; las demás
// (MIT, BSD, ISC…) llevan el texto completo de la versión exacta.
func Copyright(m *manifest.Manifest, text []byte) []byte {
	var b strings.Builder
	b.WriteString("Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\n")
	fmt.Fprintf(&b, "Upstream-Name: %s\n", m.Name)
	fmt.Fprintf(&b, "Source: %s\n\n", m.Homepage)
	b.WriteString("Files: *\n")
	fmt.Fprintf(&b, "Copyright: %s\n", copyrightHolder(m, string(text)))
	fmt.Fprintf(&b, "License: %s\n", m.License.SPDX)
	if c, ok := commonLicenses[m.License.SPDX]; ok {
		fmt.Fprintf(&b, " On Debian systems, the complete text of the %s\n can be found in \"/usr/share/common-licenses/%s\".\n",
			c.name, c.file)
		return []byte(b.String())
	}
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			b.WriteString(" .\n")
		} else {
			b.WriteString(" " + l + "\n")
		}
	}
	return []byte(b.String())
}

// copyrightHolder toma la primera línea "Copyright …" del texto que no sea
// de la propia licencia (FSF) ni una plantilla ("<year>", "[yyyy]"); si no
// hay, license.copyright; si tampoco, el dueño del repositorio upstream.
func copyrightHolder(m *manifest.Manifest, text string) string {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "Copyright") || strings.Contains(l, "Free Software Foundation") ||
			strings.ContainsAny(l, "<>[]") {
			continue
		}
		if rest := strings.TrimSpace(strings.TrimPrefix(l, "Copyright")); rest != "" && !strings.HasPrefix(rest, "License") {
			return l
		}
	}
	if m.License.Copyright != "" {
		return m.License.Copyright
	}
	owner, _, _ := strings.Cut(m.Upstream, "/")
	return owner
}

// Changelog genera changelog.Debian.gz: una entrada con la fecha de
// SOURCE_DATE_EPOCH, comprimida como `gzip -9n` (sin nombre ni fecha).
func Changelog(m *manifest.Manifest, maintainer string, epoch int64) ([]byte, error) {
	date := time.Unix(epoch, 0).UTC().Format(time.RFC1123Z)
	text := fmt.Sprintf("%s (%s) unstable; urgency=medium\n\n"+
		"  * Repackage upstream release %s with debfactory.\n\n"+
		" -- %s  %s\n", m.Name, m.DebVersion(), m.Version, maintainer, date)
	return assemble.Gzip([]byte(text))
}

// OverrideLines devuelve las líneas de override sin el prefijo del paquete,
// p. ej. "statically-linked-binary [usr/bin/fnm]" (sin la "/" inicial: con
// ella lintian 2.122 da mismatched-override).
func OverrideLines(m *manifest.Manifest) []string {
	var out []string
	for _, o := range m.LintianOverrides {
		line := o.Tag
		if o.Path != "" {
			line += " [" + strings.TrimPrefix(o.Path, "/") + "]"
		}
		out = append(out, line)
	}
	return out
}

// LintianOverrides genera /usr/share/lintian/overrides/<name>, o nil si el
// manifiesto no declara overrides.
func LintianOverrides(m *manifest.Manifest) []byte {
	lines := OverrideLines(m)
	if len(lines) == 0 {
		return nil
	}
	var b strings.Builder
	for i, o := range m.LintianOverrides {
		fmt.Fprintf(&b, "# %s\n%s: %s\n", strings.TrimSpace(o.Reason), m.Name, lines[i])
	}
	return []byte(b.String())
}

// MetaFiles escribe en dir el copyright, el changelog y los overrides, y
// devuelve sus entradas del BuildPlan (modo 0644).
func MetaFiles(m *manifest.Manifest, maintainer string, license []byte, epoch int64, dir string) ([]assemble.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	changelog, err := Changelog(m, maintainer, epoch)
	if err != nil {
		return nil, err
	}
	docs := "/usr/share/doc/" + m.Name + "/"
	items := []struct {
		name, dst string
		data      []byte
	}{
		{"copyright", docs + "copyright", Copyright(m, license)},
		{"changelog.Debian.gz", docs + "changelog.Debian.gz", changelog},
		{"lintian-overrides", "/usr/share/lintian/overrides/" + m.Name, LintianOverrides(m)},
	}
	var files []assemble.File
	for _, it := range items {
		if it.data == nil {
			continue
		}
		p := filepath.Join(dir, it.name)
		if err := os.WriteFile(p, it.data, 0o644); err != nil {
			return nil, err
		}
		files = append(files, assemble.File{Dst: it.dst, Src: p, Mode: 0o644})
	}
	return files, nil
}
