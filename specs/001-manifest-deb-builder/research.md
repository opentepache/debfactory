# Research: Constructor de .deb desde manifiestos declarativos

**Feature**: `001-manifest-deb-builder` · **Fecha**: 2026-11-11

Todos los datos de upstream se obtuvieron el 2026-11-11 descargando los assets reales y
listando su contenido; los binarios se analizaron con `file` y `readelf` sin ejecutarlos. Las
severidades de lintian se verificaron construyendo `.deb` de prueba con los binarios reales y
pasándoles `lintian` 2.122.0 (Debian trixie).

## 1. Hallazgos por paquete

### 1.1 Resumen

| | fnm | pnpm | bob | buf | aqua |
|---|---|---|---|---|---|
| Versión | v1.39.0 | v12.18.0 | v4.3.0 | v1.75.0 | v2.66.1 |
| Publicado | 2026-03-06 | 2026-11-05 | 2026-10-30 | 2026-10-22 | 2026-11-04 |
| Tag | `v{{version}}` | `v{{version}}` | `v{{version}}` | `v{{version}}` | `v{{version}}` |
| Formato | `.zip` | `.tar.gz` | `.zip` | `.tar.gz` | `.tar.gz` |
| Asset amd64 | `fnm-linux.zip` | `pnpm-linux-x64.tar.gz` | `bob-linux-x86_64.zip` | `buf-Linux-x86_64.tar.gz` | `aqua_linux_amd64.tar.gz` |
| Asset arm64 | `fnm-arm64.zip` | `pnpm-linux-arm64.tar.gz` | `bob-linux-arm.zip` | `buf-Linux-aarch64.tar.gz` | `aqua_linux_arm64.tar.gz` |
| Mapa arch | `linux` / `arm64` | `x64` / `arm64` | `x86_64` / `arm` | `x86_64` / `aarch64` | `amd64` / `arm64` |
| Checksums upstream | ninguno | ninguno | ninguno | `sha256.txt` (+ `.minisig`) | `aqua_{{version}}_checksums.txt` (+ sigstore) |
| Licencia (SPDX) | GPL-3.0-only¹ | MIT | MIT | Apache-2.0 | MIT |
| Licencia en artefacto | no | no | no | `buf/LICENSE` | `LICENSE` (+ `third_party_licenses/`) |
| API GitHub detecta | `GPL-3.0` (ambiguo) | MIT | MIT | Apache-2.0 | **404** (no detecta) |
| Enlazado | estático (static-pie en amd64) | dinámico, glibc ≥ 2.18 | dinámico, **glibc ≥ 2.39** | estático (Go) | estático (Go) |
| Bibliotecas | — | libc, libm, libdl, librt, libpthread, libgcc_s | libc, libm, libgcc_s | — | — |
| Ejecutables | `fnm` | `pnpm` (+ árbol `dist/`) | `bob` | `buf`, `protoc-gen-buf-breaking`, `protoc-gen-buf-lint` | `aqua` |
| Completions | se generan (`fnm completions --shell X`) | se generan (`pnpm completion X`) | se generan (`bob complete X`) | incluidas: bash, zsh, fish | se generan (`aqua completion X`) |
| Man pages | no | no | no | 113 (`man1/*.1`, ambas arch) | no |

¹ `Cargo.toml` declara `license = "GPL-3.0"` (identificador SPDX obsoleto equivalente a
`GPL-3.0-only`). El texto de `LICENSE` es idéntico para `-only` y `-or-later`, así que el
detector de texto no puede distinguirlos.

### 1.2 Rutas internas

- **fnm**: el `.zip` contiene solo `fnm` en la raíz.
- **pnpm**: `pnpm` en la raíz más `dist/` (580 entradas: `dist/node_modules/…`,
  `dist/node-gyp-bin/…`). El ejecutable es un binario Node SEA que usa `dist/` como recursos
  junto a sí mismo, así que se instala el árbol completo en `/usr/lib/pnpm/` y se crea el
  enlace `/usr/bin/pnpm → /usr/lib/pnpm/pnpm`. El "nombre por plataforma" ya no está dentro del
  archivo (lo está en el asset); no hace falta renombrar el ejecutable, pero sí el árbol.
