# Contrato: configuración compartida

Dos archivos en `config/`, validados con su propio schema al arrancar el binario.

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

community_repos:           # constitución V; comprobación manual en la fase 1
  - name: griffo
    url: https://debian.griffo.io

completions_image: "debian:trixie"
```

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
