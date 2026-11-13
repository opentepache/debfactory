package pkg

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"

	"debfactory/internal/manifest"
)

func fnm() *manifest.Manifest {
	return &manifest.Manifest{
		Name: "fnm", Version: "1.39.0", Revision: 1, Upstream: "Schniz/fnm",
		Homepage: "https://github.com/Schniz/fnm",
		License:  manifest.License{SPDX: "GPL-3.0-only"},
		LintianOverrides: []manifest.LintianOverride{
			{Tag: "statically-linked-binary", Path: "/usr/bin/fnm", Reason: "Static by design."},
			{Tag: "no-manual-page", Reason: "Upstream ships none."},
		},
	}
}

func TestCopyright(t *testing.T) {
	m := fnm()
	m.License.SPDX = "MIT"
	got := string(Copyright(m, []byte("\nCopyright (c) 2026 Ana\n\nLine two  \n\n")))
	want := "Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\n" +
		"Upstream-Name: fnm\nSource: https://github.com/Schniz/fnm\n\n" +
		"Files: *\nCopyright: Copyright (c) 2026 Ana\nLicense: MIT\n Copyright (c) 2026 Ana\n .\n Line two\n"
	if got != want {
		t.Errorf("copyright:\n%s\nse esperaba:\n%s", got, want)
	}
}

// TestCopyrightCommonLicense: GPL y Apache remiten a /usr/share/common-licenses
// (lintian da error si se copia el texto completo).
func TestCopyrightCommonLicense(t *testing.T) {
	m := fnm()
	got := string(Copyright(m, []byte("GNU GENERAL PUBLIC LICENSE\nVersion 3\n")))
	want := "License: GPL-3.0-only\n On Debian systems, the complete text of the GNU General Public License version 3\n" +
		" can be found in \"/usr/share/common-licenses/GPL-3\".\n"
	if !strings.HasSuffix(got, want) || strings.Contains(got, "GNU GENERAL PUBLIC LICENSE") {
		t.Errorf("copyright:\n%s", got)
	}
}

func TestCopyrightHolder(t *testing.T) {
	read := func(f string) string {
		b, err := os.ReadFile("../../testdata/licenses/" + f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	m := fnm()
	cases := map[string]string{
		"fnm-v1.39.0-LICENSE":  "Schniz", // GPL: la línea de la FSF y la plantilla no cuentan
		"bob-v4.3.0-LICENSE":   "Copyright (c) 2022 Mordechai hadad and Nethanel Menachem Eitan",
		"buf-v1.75.0-LICENSE":  "Copyright 2020-2026 Buf Technologies, Inc.",
		"aqua-v2.66.1-LICENSE": "Copyright (c) 2021 Shunsuke Suzuki",
	}
	for f, want := range cases {
		if got := copyrightHolder(m, read(f)); got != want {
			t.Errorf("%s: %q; se esperaba %q", f, got, want)
		}
	}
	m.License.Copyright = "2019 Gal Schlezinger"
	if got := copyrightHolder(m, read("fnm-v1.39.0-LICENSE")); got != "2019 Gal Schlezinger" {
		t.Errorf("license.copyright no se usa como respaldo: %q", got)
	}
}

func TestChangelogReproducible(t *testing.T) {
	a, err := Changelog(fnm(), "Juan Pavon <pavon.gnu@gmail.com>", 1772810627)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Changelog(fnm(), "Juan Pavon <pavon.gnu@gmail.com>", 1772810627)
	if !bytes.Equal(a, b) {
		t.Fatal("changelog no determinista")
	}
	// Cabecera gzip: sin nombre (FLG=0), MTIME=0 y XFL=2 (compresión máxima).
	if a[3] != 0 || a[4]|a[5]|a[6]|a[7] != 0 || a[8] != 2 {
		t.Errorf("cabecera gzip = % x", a[:10])
	}
	zr, _ := gzip.NewReader(bytes.NewReader(a))
	text, _ := io.ReadAll(zr)
	want := "fnm (1.39.0-0~tepache1) unstable; urgency=medium\n\n" +
		"  * Repackage upstream release 1.39.0 with debfactory.\n\n" +
		" -- Juan Pavon <pavon.gnu@gmail.com>  Fri, 06 Mar 2026 15:23:47 +0000\n"
	if string(text) != want {
		t.Errorf("changelog:\n%s", text)
	}
}

func TestLintianOverrides(t *testing.T) {
	got := string(LintianOverrides(fnm()))
	want := "# Static by design.\nfnm: statically-linked-binary [usr/bin/fnm]\n" +
		"# Upstream ships none.\nfnm: no-manual-page\n"
	if got != want {
		t.Errorf("overrides:\n%s", got)
	}
	m := fnm()
	m.LintianOverrides = nil
	if LintianOverrides(m) != nil {
		t.Error("sin overrides no debe generarse el archivo")
	}
	if strings.Contains(got, "[/usr") {
		t.Error("la ruta no debe llevar la / inicial")
	}
}