- **bob**: un directorio con el nombre del asset: `bob-linux-x86_64/bob` y `bob-linux-arm/bob`;
  la ruta interna depende de la arquitectura.
- **buf**: `buf/bin/{buf,protoc-gen-buf-breaking,protoc-gen-buf-lint}`,
  `buf/etc/bash_completion.d/buf`, `buf/share/zsh/site-functions/_buf`,
  `buf/share/fish/vendor_completions.d/buf.fish`, `buf/share/man/man1/*.1`, `buf/LICENSE`.
- **aqua**: `aqua`, `LICENSE`, `README.md` y `third_party_licenses/…` (unas 170 entradas, entre
  ellas archivos `.go` y `.github/` copiados por la herramienta de licencias de upstream).

### 1.3 Consecuencias para el esquema

| Caso real | Capacidad del esquema |
|---|---|
| Nombres de asset distintos por arch (todos) | `arch_map` por paquete + plantilla `{{arch}}` |
| fnm usa `linux` para amd64 | el mapa es libre, no se deduce |
| bob: ruta interna con `{{arch}}` | plantillas también en rutas de origen |
| buf: 3 ejecutables y 113 man pages | `install` como lista; `src` admite glob; `man` con glob |
| buf: completions en el artefacto | `completions.<shell>.file` |
| fnm/pnpm/bob/aqua: completions generadas | `completions.<shell>.generate` (orden a ejecutar) |
| pnpm: árbol `dist/` + enlace | `install` con `type: tree` + `links` |
| bob: glibc 2.39 | `Depends` calculado desde ELF + `exclude.suites.bookworm` |
| buf/aqua: checksums publicados | `checksums.url` + `checksums.format: sha256sum` |
| fnm/pnpm/bob: sin checksums | hash en lockfile |
| aqua: API no detecta licencia | texto desde el artefacto (`license.file.archive`) |
| pnpm/fnm/bob: sin licencia en artefacto | texto del repo al tag (`license.file.repo`) |
| buf/aqua: binario Go estático | `lintian_overrides` con motivo |

## 2. Decisiones técnicas

### D1. Lenguaje y toolchain: Go 1.26 desde trixie-backports

- **Decision**: Go con `golang-1.26` (1.26.10) de `trixie-backports`; `go.mod` con `go 1.26`.
- **Rationale**: decisión del usuario (binario único; nfpm como biblioteca). El Go de trixie
  (1.24) es demasiado viejo para nfpm reciente. nfpm v2.49.0 exige Go 1.27.4 y backports solo
  tiene 1.27.3; nfpm v2.47/v2.48 exige 1.26.4, que backports cubre. Usar paquetes de Debian
  cumple la constitución IX y evita que `GOTOOLCHAIN` descargue compiladores en silencio.
- **Alternatives considered**: `GOTOOLCHAIN=auto` (descarga sin control, choca con III);
  instalar Go de go.dev (fuera de Debian, choca con IX); `golang-1.27` de backports (1.27.3, una
  versión por debajo de lo que pide nfpm v2.49.0).
- **Configuración**: `GOTOOLCHAIN=local` en el `justfile` para que un `go.mod` más nuevo falle
  en lugar de descargar.

### D2. Generación del .deb: nfpm v2.48.x como biblioteca

- **Decision**: `github.com/goreleaser/nfpm/v2` (última v2.48.x), packager `deb`, con
  `VersionSchema: "none"`, `Version: <upstream>`, `Release: "0~pav<N>"` y `MTime` igual a
  `SOURCE_DATE_EPOCH`.
- **Rationale**: en `deb/deb.go` (v2.48.0) la versión se compone como
  `Version[~Prerelease][+Metadata]-Release`, así que `Release = "0~pav1"` produce exactamente
  `1.40.0-0~pav1`. nfpm escribe `md5sums`, `Installed-Size` y aplica `MTime` a todos los
  miembros. `VersionSchema: "none"` evita que interprete versiones que no son semver.
- **Alternatives considered**: `dpkg-deb` como proceso externo (el usuario lo descartó y obliga
  a fijar permisos y orden a mano); `debhelper` (requiere árbol `debian/` por paquete, contrario
  a I).
