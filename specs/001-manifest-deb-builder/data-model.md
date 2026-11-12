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
| `name` coincide con el nombre del archivo | `packages/fnm.yaml: name: "fnmx" no coincide con el archivo "fnm"` |
| Solo variables `{{version}}`, `{{tag}}`, `{{arch}}` | `…: artifact.url: variable desconocida {{arc}}` |
| `artifact.url` y `checksums.url` empiezan por `https://github.com/<upstream>/releases/download/<tag renderizado>/` | `…: artifact.url: fuera de los releases de Schniz/fnm` |
| `summary` sin artículo inicial (a/an/the) | `…: summary: no debe empezar con artículo` |
| Claves de `exclude.suites` y `exclude.arches` existen en la config | `…: exclude.suites.bullseye: suite no soportada` |
| Arquitecturas de `arch_map` ∖ `exclude.arches` = arquitecturas a construir | `…: arch_map: falta arm64 y no está excluida` |
| `license.spdx` es una expresión SPDX válida | `…: license.spdx: "MIT/Apache" no es SPDX` |
| `lintian_overrides[].tag` no está en `lintian.never_override` | `…: lintian_overrides[0].tag: "embedded-library" no se puede silenciar` |
| Destinos únicos (incluidos enlaces y archivos generados) | `…: install[1].dst: /usr/bin/fnm duplicado` |
| Con `format: binary`, `install` tiene exactamente un elemento | — |

**Defaults** que aplica el builder: `tag: v{{version}}`, `section: devel`, `mode` 0755 bajo
`/usr/bin`, `/usr/sbin` y para ejecutables de `type: tree` (según el bit x del artefacto), 0644
para lo demás, `test.command: <basename del primer destino en /usr/bin> --version`,
`test.expect: {{version}}`.

### LockFile (`packages/<name>.lock.yaml`)

Lo genera `debfactory lock`; nunca `build`. Ejemplo:
[fnm.lock.yaml](contracts/examples/fnm.lock.yaml).

| Campo | Tipo | Regla |
|---|---|---|
| `schema` | int | `1` |
| `versions.<ver>.source_date_epoch` | int | `published_at` del release upstream |
| `versions.<ver>.license_sha256` | hex(64) | sha256 del texto de licencia |
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
lista ordenada de entradas `{dst, origen, mode, tipo: file|dir|symlink}`, metadatos de control
(Package, Version `<v>-0~pav<N>`, Architecture, Maintainer, Section, Priority, Homepage,
Depends calculado ∪ declarado, Recommends, Conflicts, Replaces, Provides, Description),
`copyright`, `changelog.Debian.gz`, overrides de lintian y `SOURCE_DATE_EPOCH`. Es la entrada
de nfpm.

### Package (`dist/<name>_<version>_<arch>.deb`)

Salida. Un solo `.deb` por `(versión, arch)` sirve para todas las suites (nivel 1). Va
acompañado de `dist/<name>_<version>_<arch>.build.json`:
`{sha256, manifest_sha256, lock_sha256, lintian: "pass", overrides: [...]}`.

### TestReport (`dist/<name>_<version>_<arch>.test.json`)

| Campo | Valores |
|---|---|
| `results[].suite` | bookworm … resolute |
| `results[].status` | `pass` · `fail` · `excluded` |
| `results[].stage` | `install` · `run` · `version` · `purge` · `leftovers` (si `fail`) |
| `results[].reason` | motivo de la exclusión o salida relevante del fallo (recortada) |
| `overall` | `pass` si ningún resultado es `fail` |

## Ciclo de vida de una versión

```text
manifest editado ──lock──▶ lock con hashes ──build──▶ .deb + build.json ──test──▶ test.json
      │                         │                          │                         │
   validate                verifica contra           lintian (falla               overall=fail
   (falla → error)         checksums upstream        → no queda .deb)             bloquea publicar
                                                                                 (fase futura)
```

Errores: cada etapa devuelve un error tipado `{package, version, arch, stage, cause, detail}`
que se imprime como una línea legible, más el detalle (FR-019). Etapas: `validate`, `fetch`,
`verify`, `license`, `assemble`, `package`, `lintian`, `test`.
