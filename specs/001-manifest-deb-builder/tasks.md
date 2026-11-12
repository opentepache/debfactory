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

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: esqueleto del módulo, configuración compartida y atajos.

- [ ] T001 Crear `go.mod` con `module debfactory` y `go 1.26`, y los directorios `cmd/debfactory/`, `internal/{config,manifest,lock,fetch,license,assemble,container,pkg,lint,installtest,report,build}/`, `schema/`, `config/`, `packages/`, `testdata/{manifests/valid,manifests/invalid,licenses,negative}/`
- [ ] T002 [P] Crear `.gitignore` con `dist/` y `*.deb`
- [ ] T003 [P] Crear `config/debfactory.yaml` copiando literalmente el bloque YAML de `specs/001-manifest-deb-builder/contracts/config.md` (maintainer, arches, suites con `image` y `level2_suffix`, `lintian`, `elf_dependencies`, `community_repos`, `completions_image`)
- [ ] T004 [P] Crear `config/licenses.yaml` copiando literalmente la allowlist de `specs/001-manifest-deb-builder/contracts/config.md`
- [ ] T005 [P] Copiar `specs/001-manifest-deb-builder/contracts/manifest.v1.schema.json` a `schema/manifest.v1.schema.json` y crear `schema/embed.go` (package `schema`) que lo exponga con `//go:embed` como `ManifestV1 []byte`
- [ ] T006 [P] Crear `justfile` con `export GOTOOLCHAIN := "local"`, `PATH` con `/usr/lib/go-1.26/bin` y las recetas de `contracts/cli.md` (`build pkg arch=""`, `build-all`, `lint pkg`, `test pkg arch=""`, `lock pkg`, `validate`, `check`), todas llamando a `go run ./cmd/debfactory` (o al binario en `bin/` si existe)
- [ ] T007 Añadir dependencias a `go.mod`/`go.sum`: `github.com/goreleaser/nfpm/v2` (última v2.48.x; **no** v2.49, que exige Go 1.27.4), `github.com/santhosh-tekuri/jsonschema/v6@v6.0.3`, `github.com/google/licensecheck`, `gopkg.in/yaml.v3`; verificar que `go build ./...` funciona con Go 1.26.10

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: errores, config, carga y validación de manifiestos, lockfile, contenedores y los dos
spikes de riesgo que condicionan el diseño (research.md §3).

**⚠️ CRITICAL**: ninguna historia empieza hasta terminar esta fase.

