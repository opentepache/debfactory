---
description: "Task list for 001-manifest-deb-builder"
---

# Tasks: Constructor de .deb desde manifiestos declarativos

**Input**: Design documents from `/specs/001-manifest-deb-builder/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: se incluyen solo donde la spec los exige explícitamente: casos negativos (US2,
SC-005), reproducibilidad (SC-006), cobertura del detector de licencias con textos reales,
orden de versiones (FR-024) y la prueba de instalación (US3). Los tests Go viven junto al código
(`*_test.go`), como es convención en Go.

**Organization**: por historia de usuario; cada fase deja algo comprobable por sí solo.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: se puede hacer en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia a la que pertenece (US1…US5)

## Path Conventions

Un solo módulo Go en la raíz del repo (ver plan.md § Project Structure): `cmd/debfactory/`,
`internal/<etapa>/`, `schema/`, `config/`, `packages/`, `testdata/`, `justfile`.
Entorno: `PATH=/usr/lib/go-1.26/bin:$PATH`, `GOTOOLCHAIN=local`.

Convenciones que usan varias tareas:

- **Nombre del `.deb`**: `<name>_<upstream>-0~pav<N>_<arch>.deb` (p. ej.
  `fnm_1.39.0-0~pav1_amd64.deb`); `<upstream>` es `version` del manifiesto y `N` es `pav`.
- **Paquetes iniciales**: fnm, bob, buf y aqua (constitución v1.5.0; pnpm descartado, research
  §1.2b). Todas las arquitecturas soportadas (amd64, arm64) son obligatorias.
- **Orden del pipeline** (constitución, Flujo): `validate → lock → fetch → verify → license →
  assemble → package → lintian`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: esqueleto del módulo, configuración compartida y atajos.

- [X] T001 Crear `go.mod` con `module debfactory` y `go 1.26`, y los directorios `cmd/debfactory/`, `internal/{config,manifest,lock,fetch,license,assemble,container,pkg,lint,installtest,report,build}/`, `schema/`, `config/`, `packages/`, `testdata/{manifests/valid,manifests/invalid,licenses,fixtures/ok,negative}/`
- [X] T002 [P] Crear `.gitignore` con `dist/`, `bin/` y `*.deb`
- [X] T003 [P] Crear `config/debfactory.yaml` copiando literalmente el bloque YAML de `specs/001-manifest-deb-builder/contracts/config.md` (maintainer, arches, suites con `image` y `level2_suffix`, `lintian` con `never_override_patterns`, `elf_dependencies`, `community_repos`, `completions_image`)
- [X] T004 [P] Crear `config/licenses.yaml` copiando literalmente la allowlist de `specs/001-manifest-deb-builder/contracts/config.md`
- [X] T005 [P] Copiar `specs/001-manifest-deb-builder/contracts/manifest.v1.schema.json` a `schema/manifest.v1.schema.json` y crear `schema/embed.go` (package `schema`) que lo exponga con `//go:embed` como `ManifestV1 []byte`
- [X] T006 [P] Crear `justfile` con `export GOTOOLCHAIN := "local"`, `PATH` con `/usr/lib/go-1.26/bin`, una receta `bin` (`go build -o bin/debfactory ./cmd/debfactory`) de la que dependen las demás, y las recetas de `contracts/cli.md` (`build pkg arch=""`, `build-all`, `lint pkg`, `test pkg arch=""`, `lock pkg`, `validate`, `check`) llamando a `bin/debfactory`
- [X] T007 Añadir dependencias a `go.mod`/`go.sum`: `github.com/goreleaser/nfpm/v2` (última v2.48.x; **no** v2.49, que exige Go 1.27.4), `github.com/santhosh-tekuri/jsonschema/v6@v6.0.3`, `github.com/google/licensecheck`, `go.yaml.in/yaml/v3` (no `gopkg.in/yaml.v3`, archivado); verificar que `go build ./...` funciona con Go 1.26.10

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: errores, config, carga y validación de manifiestos, lockfile, contenedores y los tres
spikes de riesgo que condicionan el diseño (research.md §3).

**⚠️ CRITICAL**: ninguna historia empieza hasta terminar esta fase.

