package site

import (
	"strings"
	"testing"
)

func TestCommands(t *testing.T) {
	m := newFixture(t).load(t)
	c := m.Commands
	if c.Debian.Step != "add-debian" || c.Ubuntu.Step != "add-ubuntu" || c.Verify.Step != "verify-key" || c.Remove.Step != "remove" {
		t.Errorf("steps = %+v", c)
	}
	for _, s := range []string{c.Debian.Script, c.Ubuntu.Script} {
		for _, want := range []string{
			"sudo curl -fsSLo /etc/apt/keyrings/debfactory.gpg https://apt.opentepache.org/debfactory.gpg",
			"URIs: https://apt.opentepache.org/\n",
			"Suites: $suite\n",
			"Components: main\n",
			"Architectures: amd64 arm64\n",
			"Signed-By: /etc/apt/keyrings/debfactory.gpg\n",
			"/etc/apt/sources.list.d/debfactory.sources",
			"VERSION_CODENAME",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("falta %q en:\n%s", want, s)
			}
		}
	}
	// En sid, os-release dice el codename de la próxima estable: todo lo que
	// no sea una estable soportada es sid.
	if !strings.Contains(c.Debian.Script, `in bookworm|trixie) echo "$VERSION_CODENAME" ;; *) echo sid ;; esac`) {
		t.Errorf("selección de suite de Debian:\n%s", c.Debian.Script)
	}
	if !strings.Contains(c.Remove.Script, "rm -f /etc/apt/sources.list.d/debfactory.sources /etc/apt/keyrings/debfactory.gpg") {
		t.Errorf("remove:\n%s", c.Remove.Script)
	}
	for _, s := range []string{c.Debian.Script, c.Ubuntu.Script, c.Verify.Script, c.Remove.Script} {
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "$ ") || strings.HasPrefix(line, "# ") {
				t.Errorf("línea con prompt: %q", line)
			}
		}
	}
}

// TestDetectCommand: el bloque principal detecta la suite, también en
// derivadas, y no agrega el repositorio si no reconoce la distribución (FR-002).
func TestDetectCommand(t *testing.T) {
	s := newFixture(t).load(t).Commands.Detect
	if s.Step != "add" {
		t.Errorf("step = %q", s.Step)
	}
	for _, want := range []string{
		`s="${UBUNTU_CODENAME:-${DEBIAN_CODENAME:-$VERSION_CODENAME}}"`,
		`case "$s" in bookworm|trixie|noble|resolute) echo "$s" ;; *) [ "$ID" = debian ] && grep -qs sid /etc/debian_version && echo sid ;; esac`,
		`[ -n "$suite" ] || echo "OpenTepache: this distribution is not supported; the repository was not added" >&2`,
		`[ -n "$suite" ] && sudo tee /etc/apt/sources.list.d/debfactory.sources >/dev/null <<EOF`,
	} {
		if !strings.Contains(s.Script, want) {
			t.Errorf("falta %q en:\n%s", want, s.Script)
		}
	}
}