- [ ] T008 Implementar `internal/report/errors.go`: tipo `Error{Package, Version, Arch, Stage, Cause, Detail}`, constantes de etapa `validate, lock, fetch, verify, license, assemble, package, lintian, test`; `Error()` con el formato exacto de `contracts/cli.md` (`debfactory: <pkg> <version> <arch>: <stage>: <causa>`, `-` si el campo está vacío, detalle indentado con dos espacios) y `MarshalJSON` para `--json`
- [ ] T009 [P] Implementar `internal/config/config.go` (+ `config_test.go`): cargar y validar `config/debfactory.yaml` y `config/licenses.yaml` (campos de `contracts/config.md`; `schema: 1`; suites con `image` no vacío; `elf_dependencies` cuyos valores solo usan el marcador `{glibc}`); error tipado con etapa `validate`
- [ ] T010 [P] Definir structs en `internal/manifest/types.go` que reflejen 1:1 `schema/manifest.v1.schema.json` (Manifest, License, LicenseFile{Archive,Repo}, Artifact, Checksums, InstallItem{Src,Dst,Type,Mode}, Link, Completion{File,Generate}, Exclude, Test, LintianOverride)
- [ ] T011 Implementar `internal/manifest/load.go` (+ `load_test.go`): leer `packages/<name>.yaml` con yaml.v3 a `any`, normalizar claves a `map[string]any`, validar con santhosh-tekuri/jsonschema v6 contra `schema.ManifestV1`, traducir cada error a `packages/<f>.yaml: <campo.con.puntos>: <mensaje>`, decodificar en structs y aplicar los defaults de data-model.md: `tag: v{{version}}`, `section: devel`, `mode` 0755 bajo `/usr/bin` y `/usr/sbin`, 0644 para lo demás, `test.command: <basename del primer destino en /usr/bin> --version`, `test.expect: {{version}}`
- [ ] T012 Implementar `internal/manifest/template.go` (+ `template_test.go`): `Render(s string, vars Vars)` con sustitución literal de `{{version}}`, `{{tag}}` y `{{arch}}` (y `{{name}}` solo en `install.src` con `format: binary`); cualquier otra `{{…}}` es error `variable desconocida {{x}}`
- [ ] T013 Implementar `internal/manifest/semantic.go` (+ `semantic_test.go` con un fixture por regla en `testdata/manifests/invalid/`) con las reglas de data-model.md: `name` coincide con el archivo; solo variables conocidas; `artifact.url` y `checksums.url` empiezan por `https://github.com/<upstream>/releases/download/<tag renderizado>/`; `summary` sin artículo inicial (a/an/the); claves de `exclude.suites` y `exclude.arches` existen en la config; arquitecturas de `arch_map` ∖ `exclude.arches` = arquitecturas a construir (falta = error); `license.spdx` es una expresión SPDX válida; `lintian_overrides[].tag` no está en `lintian.never_override`; destinos únicos (incluidos enlaces y archivos generados); con `format: binary`, `install` tiene exactamente un elemento
- [ ] T014 [P] Implementar `internal/lock/lock.go` (+ `lock_test.go`): leer y escribir `packages/<name>.lock.yaml` con `schema: 1` y `versions.<ver>.{source_date_epoch:int, license_sha256:hex64, artifacts.<arch>:hex64}`; salida determinista (claves ordenadas) y comentario de cabecera "Generado por `debfactory lock`; no se edita a mano"
- [ ] T015 [P] Implementar `internal/container/docker.go`: `Run(ctx, Opts{Image, Platform, Network ("none"|"bridge"), ReadOnly, Mounts, Env, Workdir, Cmd}) (stdout, stderr, exit)` sobre `docker` con `os/exec`, y `Preflight()` que comprueba `docker info` y que existe `/proc/sys/fs/binfmt_misc/qemu-aarch64` (si falta, error de entorno con código de salida 3)
- [ ] T016 Implementar `cmd/debfactory/main.go`: subórdenes `validate|lock|build|lint|test`, flags globales `--root`, `--workdir` (por defecto `$XDG_RUNTIME_DIR/debfactory`, si no `os.TempDir()`), `--cache` (por defecto `~/.cache/debfactory`), `--json`; códigos de salida 0/1/2/3 de `contracts/cli.md`; implementar ya `validate [pkg…]` (config + load + semantic)
- [ ] T017 Spike de reproducibilidad de nfpm en `internal/pkg/spike_test.go`: construir dos veces un `.deb` trivial con nfpm (`VersionSchema: "none"`, `Version: "1.0.0"`, `Release: "0~pav1"`, `MTime` fijo, un archivo 0755 y otro 0644, owner root) en directorios distintos y comparar sha256; comprobar con `dpkg-deb -f` que Version = `1.0.0-0~pav1`. Anotar el resultado (y la corrección si no es determinista, p. ej. en `control.tar.gz`) en `specs/001-manifest-deb-builder/research.md` §3 riesgo 1
- [ ] T018 [P] Spike del detector de licencias en `internal/license/detect_test.go`: guardar en `testdata/licenses/` los textos reales al tag exacto (`fnm-v1.39.0-LICENSE`, `pnpm-v12.18.0-LICENSE`, `bob-v4.3.0-LICENSE`, `buf-v1.75.0-LICENSE`, `aqua-v2.66.1-LICENSE`) y comprobar que `google/licensecheck` los identifica como GPL-3.0 / MIT / MIT / Apache-2.0 / MIT con cobertura ≥ 90 %; anotar el resultado en research.md §3 riesgo 4

