package site

import (
	"slices"
	"strings"
)

// commands genera los bloques de la portada desde el modelo (research D8 de
// la fase 006; D4 de la 007): el mismo procedimiento deb822 con Signed-By de
// docs/usuario.md del repositorio de paquetes, sin nada que editar a mano y
// sin ninguna suite escrita en el código.
//
//   - detect: toma el codename de /etc/os-release, primero UBUNTU_CODENAME y
//     DEBIAN_CODENAME (los declaran las derivadas: Linux Mint, LMDE,
//     Pop!_OS…) y si no VERSION_CODENAME. Si coincide con una suite
//     configurada, la elige. Si no, la suite catchall de la distribución del
//     sistema (ID), si la hay; en Debian, solo si /etc/debian_version la
//     nombra (sid y testing dicen ".../sid"). Si nada, no escribe el
//     .sources y lo dice.
//   - una variante explícita por distribución: sus codenames, y si no, su
//     catchall.
func commands(m *Model) Commands {
	var named []string                // codenames de todas las suites sin catchall
	byDistro := map[string][]string{} // codenames sin catchall de cada distribución
	catchall := map[string]string{}   // la suite catchall de cada distribución
	var distros []string
	for _, s := range m.Repo.Suites {
		if !slices.Contains(distros, s.Distro) {
			distros = append(distros, s.Distro)
		}
		if s.Catchall {
			catchall[s.Distro] = s.Name
			continue
		}
		named = append(named, s.Name)
		byDistro[s.Distro] = append(byDistro[s.Distro], s.Name)
	}
	// fallback es la rama *) del bloque detect: la catchall de la distribución
	// del sistema.
	var fallback []string
	for _, d := range distros {
		c, ok := catchall[d]
		if !ok {
			continue
		}
		cond := `[ "$ID" = ` + d + ` ]`
		if d == "debian" {
			cond += ` && grep -qs ` + c + ` /etc/debian_version`
		}
		fallback = append(fallback, `{ `+cond+` && echo `+c+`; }`)
	}
	detectCase := `case "$s" in ` + strings.Join(named, "|") + `) echo "$s" ;;`
	if len(fallback) > 0 {
		detectCase += ` *) ` + strings.Join(fallback, " || ") + ` ;;`
	}
	detectCase += ` esac`
	distroSuite := func(d string) string {
		cs := byDistro[d]
		c, ok := catchall[d]
		switch {
		case ok && len(cs) > 0:
			return `suite=$(. /etc/os-release; case "$VERSION_CODENAME" in ` + strings.Join(cs, "|") +
				`) echo "$VERSION_CODENAME" ;; *) echo ` + c + ` ;; esac)`
		case ok:
			return `suite=` + c
		}
		return `suite=$(. /etc/os-release; echo "$VERSION_CODENAME")`
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
		`suite=$(. /etc/os-release; s="${UBUNTU_CODENAME:-${DEBIAN_CODENAME:-$VERSION_CODENAME}}"; ` + detectCase + `)`,
		`[ -n "$suite" ] || echo "OpenTepache: this distribution is not supported; the repository was not added" >&2`,
	}
	return Commands{
		Detect: Command{Step: "add", Script: lines(prelude, detect, sources(`[ -n "$suite" ] && `))},
		Debian: Command{Step: "add-debian", Script: lines(prelude, []string{distroSuite("debian")}, sources(""))},
		Ubuntu: Command{Step: "add-ubuntu", Script: lines(prelude, []string{distroSuite("ubuntu")}, sources(""))},
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