- **Riesgos que verificar al inicio** (tarea de spike): (a) que dos builds den bytes idénticos,
  incluido `control.tar.gz`; (b) que el changelog no lo genere nfpm, sino el builder como
  archivo normal comprimido con `gzip -n`.

### D3. Formato de versión: confirmado con lintian

- **Decision**: `<upstream>-0~pav<N>` como manda la constitución VI.
- **Rationale**: `lintian` da **error** `debian-revision-is-zero` para `1.39.0-0`, pero **no**
  para `1.39.0-0~pav1` (verificado). El orden con `dpkg --compare-versions` ya se verificó al
  enmendar la constitución.

### D4. Manifiestos YAML + JSON Schema

- **Decision**: un archivo `packages/<nombre>.yaml` por paquete, validado con JSON Schema
  2020-12 (`schema/manifest.v1.schema.json`, embebido en el binario con `go:embed`) usando
  `github.com/santhosh-tekuri/jsonschema/v6` (v6.0.3, Go ≥ 1.21, Apache-2.0).
  `additionalProperties: false` en todos los objetos (FR-010).
- **Rationale**: santhosh-tekuri/jsonschema da errores con ruta JSON Pointer de cada fallo, lo
  que permite el mensaje "archivo, campo, regla". YAML se decodifica a `any` y se valida antes
  de mapearlo a structs.
- **Validaciones fuera del schema** (semánticas, en Go): plantillas con variables desconocidas,
  URLs fuera del upstream declarado, exclusión de suites inexistentes, SPDX bien formado.
- **Alternatives considered**: `xeipuuv/gojsonschema` (sin mantenimiento, draft-07); CUE (otra
  herramienta y otro lenguaje que aprender, choca con IX).

### D5. Plantillas de URL y rutas

- **Decision**: sustitución literal de `{{version}}`, `{{tag}}` y `{{arch}}` (valor upstream
  según `arch_map`), sin lógica. Una variable desconocida es un error de validación.
- **Rationale**: los 5 casos solo necesitan sustitución. `text/template` permitiría lógica en
  los manifiestos, lo que convierte datos en código (contrario a I).
- **Alternatives considered**: `text/template` (demasiado poder); variables por entorno (oculta
  datos fuera del manifiesto).

### D6. Descarga, caché y lockfile

- **Decision**:
  - Caché direccionada por contenido en `~/.cache/debfactory/sha256/<hash>`; nada se guarda en
    la caché hasta que se verifica el hash.
  - Lockfile por paquete, `packages/<nombre>.lock.yaml`, con, por versión: sha256 de cada
    artefacto por arquitectura, sha256 del texto de licencia y la fecha de publicación del
    release (`source_date_epoch`).
  - El lockfile solo lo escribe `debfactory lock <pkg>`, nunca `build`. `lock` verifica antes
    contra los checksums upstream cuando existen. El diff del lockfile se revisa en git.
  - `build` verifica cada artefacto contra el lockfile **y**, si existen, contra los checksums
    upstream. Si falta la entrada de lock para esa versión, falla y sugiere ejecutar `lock`.
- **Rationale**: cumple la constitución III en los dos casos (con o sin checksums upstream) y
  además detecta que upstream reemplace un asset junto con su archivo de checksums. Guardar la
  fecha del release en el lock hace el build reproducible sin consultar la API de GitHub.
- **Alternatives considered**: hash dentro del manifiesto (mezcla datos que edita una persona
  con datos generados y vuelve los diffs ruidosos); solo checksums upstream (no detecta un
  reemplazo coordinado).
- **Firmas** (minisign de buf, sigstore de aqua): **no se verifican en la fase 1**. La
  constitución III exige checksums *o* firmas; la verificación de firmas queda como mejora
  posterior.

### D7. Gate de licencias

- **Decision**: detección local sobre el texto con `github.com/google/licensecheck` (BSD-3,
  Go puro). El manifiesto declara la expresión SPDX esperada y de dónde sale el texto:
  `license.file.archive` (ruta dentro del artefacto) o `license.file.repo` (ruta en el repo
  upstream al tag exacto, descargada de `raw.githubusercontent.com/<repo>/<tag>/<ruta>`).
