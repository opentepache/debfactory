# Data Model: Paquetes de nivel 2

## 1. Manifiesto de nivel 2 (`packages/<p>.yaml` + `packages/<p>/debian/`)

```yaml
schema: 1
name: tepache-hello
version: 1.0.0
revision: 1
level: 2
justification: |
  Enlaza con libcrypto: cada suite necesita su binario contra su OpenSSL.
summary: prints the OpenSSL version it links against
description: |
  Test package for level-2 source builds.
homepage: https://example.invalid/tepache-hello
upstream: opentepache/tepache-hello
license: {spdx: MIT, file: {archive: LICENSE}}
value: Test fixture; never published.
source:
  url: https://example.invalid/tepache-hello-{{version}}.tar.gz
  format: tar.gz
build:                    # opcional
  tmpfs: 6g
exclude:
  suites:                 # acota la prueba (tasks T003); noble cubre Ubuntu
    forky: acotar la prueba de nivel 2
    sid: acotar la prueba de nivel 2
    resolute: acotar la prueba de nivel 2
```

```text
packages/tepache-hello/debian/
├── control      # Source: tepache-hello; Build-Depends: debhelper-compat (= 13), libssl-dev
├── rules        # ejecutable; dh $@
├── copyright
└── source/format   # 3.0 (quilt)   (opcional: la herramienta lo pone si falta)
```

| Campo | Reglas |
|---|---|
| `level` | `1` (por defecto) o `2` |
| `justification` | obligatorio si `level: 2`; texto no vacío (constitución II) |
| `source.url`, `source.format` | obligatorios si `level: 2`; plantillas `{{version}}`, `{{tag}}`; formato `tar.gz`, `tar.xz` o `tar.bz2` |
| `artifact`, `install`, `completions`, `arch_map`, `checksums` | prohibidos si `level: 2` |
| `build.tmpfs` | opcional; ≤ `level2.tmpfs_max` de la configuración |
| `debian/control` | `Source:` = `name`; sin `debian/changelog` |
| lockfile | `versions.<v>.source: {url, sha256, date}` (en vez de `artifacts` por arquitectura); `date` es la publicación del release upstream y alimenta el changelog (FR-002) |

## 2. Estado (`repo/state.yaml`, `schema: 2`)

Una versión de nivel 1 no cambia. Una de nivel 2:

```yaml
schema: 2
serial: 4
packages:
  tepache-hello:
    - version: 1.0.0-0~tepache1          # versión base, sin sufijo
      level: 2
      published: 2026-12-01T10:00:00Z
      orig: {file: tepache-hello_1.0.0.orig.tar.gz, sha256: "…"}
      builds:
        bookworm:
          version: 1.0.0-0~tepache1+deb12
          dsc: {sha256: "…"}
          debian_tar: {sha256: "…"}
          debs: {amd64: "…", arm64: "…"}
        trixie:
          version: 1.0.0-0~tepache1+deb13
          dsc: {sha256: "…"}
          debian_tar: {sha256: "…"}
          debs: {amd64: "…", arm64: "…"}
```

`suites` de una versión de nivel 2 son las claves de `builds`: al leer el estado, `Published.Suites`
se rellena en memoria con esas claves (en el orden de la configuración), para que el código que ya
lo usa (generación, retención, ampliación, sitio) siga funcionando, y `Validate` exige que
coincidan. Cada `builds.<s>.<arch>` registra también el sha256 de su `.buildinfo`. La retención y el retiro trabajan
sobre la versión base. Validación: cada `builds.<s>.version` es la base + `+<level2_suffix de s>`.

## 3. Auditoría

`publish` y `extend` de nivel 2 llevan `builds` (las suites publicadas en esa operación, con sus
sha256) y `orig` en vez de `debs`.

## 4. Configuración

```yaml
level2:
  jobs: 3          # compilaciones simultáneas
  tmpfs: 6g        # tmpfs por compilación
  tmpfs_max: 12g   # límite para build.tmpfs de un manifiesto
suites:
  bookworm: {…, mirror: "http://deb.debian.org/debian"}   # opcional; por defecto el de la distribución
```

`ci doctor` comprueba `jobs × tmpfs ≤ 18g` (31 GB de la PC menos runners y servicios).