**Checkpoint**: `go test ./...` pasa, `debfactory validate` funciona y los dos spikes están
resueltos y documentados.

---

## Phase 3: User Story 1 - Construir un paquete a partir de su manifiesto (Priority: P1) 🎯 MVP

**Goal**: `just lock fnm && just build fnm` produce `dist/fnm_1.39.0-0~pav1_{amd64,arm64}.deb`
con metadatos completos, `copyright` DEP-5, changelog y lintian sin errores, de forma
reproducible.

**Independent Test**: quickstart.md, escenarios 1 y 3, con `packages/fnm.yaml` sin
completions.

- [ ] T019 [US1] Implementar `internal/fetch/fetch.go`: `Get(ctx, url, expectedSHA string) (path, sha, error)` que acepta solo `https`, descarga a un temporal dentro de `<cache>/tmp/` calculando sha256 al vuelo, compara con `expectedSHA` (si no está vacío) y solo entonces hace `rename` a `<cache>/sha256/<hash>`; si ya existe en caché, no descarga; timeout HTTP configurable
- [ ] T020 [P] [US1] Implementar `internal/fetch/github.go`: `PublishedAt(repo, tag) (time.Time, error)` vía `GET /repos/{repo}/releases/tags/{tag}` (usa `GITHUB_TOKEN` si existe) y `LicenseURL(repo, tag, path)` → `https://raw.githubusercontent.com/<repo>/<tag>/<path>`
- [ ] T021 [US1] Implementar `internal/assemble/extract.go` (+ test): extraer `zip`, `tar.gz` o `binary` (copia como `{{name}}`) en el workdir; rechazar entradas con `..`, rutas absolutas o symlinks que salgan del directorio; conservar el bit ejecutable de origen
- [ ] T022 [US1] Implementar `internal/assemble/install.go` (+ test): resolver `install[]` de tipo `file` sin globs (renderizar `src` con `{{arch}}`); `dst` terminado en `/` = directorio que conserva el nombre de origen; modos por defecto de T011; producir entradas `BuildPlan{dst, origen, mode, tipo}` ordenadas por `dst`
- [ ] T023 [P] [US1] Implementar `internal/assemble/elfdeps.go` (+ test con `/usr/bin/ls` del host como ELF dinámico y un ELF estático de fixture): leer `NEEDED` y la versión `GLIBC_x.y` máxima con `debug/elf`; mapear con `config.elf_dependencies` reemplazando `{glibc}`; deduplicar; una biblioteca no mapeada es error de etapa `assemble` que nombra la biblioteca y el binario
- [ ] T024 [P] [US1] Implementar `internal/pkg/copyright.go` (+ test): `copyright` DEP-5 (`Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/`, `Upstream-Name`, `Source: <homepage>`, `Files: *` con `Copyright:` tomado de la primera línea "Copyright" del texto, o de `license.copyright`, o del dueño de `upstream`; `License: <spdx>` con el texto completo indentado un espacio y las líneas vacías como ` .`)
- [ ] T025 [P] [US1] Implementar `internal/pkg/changelog.go` (+ test): entrada `<name> (<ver>) unstable; urgency=medium` / `  * Repackage upstream release <version> with debfactory.` / ` -- <maintainer>  <fecha RFC 2822 de SOURCE_DATE_EPOCH>`, comprimida con `gzip` nivel 9, cabecera sin nombre y `ModTime` = Unix 0 (equivalente a `gzip -9n`), en `/usr/share/doc/<name>/changelog.Debian.gz`
- [ ] T026 [US1] Implementar `internal/pkg/build.go`: `BuildPlan` → `nfpm.Info` (Name, Arch, `VersionSchema: "none"`, `Version: <upstream>`, `Release: "0~pav<N>"`, Maintainer de la config, Description = `summary` + `description`, Homepage, Section, Priority `optional`, Depends = ELF ∪ manifiesto, `MTime` = SOURCE_DATE_EPOCH, contenidos con `FileInfo{Mode, MTime, Owner: root, Group: root}`, copyright con modo 0644 y changelog como archivo normal, **no** con la función de changelog de nfpm) → packager `deb` → escribe `<workdir>/out/<name>_<v>_<arch>.deb`
- [ ] T027 [US1] Implementar `internal/lint/lintian.go`: imagen derivada `debfactory-lintian` (`config.lintian.image` + `apt-get install lintian`), ejecutar `lintian --fail-on error --no-cfg` con el `.deb` montado en solo lectura y `--network none`; devolver las etiquetas E/W parseadas; si hay errores, falla en la etapa `lintian` listándolas
- [ ] T028 [US1] Implementar `internal/build/lockcmd.go`: orden `lock <pkg> [--version V]`: para cada arquitectura no excluida, renderizar la URL y descargar con `fetch.Get` sin hash esperado; descargar el texto de licencia (`license.file.repo` vía `LicenseURL`, o extraerlo del artefacto de amd64 si es `license.file.archive`); obtener `PublishedAt`; escribir la entrada en el lockfile conservando las versiones anteriores
- [ ] T029 [US1] Implementar `internal/build/pipeline.go`: `validate → lock` (si falta la entrada de la versión, error `lock: no hay entrada para <v> en packages/<pkg>.lock.yaml; ejecuta \`debfactory lock <pkg>\``) `→ fetch` (con el hash del lock) `→ license` (texto con sha256 = `license_sha256`) `→ assemble → package → lintian →` mover con `rename` a `dist/` más `dist/<name>_<v>_<arch>.build.json` `{sha256, manifest_sha256, lock_sha256, lintian, overrides}`; borrar el workdir siempre; `--arch` o todas las no excluidas; `--check-reproducible` construye dos veces en workdirs distintos y compara sha256
- [ ] T030 [US1] Conectar `lock`, `build` y `lint <deb…>` en `cmd/debfactory/main.go`
- [ ] T031 [US1] Crear `packages/fnm.yaml` a partir de `specs/001-manifest-deb-builder/contracts/examples/fnm.yaml` **sin** el bloque `completions` (se añade en US5), ejecutar `debfactory lock fnm` y versionar `packages/fnm.lock.yaml` tras revisar los hashes
- [ ] T032 [US1] Test de reproducibilidad en `internal/build/pipeline_test.go`: servir con `httptest` un `tar.gz` de fixture (`testdata/negative/ok/`) con un binario estático, construir dos veces y comparar sha256 (SC-006); comprobar con `dpkg-deb -f` Package, Version (`<v>-0~pav1`), Maintainer, Homepage y Architecture

