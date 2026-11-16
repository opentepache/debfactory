# Contrato: configuración compartida

Dos archivos en `config/`, validados en Go al arrancar el binario (tipos, campos obligatorios y
que cada patrón de `never_override_patterns` compile como regex).

## `config/debfactory.yaml`

```yaml
schema: 1
maintainer: Juan Pavon <pavon.gnu@gmail.com>

arches: [amd64, arm64]

suites:            # orden = orden del informe
  bookworm: {image: "debian:bookworm", level2_suffix: deb12}
  trixie:   {image: "debian:trixie",   level2_suffix: deb13}
  sid:      {image: "debian:sid",      level2_suffix: debsid}
  noble:    {image: "ubuntu:noble",    level2_suffix: u2404}
  resolute: {image: "ubuntu:resolute", level2_suffix: u2604}

lintian:
  image: "debian:trixie"
  fail_on: error
  never_override_patterns:  # regex; constitución VII: seguridad, licencia y copyright
    - "^copyright-"
    - "^license-problem-"
    - "^dep5-"
    - "^missing-license"
    - "^no-copyright-file$"
    - "^embedded-library$"
    - "^setuid-binary$"
    - "^setgid-binary$"
    - "^non-standard-executable-perm$"

elf_dependencies:          # NEEDED -> paquete; {glibc} se reemplaza por la GLIBC_x.y máxima
  libc.so.6:       "libc6 (>= {glibc})"
  libm.so.6:       "libc6 (>= {glibc})"
  libdl.so.2:      "libc6 (>= {glibc})"
  librt.so.1:      "libc6 (>= {glibc})"
  libpthread.so.0: "libc6 (>= {glibc})"
  libgcc_s.so.1:   "libgcc-s1"
  ld-linux-x86-64.so.2:  "libc6 (>= {glibc})"   # el cargador, si un binario lo declara en NEEDED
  ld-linux-aarch64.so.1: "libc6 (>= {glibc})"   # (bob arm64 lo hace)

community_repos:           # constitución V (1.7.0): solo bloquean si sirven gratis, sin registro y al día
  - name: griffo-free      # espejo gratuito; deb.griffo.io exige suscripción desde 2026-10-01 y no cuenta
    url: https://deb-free.griffo.io

completions_image: "debian:trixie"

repo:                      # fase 002: ver specs/002-signed-apt-repo/contracts/config.md
  url: ""
  origin: OpenTepache
  label: OpenTepache
  component: main
  keep: 3
  signing: {masters: [], subkey: "", warn_days: 60}
  destinations:
    local: {remote: /var/tmp/debfactory-repo, cache_control: false}
    r2: {remote: "r2:debfactory-apt", public_url: "", cache_control: true}
  cache_control:
    immutable: "public, max-age=31536000, immutable"
    mutable: "public, max-age=60"
```

La sección `repo` la usa la publicación (fase 002); su contrato está en
[002 config.md](../../002-signed-apt-repo/contracts/config.md).

## `config/licenses.yaml`

Allowlist de la constitución IV. Ampliarla requiere enmienda MINOR de la constitución.

```yaml
schema: 1
allowed:
  - MIT
  - Apache-2.0
  - BSD-2-Clause
  - BSD-3-Clause
  - ISC
  - MPL-2.0
  - GPL-2.0-only
  - GPL-2.0-or-later
  - GPL-3.0-only
  - GPL-3.0-or-later
  - LGPL-2.1-only
  - LGPL-2.1-or-later
  - LGPL-3.0-only
  - LGPL-3.0-or-later
```

## Ubicaciones fijas que no se configuran

| Qué | Dónde |
|---|---|
| bash completion | `/usr/share/bash-completion/completions/<cmd>` |
| zsh completion | `/usr/share/zsh/vendor-completions/_<cmd>` |
| fish completion | `/usr/share/fish/vendor_completions.d/<cmd>.fish` |
| man pages | `/usr/share/man/man<N>/<archivo>.gz` (`gzip -9n`) |
| copyright | `/usr/share/doc/<pkg>/copyright` (formato DEP-5) |
| changelog | `/usr/share/doc/<pkg>/changelog.Debian.gz` |
| overrides de lintian | `/usr/share/lintian/overrides/<pkg>` |

`<cmd>` es el nombre del paquete.
