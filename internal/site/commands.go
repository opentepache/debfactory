package site

import "strings"

// commands genera los bloques de la portada desde el modelo (research D8):
// el mismo procedimiento deb822 con Signed-By de docs/usuario.md del
// repositorio de paquetes, sin nada que editar a mano.
//
//   - detect (FR-002): toma la suite de /etc/os-release, primero
//     UBUNTU_CODENAME y DEBIAN_CODENAME (los declaran las derivadas: Linux
//     Mint, LMDE, Pop!_OS…) y si no VERSION_CODENAME. En Debian, un codename
//     que no es una estable soportada es sid (en sid, os-release dice el
//     nombre de la próxima estable). Si no corresponde a ninguna suite
//     publicada, no escribe el .sources y lo dice.
//   - debian y ubuntu: la variante explícita de cada distribución.
func commands(m *Model) Commands {
	var stable, debian []string // suites estables; estables de Debian
	for _, s := range m.Repo.Suites {
		if s.Name == "sid" {
			continue
		}
		stable = append(stable, s.Name)
		if s.Distro == "debian" {
			debian = append(debian, s.Name)
		}
	}
	key := m.URL + "/" + m.Repo.Keyring.Path
	prelude := []string{
		"sudo apt-get update && sudo apt-get install -y curl ca-certificates",
		"sudo install -d -m 0755 /etc/apt/keyrings",
		"sudo curl -fsSLo " + m.Repo.Keyring.InstalledAs + " " + key,
	}
	sources := func(guard string) []string {
		return []string{
			guard + "sudo tee " + SourcesFile + " >/dev/null <<EOF",
			"Types: deb",
			"URIs: " + m.URL + "/",
			"Suites: $suite",
			"Components: " + m.Repo.Component,
			"Architectures: " + strings.Join(m.Repo.Architectures, " "),
			"Signed-By: " + m.Repo.Keyring.InstalledAs,
			"EOF",
			"sudo apt-get update",
		}
	}
	lines := func(parts ...[]string) string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return strings.Join(out, "\n")
	}
	detect := []string{
		`suite=$(. /etc/os-release; s="${UBUNTU_CODENAME:-${DEBIAN_CODENAME:-$VERSION_CODENAME}}"; case "$s" in ` +
			strings.Join(stable, "|") + `) echo "$s" ;; *) [ "$ID" = debian ] && grep -qs sid /etc/debian_version && echo sid ;; esac)`,
		`[ -n "$suite" ] || echo "OpenTepache: this distribution is not supported; the repository was not added" >&2`,
	}
	debianSuite := `suite=$(. /etc/os-release; case "$VERSION_CODENAME" in ` + strings.Join(debian, "|") +
		`) echo "$VERSION_CODENAME" ;; *) echo sid ;; esac)`
	ubuntuSuite := `suite=$(. /etc/os-release; echo "$VERSION_CODENAME")`
	return Commands{
		Detect: Command{Step: "add", Script: lines(prelude, detect, sources(`[ -n "$suite" ] && `))},
		Debian: Command{Step: "add-debian", Script: lines(prelude, []string{debianSuite}, sources(""))},
		Ubuntu: Command{Step: "add-ubuntu", Script: lines(prelude, []string{ubuntuSuite}, sources(""))},
		Verify: Command{Step: "verify-key", Script: strings.Join([]string{
			"sudo apt-get install -y gnupg",
			"gpg --show-keys --with-fingerprint " + m.Repo.Keyring.InstalledAs,
		}, "\n")},
		Remove: Command{Step: "remove", Script: strings.Join([]string{
			"sudo rm -f " + SourcesFile + " " + m.Repo.Keyring.InstalledAs,
			"sudo apt-get update",
		}, "\n")},
	}
}