**Checkpoint**: quickstart escenarios 1 y 3 pasan con fnm en amd64 y arm64.

---

## Phase 4: User Story 2 - Rechazar entradas inválidas o inseguras antes de construir (Priority: P1)

**Goal**: gate de licencias completo (falla cerrado), verificación contra checksums upstream,
redirecciones controladas y mensajes exactos para cada caso de SC-005.

**Independent Test**: quickstart escenario 5; `go test ./internal/build -run Negative`.

- [ ] T033 [P] [US2] Crear fixtures en `testdata/negative/`: `missing-license-file.yaml` (sin `license.file`), `foreign-url.yaml` (`artifact.url` de otro repo), `unknown-var.yaml` (`{{arc}}`), `licenses/BUSL-1.1.txt`, `licenses/gibberish.txt`, un `tar.gz` válido con su `lock` correcto y otro `lock` con un hash alterado, y un `sha256.txt` de upstream que no coincide
- [ ] T034 [US2] Implementar `internal/fetch/checksums.go` (+ test): descargar `checksums.url` renderizada, parsear el formato `sha256sum` (`<hex>  <archivo>` y `<hex> *<archivo>`), buscar por el nombre base del asset (si falta, error); en caso de discrepancia, error `verify: sha256 no coincide con <archivo de checksums> de upstream para <asset>` con esperado y obtenido
- [ ] T035 [US2] Restringir redirecciones en el cliente HTTP de `internal/fetch/fetch.go`: solo `https` hacia `github.com`, `objects.githubusercontent.com`, `release-assets.githubusercontent.com` y `raw.githubusercontent.com`; cualquier otro destino es error de etapa `fetch` que nombra el host
- [ ] T036 [US2] Implementar `internal/license/spdx.go` (+ test): parser mínimo de expresiones SPDX (identificadores, `OR`, `AND`, `WITH`, paréntesis) y evaluación contra la allowlist (`OR`: basta una alternativa; `AND`: todas)
- [ ] T037 [US2] Implementar `internal/license/gate.go` (+ test con `testdata/licenses/`): detectar con licensecheck; cobertura < 90 % → `license: texto de licencia no identificable (cobertura X % < 90 %)`; identificador detectado compatible con el declarado (GPL-3.0 ↔ `GPL-3.0-only`/`GPL-3.0-or-later`, y lo mismo para GPL-2.0 y LGPL); cada término en la allowlist, si no `license: licencia detectada "<id>" no está en config/licenses.yaml`; distinta de la declarada → error con detectada y esperada
- [ ] T038 [US2] Integrar en `internal/build/pipeline.go` y `internal/build/lockcmd.go`: `lock` verifica contra los checksums upstream y pasa el gate de licencias **antes** de escribir; `build` verifica lock **y** checksums upstream (stage `verify`) y vuelve a pasar el gate (stage `license`) antes de `assemble`
- [ ] T039 [US2] Tests negativos en `internal/build/negative_test.go` con `httptest` y los fixtures de T033: manifiesto inválido, checksum alterado (lock y upstream), licencia no permitida, licencia no identificable y URL no oficial; verificar el mensaje exacto de `contracts/cli.md`, código de salida 1 y que `dist/` queda sin archivos nuevos (SC-005)

