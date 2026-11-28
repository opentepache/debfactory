package zon

import (
	"os"
	"strings"
	"testing"
)

// TestGhosttyZon: el build.zig.zon real de ghostty 1.4.0.
func TestGhosttyZon(t *testing.T) {
	src, err := os.ReadFile("../../testdata/zon/build.zig.zon")
	if err != nil {
		t.Fatal(err)
	}
	deps, err := Dependencies(src)
	if err != nil {
		t.Fatal(err)
	}
	var remote, local int
	byName := map[string]Dependency{}
	for _, d := range deps {
		byName[d.Name] = d
		if d.Path != "" {
			local++
		} else {
			remote++
		}
	}
	if remote != 15 || local == 0 {
		t.Errorf("remotas %d, locales %d", remote, local)
	}
	u := byName["uucode"]
	if !strings.HasPrefix(u.URL, "https://deps.files.ghostty.org/uucode-") || u.Hash != "uucode-0.2.0-ZZjBPoGFVgCiflJNFc6Io8xzC67B6PA5bUkk4LT0kCxg" {
		t.Errorf("uucode = %+v", u)
	}
	if !byName["libxev"].Lazy || byName["translate_c"].Path != "./pkg/translate-c" {
		t.Errorf("lazy/path: %+v %+v", byName["libxev"], byName["translate_c"])
	}
}

// TestPkgZons: los build.zig.zon de pkg/ tienen una dependencia remota.
func TestPkgZons(t *testing.T) {
	for _, f := range []string{"pkg_translate-c_build.zig.zon", "pkg_spirv-cross_build.zig.zon", "pkg_harfbuzz_build.zig.zon"} {
		src, _ := os.ReadFile("../../testdata/zon/" + f)
		deps, err := Dependencies(src)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		n := 0
		for _, d := range deps {
			if d.URL != "" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: %d remotas (%+v)", f, n, deps)
		}
	}
}

// TestZonEdge: comentarios, comas finales, identificadores @"…", git+.
func TestZonEdge(t *testing.T) {
	src := `.{
    .name = .x, // comentario
    .paths = .{ "build.zig", "src" },
    .dependencies = .{
        .@"zig-js" = .{ .url = "git+https://codeberg.org/a/b#0123", .hash = "zig_js-0.0.0-AAA", },
    },
}`
	deps, err := Dependencies([]byte(src))
	if err != nil || len(deps) != 1 || deps[0].Name != "zig-js" || !strings.HasPrefix(deps[0].URL, "git+") {
		t.Fatalf("%v %+v", err, deps)
	}
	for name, bad := range map[string]string{
		"sin hash":   `.{ .dependencies = .{ .a = .{ .url = "https://x" } } }`,
		"path y url": `.{ .dependencies = .{ .a = .{ .url = "https://x", .hash = "h", .path = "p" } } }`,
		"repetido":   `.{ .name = .a, .name = .b }`,
		"sin cerrar": `.{ .name = "a }`,
		"código":     `.{ .x = @import("y") }`,
		"sobrante":   `.{} extra`,
	} {
		if _, err := Dependencies([]byte(bad)); err == nil {
			t.Errorf("%s: se esperaba error", name)
		}
	}
	if deps, err := Dependencies([]byte(`.{ .name = .x }`)); err != nil || deps != nil {
		t.Errorf("sin dependencias: %v %v", deps, err)
	}
}
