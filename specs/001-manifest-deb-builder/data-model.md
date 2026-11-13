# Data Model: Constructor de .deb desde manifiestos declarativos

Todo es archivo en el repositorio o en caché local; no hay base de datos. Contratos formales:
[manifest.v1.schema.json](contracts/manifest.v1.schema.json),
[config.md](contracts/config.md), [cli.md](contracts/cli.md).

## Entidades

### Manifest (`packages/<name>.yaml`)

Lo escribe una persona. Campos y reglas en el JSON Schema; aquí solo se resumen las reglas que
**no** expresa el schema y se validan en Go (FR-010):

| Regla | Mensaje de error (ejemplo) |
|---|---|
| `name` coincide con el nombre del archivo (el campo `revision` guarda N de `-0~tepache<N>`) | `packages/fnm.yaml: name: "fnmx" no coincide con el archivo "fnm"` |
| Solo variables `{{version}}`, `{{tag}}`, `{{arch}}` (y `{{name}}` solo en `install[].src` con `format: binary`) | `…: artifact.url: variable desconocida {{arc}}` |
| `artifact.url` y `checksums.url` empiezan por `https://github.com/<upstream>/releases/download/<tag renderizado>/` | `…: artifact.url: fuera de los releases de Schniz/fnm` |
| `summary` sin artículo inicial (a/an/the) | `…: summary: no debe empezar con artículo` |
| Claves de `exclude.suites` existen en la config | `…: exclude.suites.bullseye: suite no soportada` |
| `level` es `1` (constitución II) | `…: level: solo se admite 1 en esta fase` |
| Si `version` no está en el lockfile y este tiene otras versiones, `revision` es `1` | `…: revision: debe ser 1 para una versión upstream nueva` |
| `license.spdx` es una expresión SPDX válida | `…: license.spdx: "MIT/Apache" no es SPDX` |
| `lintian_overrides[].tag` no coincide con ningún patrón de `lintian.never_override_patterns` | `…: lintian_overrides[0].tag: "embedded-library" no se puede silenciar` |
| Destinos literales únicos (los que salen de globs se comprueban al ensamblar, etapa `assemble`) | `…: install[1].dst: /usr/bin/fnm duplicado` |
| `test.command` y `completions.*.generate` son una orden y sus argumentos separados por espacios, sin metacaracteres de shell (`;`, `\|`, `&`, `$`, `` ` ``, `<`, `>`, `(`, `)`, comillas); se ejecutan sin shell (constitución I) | `…: test.command: no se admiten metacaracteres de shell` |
| Con `format: binary`, `install` tiene exactamente un elemento | — |

**Defaults** que aplica el builder: `tag: v{{version}}`, `section: devel`, `mode` 0755 si el
archivo de origen tiene el bit ejecutable y 0644 si no, `test.expect: {{version}}`.
`test.command` por defecto es `<basename del primer archivo instalado bajo /usr/bin> --version`;
se resuelve **después** de ensamblar, porque con globs (buf) el nombre no se conoce antes.

### LockFile (`packages/<name>.lock.yaml`)

Lo genera `debfactory lock`; nunca `build`. Ejemplo:
[fnm.lock.yaml](contracts/examples/fnm.lock.yaml).

| Campo | Tipo | Regla |
|---|---|---|
| `schema` | int | `1` |
| `versions.<ver>.source_date_epoch` | int | `published_at` del release upstream |
| `versions.<ver>.license_sha256` | hex(64) | sha256 del texto de licencia |
| `versions.<ver>.checksums_sha256` | hex(64), opcional | sha256 del archivo de checksums upstream; obligatorio si el manifiesto declara `checksums` (constitución III) |
| `versions.<ver>.artifacts.<arch>` | hex(64) | sha256 del asset descargado |

Las versiones viejas se conservan (permiten reconstruir y comparar); `lock --prune` las borra
salvo la actual.

### Config (`config/debfactory.yaml`, `config/licenses.yaml`)

Datos compartidos (FR-009). Detalle en [config.md](contracts/config.md): mantenedor, suites
(nombre, imagen, sufijo de nivel 2), arquitecturas, mapa de bibliotecas a dependencias, lista de
etiquetas de lintian que nunca se pueden silenciar, repositorios comunitarios conocidos y la
allowlist de licencias.

### Artifact (en tiempo de ejecución)

`{package, version, arch, url, sha256, path_en_cache}`. Solo pasa a la caché
(`~/.cache/debfactory/sha256/<hash>`) después de verificar el hash; un archivo en caché es
válido por definición.

### BuildPlan (en tiempo de ejecución)

Resultado de resolver Manifest + Lock + Config para una `(versión, arch)`:
lista ordenada de entradas `{dst, origen, mode, tipo: file|dir}`, metadatos de control
(Package, Version `<v>-0~tepache<N>`, Architecture, Maintainer, Section, Priority, Homepage,
Depends calculado ∪ declarado, Recommends, Conflicts, Replaces, Provides, Description),
`copyright`, `changelog.Debian.gz`, overrides de lintian y `SOURCE_DATE_EPOCH`. Es la entrada
de nfpm.

### Package (`dist/<name>_<version>_<arch>.deb`)

Salida. Un solo `.deb` por `(versión, arch)` sirve para todas las suites (nivel 1). Va
acompañado de `dist/<name>_<version>_<arch>.build.json`:
`{sha256, manifest_sha256, lock_sha256, lintian: "pass", overrides: [...], test_command}`, donde
`test_command` es la orden de prueba ya resuelta (la del manifiesto o la de por defecto).
`<version>` en estos nombres es siempre la versión completa `<upstream>-0~tepache<N>`.

### TestReport (`dist/<name>_<version>_<arch>.test.json`)

| Campo | Valores |
|---|---|
| `deb_sha256` | sha256 del `.deb` probado; `publish` (fase 002) exige que coincida con `build.json` |
| `results[].suite` | bookworm … resolute |
| `results[].status` | `pass` · `fail` · `excluded` |
| `versions` | `pass` · `fail`: comprobación de orden con `dpkg --compare-versions` (FR-024), una vez por `.deb`, antes de las suites |
| `results[].stage` | `install` · `run` · `version` · `purge` · `leftovers` (si `fail`) |
| `results[].reason` | motivo de la exclusión o salida relevante del fallo (recortada) |
| `overall` | `pass` si `versions` es `pass` y ningún resultado es `fail` |

## Ciclo de vida de una versión

```text
manifest editado ──lock──▶ lock con hashes ──build──▶ .deb + build.json ──test──▶ test.json
      │                         │                          │                         │
   validate                verifica contra           lintian (falla               overall=fail
   (falla → error)         checksums upstream        → no queda .deb)             bloquea publicar
                                                                                 (fase futura)
```

Errores: cada etapa devuelve un error tipado `{package, version, arch, stage, cause, detail}`
que se imprime como una línea legible, más el detalle (FR-019). Etapas, en orden: `validate`,
`lock`, `fetch`, `verify`, `license`, `assemble`, `package`, `lintian`, `test`. El gate de
licencias va después de `verify` y antes de `assemble` (constitución, Flujo, paso 3).