- **Reglas**:
  1. El texto debe identificarse con cobertura suficiente; si no, el gate falla ("no
     identificable").
  2. La licencia detectada debe ser compatible con la declarada. Un texto GPL-3.0 es compatible
     con `GPL-3.0-only` y con `GPL-3.0-or-later`; el manifiesto decide cuál, porque el texto no
     lo distingue.
  3. Cada término de la expresión SPDX declarada se evalúa contra la allowlist
     (`config/licenses.yaml`): con `OR` basta una alternativa; con `AND` hacen falta todas.
  4. El sha256 del texto debe coincidir con el del lockfile.
- **Rationale**: la API de GitHub no sirve como fuente: no detecta aqua (404) y para fnm
  devuelve el identificador obsoleto `GPL-3.0`. Detectar sobre el texto de la versión exacta
  cumple IV y no depende de un servicio externo.
- **Alternatives considered**: `go-license-detector` (sigue mantenido, pero arrastra muchas
  dependencias y está pensado para repositorios completos); `askalono` (Rust, proceso externo);
  la API de GitHub (fallos de arriba).

### D8. Dependencias calculadas desde ELF

- **Decision**: el builder lee las entradas `NEEDED` y las versiones `GLIBC_x.y` de cada ELF
  instalado (`debug/elf`) y genera `Depends` con un mapa de configuración:
  `libc.so.6`, `libm.so.6`, `libdl.so.2`, `librt.so.1` y `libpthread.so.0` → `libc6 (>= <máxima
  GLIBC_x.y>)`; `libgcc_s.so.1` → `libgcc-s1`. Una biblioteca que no esté en el mapa hace fallar
  el build con un mensaje claro. Se suma a los `depends` del manifiesto.
- **Rationale**: lintian da **error** `missing-dependency-on-libc` sin esa dependencia; es una
  regla genérica que sirve para cualquier binario (I) y evita copiar `Depends` a mano. Con
  `libc6 (>= 2.39)`, bob ni siquiera se instala en bookworm (glibc 2.36), y la prueba lo
  reporta con claridad.
- **Alternatives considered**: `dpkg-shlibdeps` (necesita un árbol `debian/` y una base shlibs
  de la suite); declararlo a mano en cada manifiesto (propenso a errores).

### D9. lintian

- **Decision**: `lintian` 2.122 dentro de un contenedor `debian:trixie` efímero, con perfil
  `debian`, `--fail-on error`. Los overrides se declaran en el manifiesto por etiqueta y con
  motivo, y el builder los instala en `/usr/share/lintian/overrides/<pkg>`.
- **Severidades verificadas con los binarios reales**:

  | Etiqueta | Severidad | Afecta a |
  |---|---|---|
  | `statically-linked-binary` | **E** | buf, aqua (Go estático) |
  | `debian-revision-is-zero` | **E** | solo con revisión `0` a secas (no `0~pavN`) |
  | `missing-dependency-on-libc` | **E** | binarios dinámicos sin `Depends` (resuelto en D8) |
  | `no-changelog`, `no-copyright-file` | **E** | si faltan (el builder los genera) |
  | `uncompressed-manual-page` | **E** | buf si no se comprimen (el builder las comprime) |
  | `no-manual-page` | W | fnm, pnpm, bob, aqua |
  | `copyright-without-copyright-notice` | W | si `copyright` no tiene una línea "Copyright" |
  | `shared-library-lacks-prerequisites` | W | fnm (static-pie) |

- **Rationale**: `statically-linked-binary` es un error que no se puede corregir sin recompilar
  (nivel 2). Un override declarado y justificado es el mecanismo estándar de Debian, y la spec
  lo permite salvo para etiquetas de seguridad o licencia. La lista de etiquetas que **nunca**
  se pueden silenciar vive en `config/debfactory.yaml` y se valida.
- **Alternatives considered**: lintian del host (la versión cambia con el host; el contenedor
  la fija).

### D10. Pruebas de instalación

- **Decision**: Docker (29.9 ya instalado en la PC) con las imágenes oficiales
  `debian:bookworm`, `debian:trixie`, `debian:sid`, `ubuntu:noble` y `ubuntu:resolute`; todas
  publican `amd64` y `arm64v8` (verificado en Docker Hub). arm64 por emulación con
  `qemu-user-binfmt` (trixie 1:10.0.13; hoy no hay handlers binfmt registrados en la PC).
- **Secuencia por suite y arquitectura**, en contenedor efímero con `--platform`:
  1. `apt-get update` (con red) e instalación del `.deb` con `apt-get install ./pkg.deb`, que
     resuelve dependencias.
  2. Orden de comprobación **sin red** (`docker network disconnect`), con `HOME` temporal.
  3. La salida debe contener la versión esperada.
  4. `apt-get purge -y`, y luego no debe quedar ninguna ruta de `dpkg -L` (capturada antes de
     la purga) en el sistema.
- **Optimización**: imágenes base con `apt-get update` ya hecho, cacheadas localmente por suite
  y arquitectura (`debfactory-test/<suite>:<arch>`), reconstruidas si tienen más de 24 h.
- **Alternatives considered**: Podman (equivalente; se elige Docker porque ya está instalado y
  la interfaz es la misma); `systemd-nspawn`/`debootstrap` (más trabajo para Ubuntu).

### D11. Completions generadas: en contenedor, nunca en el host

- **Decision**: `completions.<shell>.generate` ejecuta el binario de upstream dentro de un
  contenedor `debian:trixie` efímero, sin red, con el sistema de archivos de solo lectura y la
  plataforma de la arquitectura objetivo (arm64 por qemu). Se captura `stdout`; salida vacía o
  código distinto de 0 hace fallar el build.
- **Rationale**: ejecutar binarios de terceros en el host contradice el espíritu de III. Con
  plataforma fija, la salida es determinista para la misma versión.
- **Alternatives considered**: ejecutar en el host (riesgo innecesario); no ofrecer completions
  para fnm, pnpm, bob y aqua (pierde valor).

### D12. Reproducibilidad

- **Decision**: `SOURCE_DATE_EPOCH` = `published_at` del release upstream, guardado en el
  lockfile. Se aplica a la fecha de modificación (mtime) de todos los archivos, a la fecha del
  changelog y a los `gzip -n` (man pages y changelog). Propietario `root:root`, permisos
  normalizados (0755 ejecutables y directorios, 0644 lo demás) y archivos ordenados.
- **Verificación**: `debfactory build --check-reproducible` construye dos veces en directorios
  de trabajo distintos y compara los sha256; forma parte de `just check`.

### D13. Interfaz: binario `debfactory` + `justfile`

- **Decision**: el CLI real es el binario Go (`debfactory build|lock|lint|test|validate`). Un
  `justfile` (`just` 1.40 está en trixie) da los atajos `build <pkg> [arch]`, `test <pkg>`,
  `lint <pkg>`, `build-all` y `check`.
- **Rationale**: los argumentos posicionales en `make` son incómodos; `just` está empaquetado en
  Debian (IX). La lógica vive en Go y es testeable, y el `justfile` solo encadena órdenes.

### D14. Directorio de trabajo en tmpfs

- **Decision**: `--workdir` por defecto en `$XDG_RUNTIME_DIR/debfactory` (tmpfs en Debian), con
  respaldo en `os.TempDir()`. Se borra al terminar, también si falla. La salida final se
  escribe en `dist/` de forma atómica: primero a un archivo temporal y luego `rename`.
- **Rationale**: velocidad, sin basura en disco y sin `.deb` parciales en `dist/` (FR-019).

## 3. Riesgos abiertos (se verifican en las primeras tareas)

1. **Reproducibilidad byte a byte de nfpm** (D2): si `control.tar.gz` u otro miembro no es
   determinista, se fija con opciones de nfpm o se post-procesa. Se bloquea SC-006 hasta
   verificarlo.
2. **pnpm SEA con enlace simbólico**: confirmar que `/usr/bin/pnpm → /usr/lib/pnpm/pnpm`
   encuentra `dist/` (Node resuelve `process.execPath` al destino real). Si no, se instala un
   envoltorio, que el esquema ya permite como enlace o como archivo.
3. **Completions bajo qemu**: algunos binarios pueden fallar bajo emulación; en ese caso se
   generan solo con el binario amd64. Son texto independiente de la arquitectura, pero la
   misma salida debe usarse en los dos `.deb`.
4. **`licensecheck` con textos MIT y BSD que tienen variaciones de copyright**: confirmar la
   cobertura con los 5 textos reales en las pruebas unitarias.