**Checkpoint**: los 5 casos negativos fallan con mensajes exactos; fnm sigue construyéndose.

---

## Phase 5: User Story 3 - Probar la instalación real en cada suite (Priority: P2)

**Goal**: `just test <pkg>` instala, ejecuta, verifica la versión y purga en las 5 suites × 2
arquitecturas, y produce un informe.

**Independent Test**: quickstart escenario 4 con fnm (y con bob cuando exista en US5).

- [ ] T040 [US3] Implementar `internal/installtest/images.go`: imágenes locales `debfactory-test/<suite>:<arch>` construidas desde `config.suites.<s>.image` con `--platform linux/<arch>` y `apt-get update` incluido; se reconstruyen si su fecha `Created` tiene más de 24 h
- [ ] T041 [US3] Implementar `internal/installtest/run.go`: por suite y arquitectura no excluidas, contenedor desacoplado (`sleep infinity`) con `dist/` montado en solo lectura; `apt-get install -y ./<deb>` (stage `install`); capturar `dpkg -L <pkg>`; `docker network disconnect` (sin red); ejecutar `test.command` con `HOME=$(mktemp -d)` (stage `run`); la salida debe contener `test.expect` renderizado (stage `version`); `apt-get purge -y <pkg>` (stage `purge`); ningún **archivo** de la lista de `dpkg -L` sigue existiendo (stage `leftovers`); siempre se elimina el contenedor
- [ ] T042 [P] [US3] Implementar `internal/installtest/versions.go` (+ test): con `dpkg --compare-versions` del host, comprobar que `<v>-0~pav<N>` `lt` `<v>-1` y, si `N > 1`, `gt` `<v>-0~pav<N-1>` (FR-024); un fallo es error de etapa `test`
- [ ] T043 [P] [US3] Implementar `internal/installtest/report.go`: escribir `dist/<name>_<v>_<arch>.test.json` con `results[].{suite, status: pass|fail|excluded, stage, reason}` y `overall` (`pass` si ningún `fail`), más una tabla legible en stdout
- [ ] T044 [US3] Conectar `test <pkg> [--arch A] [--suite S]` en `cmd/debfactory/main.go`: `Preflight()`, las suites excluidas se informan como `excluded: <motivo>` sin ejecutarse, y el código de salida es 1 si `overall = fail`
- [ ] T045 [US3] Ejecutar `just test fnm` (5 suites × 2 arch) y anotar en `specs/001-manifest-deb-builder/research.md` §3 el tiempo total y cualquier ajuste necesario

