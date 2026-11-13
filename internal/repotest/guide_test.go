package repotest

import (
	"strings"
	"testing"

	"debfactory/internal/config"
)

func TestGuideBlocks(t *testing.T) {
	blocks, err := Blocks("../../" + GuideFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	url := cfg.Repo.URL
	for _, step := range []string{"add", "verify", "install", "upgrade", "downgrade", "remove"} {
		if blocks[step] == "" {
			t.Errorf("falta el bloque %q", step)
		}
	}
	if n := CountCommands(blocks["add"]); n > 4 {
		t.Errorf("add tiene %d comandos (FR-031: ≤ 4)", n)
	}
	add := Render(blocks["add"], url, "http://repo", map[string]string{"suite": "trixie"})
	for _, want := range []string{
		"curl -fsSLo /etc/apt/keyrings/debfactory.gpg http://repo/debfactory.gpg",
		"URIs: http://repo/\n", "Suites: trixie\n", "\napt-get update",
	} {
		if !strings.Contains(add, want) {
			t.Errorf("add renderizado no contiene %q:\n%s", want, add)
		}
	}
	if strings.Contains(add, "sudo") || strings.Contains(add, strings.TrimPrefix(url, "https://")) || strings.Contains(add, "apt.example.org") || strings.Contains(add, "<suite>") {
		t.Errorf("add sin sustituir:\n%s", add)
	}
	inst := Render(blocks["install"], url, "http://repo", map[string]string{"packages": "fnm buf aqua"})
	if strings.TrimSpace(inst) != "apt-get install -y fnm buf aqua" {
		t.Errorf("install = %q", inst)
	}
	down := Render(blocks["downgrade"], url, "http://repo", map[string]string{"version": "1.0-0~tepache1"})
	if !strings.Contains(down, "fnm=1.0-0~tepache1") {
		t.Errorf("downgrade = %q", down)
	}
}

func TestCountCommands(t *testing.T) {
	b := "a\nb <<'EOF'\nx\ny\nEOF\nc\n"
	if n := CountCommands(b); n != 3 {
		t.Errorf("CountCommands = %d", n)
	}
}