- [X] T008 Implementar `internal/report/errors.go`: tipo `Error{Package, Version, Arch, Stage, Cause, Detail}`, constantes de etapa `validate, lock, fetch, verify, license, assemble, package, lintian, test` (en ese orden); `Error()` con el formato exacto de `contracts/cli.md` (`debfactory: <pkg> <version> <arch>: <stage>: <causa>`, `-` si el campo está vacío, detalle indentado con dos espacios) y `MarshalJSON` para `--json`
- [X] T009 [P] Implementar `internal/config/config.go` (+ `config_test.go`): cargar y validar en Go `config/debfactory.yaml` y `config/licenses.yaml` (campos de `contracts/config.md`; `schema: 1`; suites con `image` no vacío; `arches` = `[amd64, arm64]`; `elf_dependencies` cuyos valores solo usan el marcador `{glibc}`; cada entrada de `lintian.never_override_patterns` compila como regex); error tipado con etapa `validate`
- [X] T010 [P] Definir structs en `internal/manifest/types.go` que reflejen 1:1 `schema/manifest.v1.schema.json` (Manifest con `Pav int` para N y `Level int`, License, LicenseFile{Archive,Repo}, Artifact, Checksums, InstallItem{Src,Dst,Mode}, Completion{File,Generate}, Exclude{Suites}, Test, LintianOverride{Tag,Path,Reason})
- [X] T011 Implementar `internal/manifest/load.go` (+ `load_test.go`): leer `packages/<name>.yaml` con go.yaml.in/yaml/v3 a `any`, normalizar claves a `map[string]any`, validar con santhosh-tekuri/jsonschema v6 contra `schema.ManifestV1` y **traducir al español** cada tipo de error de la librería (`required` → `falta la propiedad "<p>"`, `additionalProperties` → `propiedad desconocida "<p>"`, `enum`/`const` → `valor no permitido`, `pattern` → `no cumple el formato`, `type` → `tipo incorrecto`, `minLength`/`minItems`/`minimum` → `demasiado corto`/`demasiado pocos elementos`/`demasiado pequeño`, `maxLength` → `demasiado largo`, `minProperties`/`maxProperties` → `debe tener exactamente una de: <claves>`, `format` → `formato inválido`), con la forma `packages/<f>.yaml: <campo.con.puntos>: <mensaje>`; decodificar en structs y aplicar los defaults de data-model.md que no dependen del artefacto: `tag: v{{version}}`, `section: devel`, `test.expect: {{version}}` (el `mode` y el `test.command` por defecto se resuelven al ensamblar, T024 y T033)
- [X] T012 Implementar `internal/manifest/template.go` (+ `template_test.go`): `Render(s string, vars Vars)` con sustitución literal de `{{version}}`, `{{tag}}` y `{{arch}}`, y `{{name}}` solo en `install[].src` con `format: binary`; cualquier otra `{{…}}` es error `variable desconocida {{x}}`
- [X] T013 Implementar `internal/license/spdx.go` (+ test): parser mínimo de expresiones SPDX (identificadores, `OR`, `AND`, `WITH`, paréntesis) y evaluación contra la allowlist (`OR`: basta una alternativa; `AND`: todas)
- [X] T014 Implementar `internal/manifest/semantic.go` (+ `semantic_test.go` con un fixture por regla en `testdata/manifests/invalid/`) con las reglas de data-model.md: `name` coincide con el archivo; solo variables conocidas; `artifact.url` y `checksums.url` empiezan por `https://github.com/<upstream>/releases/download/<tag renderizado>/`; `summary` sin artículo inicial (a/an/the); claves de `exclude.suites` existen en la config; `level` es `1`; `license.spdx` es una expresión SPDX válida (con el parser de T013); `lintian_overrides[].tag` no coincide con ningún patrón de `lintian.never_override_patterns`; destinos **literales** únicos (los de globs se comprueban en T053); `test.command` y `completions.*.generate` sin metacaracteres de shell (`;`, `|`, `&`, `$`, `` ` ``, `<`, `>`, `(`, `)`, comillas), porque se ejecutan sin shell; con `format: binary`, `install` tiene exactamente un elemento
- [X] T015 [P] Implementar `internal/lock/lock.go` (+ `lock_test.go`): leer y escribir `packages/<name>.lock.yaml` con `schema: 1` y `versions.<ver>.{source_date_epoch:int, license_sha256:hex64, checksums_sha256?:hex64, artifacts.{amd64,arm64}:hex64}` (`checksums_sha256` obligatorio si el manifiesto declara `checksums`, constitución III); salida determinista (claves ordenadas) y comentario de cabecera "Generado por `debfactory lock`; no se edita a mano"
- [X] T016 [P] Implementar `internal/container/docker.go` sobre `docker` con `os/exec`: `Build(ctx, tag, dockerfile string, platform)`, `Run(ctx, Opts{Image, Platform, Network ("none"|"bridge"), ReadOnly, Mounts, Env, Workdir, Cmd}) (stdout, stderr, exit)`, `Start(ctx, Opts) (id)` (desacoplado, `sleep infinity`), `Exec(ctx, id, env, cmd) (stdout, stderr, exit)`, `DisconnectNetwork(ctx, id)`, `Remove(ctx, id)` y `Preflight()`, que comprueba `docker info` y `/proc/sys/fs/binfmt_misc/qemu-aarch64` (si falta, error de entorno → código de salida 3)
- [X] T017 Implementar `cmd/debfactory/main.go`: subórdenes `validate|lock|build|lint|test`, flags globales `--root`, `--workdir` (por defecto `$XDG_RUNTIME_DIR/debfactory`, si no `os.TempDir()`), `--cache` (por defecto `~/.cache/debfactory`), `--json`; códigos de salida 0/1/2/3 de `contracts/cli.md`; implementar ya `validate [pkg…]` (config + load + semantic)
- [X] T018 Spike de reproducibilidad de nfpm en `internal/pkg/spike_test.go`: construir dos veces un `.deb` trivial con nfpm (`VersionSchema: "none"`, `Version: "1.0.0"`, `Release: "0~pav1"`, `MTime` fijo, un archivo 0755 y otro 0644, owner root) en directorios distintos y comparar sha256; comprobar con `dpkg-deb -f` que Version = `1.0.0-0~pav1`. Anotar el resultado (y la corrección si no es determinista, p. ej. en `control.tar.gz`) en `specs/001-manifest-deb-builder/research.md` §3 riesgo 1
- [X] T019 [P] Spike del detector de licencias en `internal/license/detect_test.go`: guardar en `testdata/licenses/` los textos reales al tag exacto (`fnm-v1.39.0-LICENSE`, `bob-v4.3.0-LICENSE`, `buf-v1.75.0-LICENSE`, `aqua-v2.66.1-LICENSE`) y comprobar que `google/licensecheck` los identifica como GPL-3.0 / MIT / Apache-2.0 / MIT con cobertura ≥ 90 %; anotar el resultado en research.md §3 riesgo 4
- [X] T020 Spike de completions bajo qemu: con `internal/container` (o `docker run` a mano), ejecutar `fnm completions --shell bash`, `bob complete bash` y `aqua completion bash` con `--platform linux/arm64` y `linux/amd64` en `debian:trixie` sin red, con `--read-only` y el binario montado en `/stage/usr/bin` (las mismas condiciones que T056); anotar en research.md §3 riesgo 3 si las salidas son idénticas por arquitectura o si hay que generarlas solo con amd64 y reutilizarlas

**Checkpoint**: `go test ./...` pasa, `debfactory validate` funciona y los tres spikes están
resueltos y documentados.

---

## Phase 3: User Story 1 - Construir un paquete a partir de su manifiesto (Priority: P1) 🎯 MVP

**Goal**: `just lock fnm && just build fnm` produce `dist/fnm_1.39.0-0~pav1_{amd64,arm64}.deb`
con metadatos completos, `copyright` DEP-5, changelog, gate de licencias y lintian sin errores
(overrides declarados para fnm arm64), de forma reproducible.

**Independent Test**: quickstart escenario 1 y `debfactory build fnm --check-reproducible`, con
`packages/fnm.yaml` sin completions.

- [X] T021 [US1] Implementar `internal/fetch/fetch.go`: `Fetcher{Client *http.Client, Cache string}` (el `Client` es inyectable: los tests le pasan un `Transport` que redirige `github.com` y `raw.githubusercontent.com` al `httptest.NewTLSServer`); `Get(ctx, url, expectedSHA) (path, sha, error)` que acepta solo `https`, descarga a un temporal dentro de `<cache>/tmp/` calculando sha256 al vuelo y solo hace `rename` a `<cache>/sha256/<hash>` si coincide con `expectedSHA`; con `expectedSHA` vacío (solo lo usa `lock`) devuelve el temporal **sin cachearlo**, y `Commit(tmp, sha)` lo mueve a la caché cuando `lock` ya lo verificó; si ya existe en caché, no descarga; una discrepancia devuelve el error tipado `*HashMismatch{URL, Expected, Got}`, que el pipeline informa en la etapa `verify`; timeout HTTP configurable
- [X] T022 [P] [US1] Implementar `internal/fetch/github.go`: `PublishedAt(repo, tag) (time.Time, error)` vía `GET /repos/{repo}/releases/tags/{tag}` (base de la API inyectable para tests; usa `GITHUB_TOKEN` si existe) y `LicenseURL(repo, tag, path)` → `https://raw.githubusercontent.com/<repo>/<tag>/<path>`
- [X] T023 [US1] Implementar `internal/assemble/extract.go` (+ test): extraer `zip`, `tar.gz` o `binary` (copia como `{{name}}`) en el workdir; rechazar entradas con `..`, rutas absolutas o symlinks que salgan del directorio; conservar el bit ejecutable de origen
- [X] T024 [US1] Implementar `internal/assemble/install.go` (+ test): resolver `install[]` sin globs (renderizar `src` con `{{arch}}`); `dst` terminado en `/` = directorio que conserva el nombre de origen, si no es la ruta completa (permite renombrar: el test incluye `src: tool-linux-amd64`, `dst: /usr/bin/tool`); `mode` explícito o, por defecto, **0755 si el archivo de origen tiene el bit ejecutable y 0644 si no**; producir entradas `BuildPlan{dst, origen, mode}` ordenadas por `dst`; un `dst` repetido tras resolver es error de etapa `assemble`
- [X] T025 [P] [US1] Implementar `internal/assemble/elfdeps.go` (+ test con `/usr/bin/ls` del host como ELF dinámico y un ELF estático de fixture): leer `NEEDED` y la versión `GLIBC_x.y` máxima con `debug/elf`; mapear con `config.elf_dependencies` reemplazando `{glibc}`; deduplicar; una biblioteca no mapeada es error de etapa `assemble` que nombra la biblioteca y el binario
- [X] T026 [P] [US1] Implementar `internal/pkg/copyright.go` (+ test): `copyright` DEP-5 (`Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/`, `Upstream-Name`, `Source: <homepage>`, `Files: *` con `Copyright:` tomado de la primera línea "Copyright" del texto, o de `license.copyright`, o del dueño de `upstream`; `License: <spdx>` con el texto completo indentado un espacio y las líneas vacías como ` .`; para GPL, LGPL, Apache-2.0 y MPL-2.0, en vez del texto, la referencia a `/usr/share/common-licenses/<archivo>`, porque lintian da error no silenciable si se copia: research D9)
- [X] T027 [P] [US1] Implementar `internal/pkg/changelog.go` (+ test): entrada `<name> (<upstream>-0~pav<N>) unstable; urgency=medium` / `  * Repackage upstream release <upstream> with debfactory.` / ` -- <maintainer>  <fecha RFC 2822 de SOURCE_DATE_EPOCH>`, comprimida con `gzip` nivel 9, cabecera sin nombre y `ModTime` = Unix 0 (equivalente a `gzip -9n`), en `/usr/share/doc/<name>/changelog.Debian.gz`
- [X] T028 [P] [US1] Implementar `internal/pkg/lintian_overrides.go` (+ test): generar `/usr/share/lintian/overrides/<name>` (modo 0644) con una línea `<name>: <tag> [<path sin la / inicial>]` por override (p. ej. `fnm: statically-linked-binary [usr/bin/fnm]`; con la `/` lintian da `mismatched-override` y no aplica el override, verificado con lintian 2.122) y el comentario `# <reason>` encima (fnm los necesita en arm64: `statically-linked-binary` y `unstripped-binary-or-object`, research D9)
- [X] T029 [US1] Implementar `internal/license/gate.go` (+ test con los textos reales de `testdata/licenses/`; los casos negativos van en US2) con este orden fijo, informando el **primer** fallo: (1) detectar con licensecheck; cobertura < 90 % → `license: texto de licencia no identificable (cobertura X % < 90 %; declarada "<declarada>")`; (2) el identificador detectado debe ser compatible con la expresión declarada (GPL-3.0 ↔ `GPL-3.0-only`/`GPL-3.0-or-later`, y lo mismo para GPL-2.0, LGPL-2.1 y LGPL-3.0), si no `license: licencia detectada "<detectada>" no coincide con la declarada "<declarada>"`; (3) cada término de la expresión **declarada** debe estar en la allowlist (vía T013), si no `license: licencia declarada "<término>" no está en config/licenses.yaml (detectada "<detectada>")`
- [X] T030 [US1] Implementar `internal/pkg/build.go`: `BuildPlan` → `nfpm.Info` (Name, Arch, `VersionSchema: "none"`, `Version: <upstream>`, `Release: "0~pav<N>"` con N = `Pav`, Maintainer de la config, Description = `summary` + `description`, Homepage, Section, Priority `optional`, Depends = ELF ∪ manifiesto, `MTime` = SOURCE_DATE_EPOCH, contenidos con `FileInfo{Mode, MTime, Owner: root, Group: root}`, copyright con modo 0644, overrides de T028 y changelog como archivo normal, **no** con la función de changelog de nfpm) → packager `deb` → escribe `<workdir>/out/<name>_<upstream>-0~pav<N>_<arch>.deb`
- [X] T031 [US1] Implementar `internal/lint/lintian.go`: imagen derivada `debfactory-lintian` creada con `container.Build` (`FROM <config.lintian.image>` + `apt-get install -y lintian`), ejecutar `lintian --fail-on <config.lintian.fail_on> --no-cfg` con el `.deb` montado en solo lectura y `--network none`; devolver las etiquetas E/W parseadas; si hay errores no cubiertos por overrides, falla en la etapa `lintian` listándolos
- [X] T032 [US1] Implementar `internal/build/lockcmd.go` (depende de T029): orden `lock <pkg> [--force]` para la versión del manifiesto (no hay `--version`): ejecutar `validate` del manifiesto; para amd64 y arm64, renderizar la URL y descargar con `fetch.Get` sin hash esperado (temporal sin cachear); descargar el texto de licencia (`license.file.repo` vía `LicenseURL`, o extraerlo del artefacto de amd64 si es `license.file.archive`); pasar el gate de T029 sobre ese texto; obtener `PublishedAt`; hacer `Commit` a la caché de lo verificado; si ya existe una entrada para esa versión con algún hash distinto, fallar con `lock: los hashes de <v> cambiaron respecto al lockfile (usa --force si el cambio es legítimo)` salvo con `--force`; escribir la entrada conservando las versiones anteriores
- [X] T033 [US1] Implementar `internal/build/pipeline.go`: `Preflight()` → `validate → lock` (si falta la entrada de la versión: `lock: no hay entrada para <v> en packages/<pkg>.lock.yaml; ejecuta \`debfactory lock <pkg>\``) `→ fetch` (con los hashes del lock; `*HashMismatch` se informa como `verify: sha256 no coincide para <asset>` con esperado y obtenido) `→ license` (texto de `license.file.repo` desde la caché, o, si es `license.file.archive`, extraer **solo ese archivo** del artefacto de la arquitectura que se construye; su sha256 debe ser `license_sha256`, que es único porque el texto es idéntico en ambas arquitecturas, y si difiere es error `verify`; luego el gate de T029) `→ assemble` (incluye resolver el `test.command` por defecto: `<basename del primer archivo instalado bajo /usr/bin> --version`, que se guarda en `build.json`) `→ package → lintian` (imprime `lintian: pass`) `→` copiar a `dist/.tmp-<archivo>` y hacer `rename` dentro de `dist/` (el workdir está en tmpfs y `rename` entre sistemas de archivos falla con EXDEV), igual para `dist/<name>_<upstream>-0~pav<N>_<arch>.build.json` `{sha256, manifest_sha256, lock_sha256, lintian, overrides, test_command}`; borrar el workdir siempre; `--arch` o amd64 y arm64; `--check-reproducible` construye dos veces en workdirs distintos, compara sha256 e imprime `reproducible: <sha256>` por `.deb` (o falla)
- [X] T034 [US1] Conectar `lock`, `build` y `lint <deb…>` en `cmd/debfactory/main.go` (`build` y `lint` llaman a `Preflight()`)
- [X] T035 [US1] Crear `packages/fnm.yaml` a partir de `specs/001-manifest-deb-builder/contracts/examples/fnm.yaml` **sin** el bloque `completions` (se añade en US5) y **con** sus `lintian_overrides`, ejecutar `debfactory lock fnm` y versionar `packages/fnm.lock.yaml` tras revisar los hashes
- [X] T036 [US1] Test de reproducibilidad en `internal/build/pipeline_test.go`: crear en `testdata/fixtures/ok/` un `tar.gz` con un binario estático, un `LICENSE` MIT y su manifiesto (con el override `statically-linked-binary` y su `path`, como `contracts/examples/example.yaml`) y su lock; servirlos con `httptest.NewTLSServer` a través del `Transport` inyectado de T021; construir dos veces y comparar sha256 (SC-006); comprobar con `dpkg-deb -f` Package, Version (`<upstream>-0~pav1`), Maintainer, Homepage, Architecture y Description

**Checkpoint**: quickstart escenario 1 y `debfactory build fnm --check-reproducible` pasan en
amd64 y arm64.

---

## Phase 4: User Story 2 - Rechazar entradas inválidas o inseguras antes de construir (Priority: P1)

**Goal**: verificación contra checksums upstream, redirecciones controladas y casos negativos
con mensajes exactos para cada caso de SC-005 (el gate de licencias ya existe desde US1).

**Independent Test**: quickstart escenario 5; `go test ./internal/build -run Negative`.

- [X] T037 [P] [US2] Crear fixtures en `testdata/negative/`: `missing-license-file.yaml` (sin `license.file`), `foreign-url.yaml` (`artifact.url` de otro repo), `unknown-var.yaml` (`{{arc}}`), `licenses/SSPL-1.0.txt` con un manifiesto que declara `SSPL-1.0` (licensecheck no tiene patrón para BUSL-1.1; research §3 riesgo 4), `licenses/gibberish.txt`, `licenses/MIT-declared-Apache.txt` (texto MIT) con un manifiesto que declara `Apache-2.0`, un `lock` con un hash de artefacto alterado y un `sha256.txt` de upstream que no coincide con el artefacto
- [X] T038 [US2] Implementar `internal/fetch/checksums.go` (+ test): descargar `checksums.url` renderizada con `fetch.Get` usando `checksums_sha256` del lock como hash esperado (así `build` lo lee de la caché), parsear el formato `sha256sum` (`<hex>  <archivo>` y `<hex> *<archivo>`), buscar por el nombre base del asset (si falta, error); en caso de discrepancia, error `verify: sha256 no coincide con <archivo de checksums> de upstream para <asset>` con esperado y obtenido
- [X] T039 [US2] Restringir redirecciones en el cliente HTTP de `internal/fetch/fetch.go`: para artefactos y archivos de checksums, solo `https` hacia `github.com`, `objects.githubusercontent.com` y `release-assets.githubusercontent.com`; para el texto de licencia, además `raw.githubusercontent.com`; cualquier otro destino es error de etapa `fetch` que nombra el host
- [X] T040 [US2] Integrar los checksums upstream en `internal/build/lockcmd.go` con este orden: descargar el archivo de checksums → verificar los artefactos contra él → solo entonces el gate de licencias (el texto de `license.file.archive` sale de un artefacto ya verificado; constitución, Flujo) → escribir `checksums_sha256` con el resto de la entrada y en `internal/build/pipeline.go` (verificar lock **y** checksums upstream en la etapa `verify`, antes de `license`)
- [X] T041 [US2] Tests negativos en `internal/build/negative_test.go` con `httptest` (Transport de T021) y los fixtures de T037: manifiesto inválido, hash del lock alterado, checksum upstream que no coincide, licencia declarada no permitida (SSPL-1.0), licencia no identificable, licencia detectada distinta de la declarada y URL no oficial (además, el servidor de prueba no recibe ninguna petición); verificar el mensaje exacto de `contracts/cli.md`, código de salida 1 y que `dist/` queda sin archivos nuevos (SC-005)

**Checkpoint**: los 7 casos negativos fallan con mensajes exactos; fnm sigue construyéndose.

---

## Phase 5: User Story 3 - Probar la instalación real en cada suite (Priority: P2)

**Goal**: `just test <pkg>` comprueba el orden de versiones, instala, ejecuta, verifica la
versión y purga en las 5 suites × 2 arquitecturas, y produce un informe.

**Independent Test**: quickstart escenario 4 con fnm (y con bob cuando exista en US5).

- [X] T042 [US3] Implementar `internal/installtest/images.go`: imágenes locales `debfactory-test/<suite>:<arch>` creadas con `container.Build` desde `config.suites.<s>.image` con `--platform linux/<arch>` y `apt-get update` incluido; se reconstruyen si su fecha `Created` tiene más de 24 h
- [X] T043 [P] [US3] Implementar `internal/installtest/versions.go` (+ test): con `dpkg --compare-versions` del host, comprobar que `<upstream>-0~pav<N>` `lt` `<upstream>-1` y, si `N > 1`, `gt` `<upstream>-0~pav<N-1>` (FR-024); el resultado va al campo `versions` del informe (no a `results[]`)
- [X] T044 [US3] Implementar `internal/installtest/run.go`: por cada suite no excluida y la arquitectura del `.deb`, `container.Start` con `dist/` montado en solo lectura; `apt-get install -y ./<deb>` vía `Exec` (stage `install`); capturar `dpkg -L <pkg>`; `DisconnectNetwork` (sin red); crear `/tmp/debfactory-home` con `Exec mkdir -p` y ejecutar el `test_command` de `build.json` separado en argumentos (sin shell) con `HOME=/tmp/debfactory-home` y `PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` literales (stage `run`); la salida debe contener `test.expect` renderizado (stage `version`); `apt-get purge -y <pkg>` (stage `purge`); ningún **archivo** de la lista de `dpkg -L` sigue existiendo (stage `leftovers`); siempre `Remove`
- [X] T045 [P] [US3] Implementar `internal/installtest/report.go`: escribir `dist/<name>_<upstream>-0~pav<N>_<arch>.test.json` con `versions: pass|fail`, `results[].{suite, status: pass|fail|excluded, stage, reason}` (stages `install`, `run`, `version`, `purge`, `leftovers`) y `overall` (`pass` si `versions` es `pass` y ningún resultado es `fail`), más una tabla legible en stdout
- [X] T046 [US3] Conectar `test <pkg> [--arch A] [--suite S]` en `cmd/debfactory/main.go`: `Preflight()`; ejecutar primero T043 (si falla, `overall = fail` sin arrancar contenedores); las suites excluidas se informan como `excluded: <motivo>` sin ejecutarse; el código de salida es 1 si `overall = fail`; añadir `test --all` (cada paquete de `packages/*.yaml`, sigue si uno falla, resumen final)
- [X] T047 [US3] Test de integración en `internal/installtest/run_test.go` (etiqueta de build `integration`, requiere Docker): con el `.deb` del fixture de T036 y un `test.expect` incorrecto, comprobar `overall: fail`, la suite, el stage `version` y código de salida 1 (US3 escenario 3)
- [X] T048 [US3] Ejecutar `just test fnm` (5 suites × 2 arch) y anotar en `specs/001-manifest-deb-builder/research.md` §3 el tiempo total y cualquier ajuste necesario

**Checkpoint**: fnm da `pass` en las 10 combinaciones; un fallo forzado da `overall: fail`
indicando la etapa.

---

## Phase 6: User Story 4 - Agregar o actualizar una herramienta editando solo datos (Priority: P2)

**Goal**: añadir o actualizar paquetes sin tocar código; construir todos de una vez.

**Independent Test**: quickstart escenarios 6 y 7.

- [X] T049 [US4] Implementar `build --all [--check-reproducible]` en `internal/build/pipeline.go` y `cmd/debfactory/main.go` (pasa `--check-reproducible` a cada paquete): recorre `packages/*.yaml` en orden alfabético, sigue con el siguiente si uno falla, imprime un resumen (`ok`/`fail` con la etapa) y sale con 1 si alguno falló
- [X] T050 [US4] Añadir una regla en `internal/manifest/semantic.go` (lee el lockfile con T015): si `version` no tiene entrada en el lockfile y el lockfile tiene otras versiones, `pav` MUST ser `1` (error `pav: debe ser 1 para una versión upstream nueva`); como `lock` valida antes de escribir (T032), la regla se aplica antes de crear la entrada nueva
- [X] T051 [US4] Añadir `lock <pkg> --prune` en `internal/build/lockcmd.go`: borra del lockfile todas las versiones salvo la del manifiesto
- [X] T052 [P] [US4] Test en `internal/manifest/examples_test.go`: todos los `specs/001-manifest-deb-builder/contracts/examples/*.yaml` (salvo `*.lock.yaml`) pasan `validate`, y `example.yaml` tiene ≤ 25 líneas (SC-003)

**Checkpoint**: escenarios 6 y 7 del quickstart pasan; `git status` muestra solo el manifiesto
y su lockfile.

---

## Phase 7: User Story 5 - Expresar paquetes no triviales con el mismo esquema (Priority: P3)

**Goal**: globs, man pages, completions (de archivo y generadas) y relaciones; los 4 paquetes
reales construidos y probados.

**Independent Test**: `just build-all && just test <cada paquete>`; cada `.deb` contiene
exactamente los archivos declarados (`dpkg-deb -c`).

- [X] T053 [US5] Añadir globs a `internal/assemble/install.go`: `src` con `*`, `?` y `[...]` resuelto con `filepath.Glob` en el directorio extraído; cero coincidencias → error `assemble: install[i].src: el patrón "<p>" no seleccionó ningún archivo`; con varias coincidencias, `dst` debe terminar en `/`; después de resolver globs, man pages y completions, un `dst` repetido es error de etapa `assemble`
- [X] T054 [P] [US5] Implementar `internal/assemble/man.go` (+ test): resolver los globs de `man`, tomar la sección de la extensión (`.1` → `man1`), comprimir con gzip nivel 9 sin nombre y con ModTime 0, e instalar en `/usr/share/man/man<N>/<archivo>.gz` con modo 0644
- [X] T055 [US5] Implementar `internal/assemble/completions.go` para `completions.<shell>.file`: copiar a `/usr/share/bash-completion/completions/<name>`, `/usr/share/zsh/vendor-completions/_<name>` y `/usr/share/fish/vendor_completions.d/<name>.fish` con modo 0644
- [X] T056 [US5] Añadir `completions.<shell>.generate` a `internal/assemble/completions.go`: ejecutar con `container.Run` en `config.completions_image` con `--network none`, `--read-only`, `--platform linux/<arch>` (o solo amd64, según T020), el árbol preparado montado en `/stage` en solo lectura, la orden separada en argumentos (sin shell) y `PATH=/stage/usr/bin:/usr/sbin:/usr/bin:/sbin:/bin` literal; stdout vacío o código ≠ 0 → error `assemble` con stderr recortado
- [X] T057 [P] [US5] Pasar `depends`, `recommends`, `conflicts`, `replaces` y `provides` del manifiesto a `nfpm.Overridables` en `internal/pkg/build.go`, fusionando `depends` con el resultado de `elfdeps` (si el manifiesto declara el mismo paquete, gana el del manifiesto)
- [X] T058 [P] [US5] Crear `packages/bob.yaml` desde `contracts/examples/bob.yaml` (con `exclude.suites.bookworm`), `debfactory lock bob` y versionar el lockfile
- [X] T059 [P] [US5] Crear `packages/buf.yaml` desde `contracts/examples/buf.yaml` (3 ejecutables por glob, completions de archivo, 113 man pages, override `statically-linked-binary`, checksums upstream), `debfactory lock buf` y versionar el lockfile
- [X] T060 [P] [US5] Crear `packages/aqua.yaml` desde `contracts/examples/aqua.yaml` (checksums upstream, override `statically-linked-binary`), `debfactory lock aqua` y versionar el lockfile
- [X] T061 [US5] Añadir a `packages/fnm.yaml` el bloque `completions` de `contracts/examples/fnm.yaml` (sin cambiar `pav`, porque aún no se ha publicado nada)
- [X] T062 [US5] Ejecutar `just build-all` y `just test` de los 4 paquetes; comprobar con `dpkg-deb -c` que buf contiene `buf`, `protoc-gen-buf-breaking`, `protoc-gen-buf-lint`, 3 completions y 113 man pages `.gz` (SC-001, SC-002)

**Checkpoint**: 8 `.deb` construidos; las pruebas dan `pass` salvo bob en bookworm
(`excluded`).

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T063 [P] Escribir `README.md`: un párrafo por componente (manifiesto, lockfile, config, caché, lintian, pruebas) y la orden de cada operación (constitución IX), con enlaces a `specs/001-manifest-deb-builder/quickstart.md` y `contracts/`
- [X] T064 Completar la receta `check` del `justfile`: `gofmt -l .` (vacío), `go vet ./...`, `go test ./...`, `debfactory validate`, `debfactory build --all --check-reproducible` y `debfactory test --all`; imprimir al final la duración total de `just check` (objetivo orientativo del plan: < 30 min)
- [X] T065 Recorrer los escenarios 1–7 de `specs/001-manifest-deb-builder/quickstart.md` (el escenario 6 con un quinto paquete real que cumpla la constitución V, que se borra al terminar) y corregir cualquier diferencia entre la guía y el comportamiento real
- [X] T066 Actualizar `specs/001-manifest-deb-builder/research.md` §3 con el resultado final de los riesgos y repetir el Constitution Check de `plan.md` con lo implementado
- [X] T067 Ampliar `internal/manifest/examples_test.go` para validar también todos los `packages/*.yaml` (después de T058–T061)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: sin dependencias.
- **Foundational (Phase 2)**: depende de Setup; bloquea todas las historias. T018 (nfpm) debe
  estar resuelto antes de T030; T019 (licensecheck) antes de T029; T020 (qemu) antes de T056.
- **US1 (Phase 3)**: depende de Foundational. Es el MVP. Dentro de US1, T032 (lock) depende de
  T029 (gate).
- **US2 (Phase 4)**: depende de US1 (amplía `fetch`, `lockcmd` y `pipeline`).
- **US3 (Phase 5)**: depende de US1 (necesita `.deb` en `dist/`); independiente de US2.
- **US4 (Phase 6)**: depende de US1.
- **US5 (Phase 7)**: depende de US1; T059 y T060 dependen de US2 (checksums upstream) y T062 de
  US3 (pruebas) y de US4 (T049, `build --all`).
- **Polish (Phase 8)**: al final; T067 después de T058–T061.

### User Story Dependencies

```text
Setup → Foundational → US1 ─┬─ US2 ─────────┐
                            ├─ US3 ─────────┼─ US5 (T059, T060, T062) → Polish
                            └─ US4 ─────────┘   (T062 también necesita US4)
```

### Within Each User Story

- Los spikes (T018, T019, T020) van antes de las tareas que dependen de su resultado.
- `internal/*` antes de conectar en `cmd/debfactory/main.go`.
- Los manifiestos reales (`packages/*.yaml`) después de que el pipeline soporte sus
  capacidades.

### Parallel Opportunities

- Setup: T002–T006 en paralelo.
- Foundational: T009, T010, T015, T016 y T019 en paralelo; T011 → T012 → T013 → T014 en
  secuencia; T020 después de T019 (los dos anotan en research.md).
- US1: T022, T025, T026, T027 y T028 en paralelo tras T021/T023/T024.
- US3: T043 y T045 en paralelo.
- US5: T054 y T057 en paralelo; T058–T060 en paralelo (un manifiesto por archivo).

---

## Parallel Example: User Story 1

```bash
# Tras T021 (fetch), T023 (extract) y T024 (install):
Task: "T022 internal/fetch/github.go — PublishedAt y LicenseURL"
Task: "T025 internal/assemble/elfdeps.go — Depends desde ELF"
Task: "T026 internal/pkg/copyright.go — copyright DEP-5"
Task: "T027 internal/pkg/changelog.go — changelog.Debian.gz reproducible"
Task: "T028 internal/pkg/lintian_overrides.go — overrides declarados"
```

## Parallel Example: User Story 5

```bash
# Tras T053 (globs) y US2:
Task: "T058 packages/bob.yaml + lock"
Task: "T059 packages/buf.yaml + lock"
Task: "T060 packages/aqua.yaml + lock"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 + Phase 2 (incluidos los spikes T018, T019 y T020).
2. Phase 3: fnm construido para amd64 y arm64, con gate de licencias, lintian sin errores y
   reproducible.
3. **STOP and VALIDATE**: quickstart escenario 1 y `debfactory build fnm --check-reproducible`.

### Incremental Delivery

1. MVP (US1) → fnm construible.
2. US2 → casos negativos y checksums upstream (requisito constitucional; antes de tener más
   paquetes).
3. US3 → pruebas de instalación (gate de publicación futuro).
4. US4 → `build --all` y flujo de actualización.
5. US5 → bob, buf y aqua y las capacidades avanzadas del esquema.

Una sola persona: seguir el orden de prioridad; hacer commit al cerrar cada checkpoint.

---

## Notes

- [P] = archivos distintos y sin dependencias pendientes.
- Ningún binario upstream se ejecuta en el host: solo en contenedores (T020, T031, T044, T056).
- `build` nunca escribe el lockfile; solo `lock` (T032, T040, T051).
- Ningún `.deb` llega a `dist/` sin haber pasado el gate de licencias y lintian (T033).