**Checkpoint**: fnm da `pass` en las 10 combinaciones; un fallo forzado da `overall: fail`
indicando la etapa.

---

## Phase 6: User Story 4 - Agregar o actualizar una herramienta editando solo datos (Priority: P2)

**Goal**: añadir o actualizar paquetes sin tocar código; construir todos de una vez.

**Independent Test**: quickstart escenarios 6 y 7.

- [ ] T046 [US4] Implementar `build --all` en `internal/build/pipeline.go` y `cmd/debfactory/main.go`: recorre `packages/*.yaml` en orden alfabético, sigue con el siguiente si uno falla, imprime un resumen (`ok`/`fail` con la etapa) y sale con 1 si alguno falló
- [ ] T047 [US4] Añadir una regla en `internal/manifest/semantic.go`: si `version` no tiene entrada en el lockfile y el lockfile tiene otras versiones, `revision` MUST ser `1` (error `revision: debe ser 1 para una versión upstream nueva`)
- [ ] T048 [US4] Añadir `lock --prune` en `internal/build/lockcmd.go`: borra del lockfile todas las versiones salvo la del manifiesto
- [ ] T049 [P] [US4] Test en `internal/manifest/examples_test.go`: todos los `specs/001-manifest-deb-builder/contracts/examples/*.yaml` (salvo `*.lock.yaml`) y `packages/*.yaml` pasan `validate`, y `minimal-static.yaml` tiene ≤ 25 líneas (SC-003)

**Checkpoint**: escenarios 6 y 7 del quickstart pasan; `git status` muestra solo el manifiesto
y su lockfile.

---

## Phase 7: User Story 5 - Expresar paquetes no triviales con el mismo esquema (Priority: P3)

**Goal**: globs, árboles, enlaces, man pages, completions (de archivo y generadas), relaciones,
exclusiones de arquitectura y overrides de lintian; los 5 paquetes reales construidos y probados.

**Independent Test**: `just build-all && just test <cada paquete>`; cada `.deb` contiene
exactamente los archivos declarados (`dpkg-deb -c`).

