package manifest

import (
	"os"
	"strings"
	"testing"
)

func readGhostty(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/fixtures/ghostty/ghostty.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestGhosttyManifest: el manifiesto de ghostty pasa el schema y las reglas
// (specs/009-hosted-hardened-builds, data-model §1).
func TestGhosttyManifest(t *testing.T) {
	m, err := Parse([]byte(readGhostty(t)), "packages/ghostty.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(m, repoConfig(t), nil); err != nil {
		t.Fatal(err)
	}
	if m.Signing.Kind() != "minisign" || m.Toolchain.Signing.Kind() != "minisign" || m.Deps.Manager != "zig" ||
		len(m.Source.ExtraFiles) != 4 {
		t.Fatalf("decodificación incompleta: %+v", m)
	}
	u, err := m.Toolchain.RenderURL(m.Toolchain.URL, "arm64")
	if err != nil || u != "https://ziglang.org/download/0.17.0/zig-aarch64-linux-0.17.0.tar.xz" {
		t.Errorf("toolchain url = %s, %v", u, err)
	}
}

// TestPhase009Rules: reglas semánticas nuevas.
func TestPhase009Rules(t *testing.T) {
	g := readGhostty(t)
	fnm := readExample(t, "fnm")
	cases := map[string]struct{ src, rel, want string }{
		"todas las arquitecturas excluidas": {fnm + "exclude:\n  arches: {amd64: no cabe en el runner estandar, arm64: no cabe en el runner estandar}\n",
			"fnm", "exclude.arches: debe quedar al menos una arquitectura"},
		"fuente fuera de GitHub sin firma": {strings.Replace(g, "signing:\n  minisign:\n    key: RWQlAjJC23149WL2sEpT/l0QKy7hMIFhYdQOFy0Z7z7PbneUgvlsnYcV\n    file: https://release.files.ghostty.org/{{version}}/ghostty-{{version}}.tar.gz.minisig\n", "", 1),
			"ghostty", "source.url: fuera de GitHub: solo se admite con signing fijado"},
		"fuente en un dominio ajeno": {strings.Replace(g, "https://release.files.ghostty.org/{{version}}/ghostty-{{version}}.tar.gz\n", "https://evil.example/ghostty.tar.gz\n", 1),
			"ghostty", "source.url: fuera de los releases y tags"},
		"firma de otro sitio": {strings.Replace(g, "file: https://release.files.ghostty.org/{{version}}/ghostty-{{version}}.tar.gz.minisig", "file: https://evil.example/x.minisig", 1),
			"ghostty", "signing: la firma debe venir del release"},
		"firma del toolchain de otro sitio": {strings.Replace(g, "file: https://ziglang.org/download/{{version}}/zig-{{arch}}-linux-{{version}}.tar.xz.minisig", "file: https://evil.example/zig.minisig", 1),
			"ghostty", "toolchain.signing: la firma debe venir del mismo sitio"},
		"comodín en extra_files": {strings.Replace(g, "path: VERSION,", "path: src/*.c,", 1), "ghostty", "sin comodines"},
		"ruta con ..":            {strings.Replace(g, "path: VERSION,", "path: ../VERSION,", 1), "ghostty", "ruta inválida"},
		"extra repetido": {strings.Replace(g, "path: src/apprt/gtk/ghostty_resources.h,", "path: VERSION,", 1),
			"ghostty", "VERSION repetido"},
		"nivel 1 con toolchain": {fnm + "toolchain:\n  name: zig\n  version: 0.17.0\n  url: \"https://ziglang.org/{{version}}/{{arch}}\"\n  arch_map: {amd64: x, arm64: y}\n",
			"fnm", "toolchain: solo se admite en nivel 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := Parse([]byte(c.src), "packages/"+c.rel+".yaml")
			if err != nil {
				t.Fatalf("no pasa el schema: %v", err)
			}
			err = Check(m, repoConfig(t), nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v\nse esperaba %q", err, c.want)
			}
		})
	}
	// Schema: dos tipos de firma a la vez, llave minisign mal formada.
	for name, src := range map[string]string{
		"dos firmas":   strings.Replace(g, "signing:\n  minisign:", "signing:\n  gpg: {fingerprint: 72671CD221684BF7FDCDFB3965B278975D4FE04B, key_file: upstream.asc, file: https://x}\n  minisign:", 1),
		"llave rara":   strings.Replace(g, "key: RWQlAjJC23149WL2sEpT/l0QKy7hMIFhYdQOFy0Z7z7PbneUgvlsnYcV", "key: nope", 1),
		"motivo corto": strings.Replace(g, "reason: version file written when packaging the release", "reason: x", 1),
	} {
		if _, err := Parse([]byte(src), "packages/ghostty.yaml"); err == nil {
			t.Errorf("%s: se esperaba un error del schema", name)
		}
	}
}

// TestLicenseExceptions (constitución IV 1.12.0): solo licencias de allowed o
// exception_only, una excepción por dependencia, rutas sin "..".
func TestLicenseExceptions(t *testing.T) {
	cfg := repoConfig(t)
	base := func() *Manifest {
		m, err := Parse([]byte(readGhostty(t)), "packages/ghostty.yaml")
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name string
		x    []LicenseException
		want string
	}{
		{"ok", []LicenseException{{Dependency: "harfbuzz", Licenses: []string{"APSL-2.0"}, Reason: "motivo suficiente"}}, ""},
		{"omitida", []LicenseException{{Dependency: "breakpad", Licenses: []string{"MIT"}, Reason: "motivo suficiente"}}, "deps.skip"},
		{"fuera", []LicenseException{{Dependency: "x", Licenses: []string{"SSPL-1.0"}, Reason: "motivo suficiente"}}, "exception_only"},
		{"doble", []LicenseException{{Dependency: "x", Licenses: []string{"MIT"}, Reason: "motivo suficiente"},
			{Dependency: "x", Licenses: []string{"MIT"}, Reason: "motivo suficiente"}}, "más de una"},
		{"ruta", []LicenseException{{Dependency: "x", Licenses: []string{"MIT"}, RepoFile: "../x", Reason: "motivo suficiente"}}, "ruta relativa"},
		{"paquete con file", []LicenseException{{Licenses: []string{"APSL-2.0"}, File: "LICENSE", Reason: "motivo suficiente"}}, "solo lleva"},
	}
	for _, c := range cases {
		m := base()
		m.LicenseExceptions = c.x
		err := Check(m, cfg, nil)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
