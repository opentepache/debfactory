package site

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if !strings.Contains(c.Debian.Script, `in bookworm|trixie|forky) echo "$VERSION_CODENAME" ;; *) echo sid ;; esac`) {
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
		`case "$s" in bookworm|trixie|forky|noble|resolute) echo "$s" ;; *) { [ "$ID" = debian ] && grep -qs sid /etc/debian_version && echo sid; } ;; esac`,
		`[ -n "$suite" ] || echo "OpenTepache: this distribution is not supported; the repository was not added" >&2`,
		`[ -n "$suite" ] && sudo tee /etc/apt/sources.list.d/debfactory.sources >/dev/null <<EOF`,
	} {
		if !strings.Contains(s.Script, want) {
			t.Errorf("falta %q en:\n%s", want, s.Script)
		}
	}
}

// TestDetectSimulated ejecuta con sh la línea que elige la suite, con
// /etc/os-release y /etc/debian_version simulados (specs/007, FR-009, FR-010).
func TestDetectSimulated(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sin sh")
	}
	c := newFixture(t).load(t).Commands
	line := func(script string) string {
		for _, l := range strings.Split(script, "\n") {
			if strings.HasPrefix(l, "suite=") {
				return l
			}
		}
		t.Fatalf("sin línea suite= en:\n%s", script)
		return ""
	}
	run := func(script, osRelease, debianVersion string) string {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "os-release"), []byte(osRelease), 0o644)
		if debianVersion != "" {
			os.WriteFile(filepath.Join(dir, "debian_version"), []byte(debianVersion+"\n"), 0o644)
		}
		l := strings.NewReplacer("/etc/os-release", filepath.Join(dir, "os-release"),
			"/etc/debian_version", filepath.Join(dir, "debian_version")).Replace(line(script))
		out, err := exec.Command("sh", "-c", l+`; printf %s "$suite"`).Output()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return string(out)
	}
	cases := []struct {
		name, osRelease, debianVersion, detect, debian string
	}{
		{"forky", "ID=debian\nVERSION_CODENAME=forky\n", "forky/sid", "forky", "forky"},
		{"sid (declara forky)", "ID=debian\nVERSION_CODENAME=forky\n", "forky/sid", "forky", "forky"},
		{"testing futura", "ID=debian\nVERSION_CODENAME=duke\n", "duke/sid", "sid", "sid"},
		{"trixie", "ID=debian\nVERSION_CODENAME=trixie\n", "13.2", "trixie", "trixie"},
		{"LMDE 7", "ID=linuxmint\nVERSION_CODENAME=gigi\nDEBIAN_CODENAME=trixie\n", "13.2", "trixie", ""},
		{"Linux Mint 22", "ID=linuxmint\nVERSION_CODENAME=wilma\nUBUNTU_CODENAME=noble\n", "trixie/sid", "noble", ""},
		{"Ubuntu 22.04", "ID=ubuntu\nVERSION_CODENAME=jammy\nUBUNTU_CODENAME=jammy\n", "bookworm/sid", "", ""},
		{"Kali", "ID=kali\nVERSION_CODENAME=kali-rolling\n", "kali-rolling", "", ""},
	}
	for _, tc := range cases {
		if got := run(c.Detect.Script, tc.osRelease, tc.debianVersion); got != tc.detect {
			t.Errorf("%s: Detectar eligió %q, se esperaba %q", tc.name, got, tc.detect)
		}
		if tc.debian != "" {
			if got := run(c.Debian.Script, tc.osRelease, tc.debianVersion); got != tc.debian {
				t.Errorf("%s: la pestaña Debian eligió %q, se esperaba %q", tc.name, got, tc.debian)
			}
		}
	}
}

// TestNoCatchall: sin suite catchall no hay rama de comodín.
func TestNoCatchall(t *testing.T) {
	f := newFixture(t)
	for i := range f.cfg.Suites {
		f.cfg.Suites[i].Catchall = false
	}
	c := f.load(t).Commands
	if strings.Contains(c.Detect.Script, "*)") || strings.Contains(c.Debian.Script, "*)") {
		t.Errorf("rama comodín sin catchall:\n%s\n%s", c.Detect.Script, c.Debian.Script)
	}
}