- [ ] T050 [US5] Spike de pnpm: en un contenedor `debian:trixie`, colocar el árbol de `pnpm-linux-x64.tar.gz` en `/usr/lib/pnpm/` con el enlace `/usr/bin/pnpm → /usr/lib/pnpm/pnpm` y ejecutar `pnpm --version`; anotar el resultado en research.md §3 riesgo 2 (si falla, un envoltorio instalado como archivo)
- [ ] T051 [US5] Spike de completions bajo qemu: ejecutar `bob complete bash` y `aqua completion bash` con `--platform linux/arm64`; anotar en research.md §3 riesgo 3 si la salida es idéntica a la de amd64 o si hay que generarla solo con amd64
- [ ] T052 [US5] Añadir globs a `internal/assemble/install.go`: `src` con `*`, `?` y `[...]` resuelto con `filepath.Glob` en el directorio extraído; cero coincidencias → error `assemble: install[i].src: el patrón "<p>" no seleccionó ningún archivo`; con varias coincidencias, `dst` debe terminar en `/`
- [ ] T053 [US5] Añadir `type: tree` a `internal/assemble/install.go`: copia recursiva ordenada; directorios 0755, archivos 0755 si tienen el bit x en el artefacto, si no 0644
- [ ] T054 [P] [US5] Añadir `links` a `internal/assemble/install.go` y `internal/pkg/build.go` (entradas nfpm de tipo `symlink`); el destino de cada enlace debe existir en el `BuildPlan`
- [ ] T055 [P] [US5] Implementar `internal/assemble/man.go` (+ test): resolver los globs de `man`, tomar la sección de la extensión (`.1` → `man1`), comprimir con gzip nivel 9 sin nombre y con ModTime 0, e instalar en `/usr/share/man/man<N>/<archivo>.gz` con modo 0644
- [ ] T056 [US5] Implementar `internal/assemble/completions.go` para `completions.<shell>.file`: copiar a `/usr/share/bash-completion/completions/<name>`, `/usr/share/zsh/vendor-completions/_<name>` y `/usr/share/fish/vendor_completions.d/<name>.fish` con modo 0644
- [ ] T057 [US5] Añadir `completions.<shell>.generate` a `internal/assemble/completions.go`: ejecutar en `config.completions_image` vía `internal/container` con `--network none`, `--read-only`, `--platform linux/<arch>`, el árbol preparado montado en `/stage` en solo lectura y `PATH=/stage/usr/bin:/stage/usr/lib/<name>:$PATH`; las rutas absolutas del comando (p. ej. `/usr/lib/pnpm/pnpm`) se reescriben a `/stage/...`; stdout vacío o código ≠ 0 → error `assemble` con stderr recortado; aplicar la decisión de T051
- [ ] T058 [P] [US5] Pasar `depends`, `recommends`, `conflicts`, `replaces` y `provides` del manifiesto a `nfpm.Overridables` en `internal/pkg/build.go`, fusionando `depends` con el resultado de `elfdeps` (si el manifiesto declara el mismo paquete, gana el del manifiesto)
- [ ] T059 [P] [US5] Implementar los overrides en `internal/pkg/lintian_overrides.go`: generar `/usr/share/lintian/overrides/<name>` (modo 0644) con una línea `<name>: <tag> [<path>]` por override (con el comentario `# <reason>` encima) e incluirlos en `build.json`
- [ ] T060 [US5] Respetar `exclude.arches` en `internal/build/pipeline.go`: `build` sin `--arch` omite esas arquitecturas e imprime `excluded: <arch>: <motivo>`; `build --arch` sobre una arquitectura excluida es error de uso (código 2)
- [ ] T061 [P] [US5] Crear `packages/pnpm.yaml` desde `contracts/examples/pnpm.yaml` (ajustado al resultado de T050), `debfactory lock pnpm` y versionar el lockfile
- [ ] T062 [P] [US5] Crear `packages/bob.yaml` desde `contracts/examples/bob.yaml` (con `exclude.suites.bookworm`), `debfactory lock bob` y versionar el lockfile
- [ ] T063 [P] [US5] Crear `packages/buf.yaml` desde `contracts/examples/buf.yaml` (3 ejecutables por glob, completions de archivo, 113 man pages, override `statically-linked-binary`), `debfactory lock buf` y versionar el lockfile
- [ ] T064 [P] [US5] Crear `packages/aqua.yaml` desde `contracts/examples/aqua.yaml` (checksums upstream, override `statically-linked-binary`), `debfactory lock aqua` y versionar el lockfile
- [ ] T065 [US5] Añadir a `packages/fnm.yaml` el bloque `completions` de `contracts/examples/fnm.yaml` (sin cambiar `revision`, porque aún no se ha publicado nada)
- [ ] T066 [US5] Ejecutar `just build-all` y `just test` de los 5 paquetes; comprobar con `dpkg-deb -c` que buf contiene `buf`, `protoc-gen-buf-breaking`, `protoc-gen-buf-lint`, 3 completions y 113 man pages `.gz`, y que pnpm contiene `/usr/lib/pnpm/dist/` y el enlace `/usr/bin/pnpm` (SC-001, SC-002)

**Checkpoint**: 10 `.deb` construidos; las pruebas dan `pass` salvo bob en bookworm
(`excluded`).

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T067 [P] Escribir `README.md`: un párrafo por componente (manifiesto, lockfile, config, caché, lintian, pruebas) y la orden de cada operación (constitución IX), con enlaces a `specs/001-manifest-deb-builder/quickstart.md` y `contracts/`
- [ ] T068 Completar la receta `check` del `justfile`: `gofmt -l .` (vacío), `go vet ./...`, `go test ./...`, `debfactory validate`, `debfactory build --all --check-reproducible` y `just test` de todos los paquetes
- [ ] T069 Recorrer los escenarios 1–7 de `specs/001-manifest-deb-builder/quickstart.md` y corregir cualquier diferencia entre la guía y el comportamiento real
- [ ] T070 Actualizar `specs/001-manifest-deb-builder/research.md` §3 con el resultado final de los 4 riesgos y repetir el Constitution Check de `plan.md` con lo implementado

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: sin dependencias.
- **Foundational (Phase 2)**: depende de Setup; bloquea todas las historias. T017 (nfpm) y
  T018 (licensecheck) deben estar resueltos antes de T026 y T037.
- **US1 (Phase 3)**: depende de Foundational. Es el MVP.
- **US2 (Phase 4)**: depende de US1 (amplía `fetch` y `pipeline`).
- **US3 (Phase 5)**: depende de US1 (necesita `.deb` en `dist/`); independiente de US2.
- **US4 (Phase 6)**: depende de US1; T049 se completa después de US5 (valida `packages/*.yaml`).
- **US5 (Phase 7)**: depende de US1; T066 depende de US3 (pruebas) y de US2 (aqua y buf usan
  checksums upstream).
- **Polish (Phase 8)**: al final.

### User Story Dependencies

```text
Setup → Foundational → US1 ─┬─ US2 ───────────┐
                            ├─ US3 ───────────┼─ US5 (T066) → Polish
                            └─ US4 (T046–T048)┘
```

### Within Each User Story

- Los spikes (T017, T018, T050, T051) van antes de las tareas que dependen de su resultado.
- `internal/*` antes de conectar en `cmd/debfactory/main.go`.
- Los manifiestos reales (`packages/*.yaml`) después de que el pipeline soporte sus
  capacidades.

### Parallel Opportunities

- Setup: T002–T006 en paralelo.
- Foundational: T009, T010, T014, T015 y T018 en paralelo; T011 → T012 → T013 en secuencia.
- US1: T020, T023, T024 y T025 en paralelo tras T019/T021/T022.
- US3: T042 y T043 en paralelo.
- US5: T054, T055, T058 y T059 en paralelo; T061–T064 en paralelo (un manifiesto por archivo).

---

## Parallel Example: User Story 1

```bash
# Tras T019 (fetch), T021 (extract) y T022 (install):
Task: "T020 internal/fetch/github.go — PublishedAt y LicenseURL"
Task: "T023 internal/assemble/elfdeps.go — Depends desde ELF"
Task: "T024 internal/pkg/copyright.go — copyright DEP-5"
Task: "T025 internal/pkg/changelog.go — changelog.Debian.gz reproducible"
```

## Parallel Example: User Story 5

```bash
# Tras T052/T053 (globs y árboles):
Task: "T061 packages/pnpm.yaml + lock"
Task: "T062 packages/bob.yaml + lock"
Task: "T063 packages/buf.yaml + lock"
Task: "T064 packages/aqua.yaml + lock"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 + Phase 2 (incluidos los spikes T017 y T018).
2. Phase 3: fnm construido para amd64 y arm64, lintian sin errores y reproducible.
3. **STOP and VALIDATE**: quickstart escenarios 1 y 3.

### Incremental Delivery

1. MVP (US1) → fnm construible.
2. US2 → la fábrica falla cerrado (requisito constitucional; antes de tener más paquetes).
3. US3 → pruebas de instalación (gate de publicación futuro).
4. US4 → `build --all` y flujo de actualización.
5. US5 → los otros 4 paquetes y las capacidades avanzadas del esquema.

Una sola persona: seguir el orden de prioridad; hacer commit al cerrar cada checkpoint.

---

## Notes

- [P] = archivos distintos y sin dependencias pendientes.
- Ningún binario upstream se ejecuta en el host: solo en contenedores (T027, T041, T050, T051,
  T057).
- `build` nunca escribe el lockfile; solo `lock` (T028, T048).
- Ningún `.deb` llega a `dist/` sin haber pasado lintian (T029).
