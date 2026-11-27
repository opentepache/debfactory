---
description: "Task list for 009-hosted-hardened-builds"
---

# Tasks: Nivel 2 con toolchains de upstream, compilación hospedada y cadena de suministro endurecida

**Input**: Design documents from `/specs/009-hosted-hardened-builds/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/pipeline.md, quickstart.md

**Tests**: incluidos; la spec fija criterios verificables (SC-001–SC-008). Tests Go junto al código;
`integration` para git/aptly/cosign; `e2e` para Docker. Lo que hace el mantenedor en GitHub o en la
PC es **(manual)**. Los workflows de `packages` los empuja el mantenedor (el token del asistente no
puede).

**Organization**: fases por historia de usuario en orden de prioridad; US6 (lo existente no cambia)
se comprueba en cada fase y al final.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: US1–US7 de la spec

---

## Phase 1: Setup (mediciones y acciones del mantenedor)

- [ ] T001 **(manual-asistido)** Medir con contenedores: (a) diferencias entre `ghostty-1.4.0.tar.gz` de `release.files.ghostty.org` y el archivo del tag `v1.4.0` de GitHub (archivos añadidos o distintos, sin el directorio superior); (b) `cosign verify-blob-attestation` contra una atestación de GitHub real de `aqua_linux_amd64.tar.gz` (emisor, identidad del workflow de aqua, ref del tag). Anotar resultados en `specs/009-hosted-hardened-builds/research.md` § D9 y § D11 ("Resultado de la medición").
- [ ] T002 **(manual)** El mantenedor: revisa el historial y hace público `opentepache/debfactory` (Settings → General); en `opentepache/packages` activa "Require signed commits" en `main` y, en ambos repositorios, "Require approval for all external contributors" para workflows de forks. Registro en `docs/operacion-ci.md` § 10 (T046).

---

## Phase 2: Foundational

**Purpose**: manifiesto, lockfile, estado, auditoría y verificación de firmas que usan todas las historias.

- [ ] T003 [P] Esquema `schema/manifest.v1.schema.json` y tipos `internal/manifest/types.go`: `exclude.arches` (mapa arquitectura → motivo ≥ 10 caracteres), `signing` (exactamente uno de `minisign: {key, file}`, `gpg: {fingerprint, key_file, file}`, `sigstore: {identity, issuer, bundle}`), nivel 2: `toolchain` (`name`, `version`, `url` con plantillas `{{version}}`/`{{arch}}`, `arch_map`, `signing`), `deps` (`manager: zig`, `exclude_paths`), `source.extra_files` (lista de `{path, reason}`; rutas exactas, sin comodines).
- [ ] T004 Validación semántica en `internal/manifest/semantic.go`: `exclude.arches` solo con arquitecturas de la configuración y al menos una restante; URLs de firma con las mismas reglas de origen que el artefacto; `source.url` de nivel 2 admite el dominio de `homepage` solo con `signing` fijado; `extra_files` sin `..`, sin comodines y sin duplicados; `toolchain` solo en nivel 2. Tests en `internal/manifest/level2_test.go` y fixtures en `testdata/manifests/invalid/`.
- [ ] T005 [P] Lockfile en `internal/lock/lock.go`: `tag_archive_sha256`, `extra_files` (ruta → sha256), `toolchain` (arquitectura → sha256), `deps` (lista `{url, hash, sha256}` ordenada por hash), `zigdeps_sha256`; `Equal`, `diff` y validación de formato. Tests.
- [ ] T006 [P] Ubicación autocontenida (research D21) en `internal/manifest/load.go` y `internal/lock/`: aceptar `packages/<p>/<p>.yaml` y `packages/<p>/<p>.lock.yaml` además de la ubicación actual (sin ambos a la vez); orden `debfactory migrate layout` en `cmd/debfactory/main.go` que mueve los archivos con `git mv`. Tests.
- [ ] T007 [P] Estado en `internal/repo/state.go`: `Published.Arches` (`arches`, omitido si son todas las de la configuración); `Validate` exige `.deb` exactamente para esas arquitecturas (nivel 1 y `builds.<s>.debs` de nivel 2); `orig.components` (`zigdeps: {file, sha256}`) en `Artifacts()`; `schemaFor` sin cambios para estados sin novedades (FR-021). Fixtures en `testdata/repo/state/` y prueba de ida y vuelta.
- [ ] T008 [P] Auditoría en `internal/ci/audit.go`: `extend` con `arches`; `publish`/`extend` de nivel 2 con `toolchain` (nombre, versión, sha256 por arquitectura); `CheckAudit` cubre arquitecturas ampliadas. Tests en `internal/ci/audit_test.go`.
- [ ] T009 Verificación de firmas en `internal/supply/signature.go` (paquete nuevo): `Verify(ctx, sig manifest.Signing, file, sigFile)` con `minisign -V -P`, `gpgv --keyring` (llave de `key_file`) o `cosign verify-blob --bundle --certificate-identity-regexp --certificate-oidc-issuer`, todos empaquetados en Debian, sin red; errores con el tipo y la llave esperada. Tests con llaves de prueba generadas en el test (minisign y gpg) y un bundle de sigstore grabado en `testdata/supply/`.
- [ ] T010 Firmas en lock y build de nivel 1 (FR-015): `internal/build/lockcmd.go` y `internal/build/pipeline.go` verifican la firma fijada del artefacto o del archivo de checksums antes de usarlo; `lock` avisa si el release trae `.minisig`, `.sig`, `.asc` o `.sigstore.json` y el manifiesto no fija `signing`. Tests con el harness de `internal/build`.

**Checkpoint**: manifiestos, lockfiles, estado y auditoría admiten lo nuevo; nivel 1 sigue igual (`go test ./...`).

---

## Phase 3: User Story 1 - Toolchain y dependencias de upstream; ghostty compila (Priority: P1) 🎯 MVP

**Goal**: con su toolchain y dependencias verificadas, ghostty 1.4.0 compila sin red en trixie (amd64) con la herramienta, dos veces igual.

**Independent Test**: `debfactory lock ghostty` y `debfactory build ghostty --suite trixie --arch amd64` en local producen `.deb` idénticos en las dos variantes; una dependencia, un toolchain o una firma alterados fallan antes de compilar.

- [ ] T011 [P] [US1] Lector de `build.zig.zon` en `internal/zon/zon.go` (paquete nuevo): extrae `dependencies` con `url`, `hash`, `lazy` y `path` (relativas), tolerante a comentarios y comas finales; sin evaluar código. Tests con los `build.zig.zon` reales de ghostty 1.4.0 copiados a `testdata/zon/`.
- [ ] T012 [US1] Cierre transitivo en `internal/supply/zigdeps.go`: rondas sobre los `build.zig.zon` del árbol (salvo `deps.exclude_paths`) y de cada dependencia obtenida; cada `url` se obtiene con `zig fetch` uno por uno dentro de un contenedor con el toolchain verificado (ningún binario de upstream en el host); el hash devuelto MUST ser el declarado; extrae cada paquete en `<dir>/<hash>/` (sin el envoltorio de la caché ni el directorio superior único, research D2); devuelve la lista para el lockfile con el sha256 de cada árbol (tar determinista). Tests con un servidor TLS falso y un "zig fetch" simulado; E2E real en T019.
- [ ] T013 [US1] Toolchain en `internal/supply/toolchain.go`: descarga por arquitectura, verifica sha256 del lockfile y firma (`signing` del toolchain) y lo deja en un directorio para montarlo en el contenedor (`/opt/toolchain`). Tests.
- [ ] T014 [US1] `debfactory lock` para nivel 2 con toolchain (`internal/build/lockcmd.go`): toolchain de las dos arquitecturas, firma del fuente, dependencias (T012), tarball contra tag (T020) y `zigdeps_sha256` del componente determinista (T015); escribe todo en el lockfile. Tests de harness.
- [ ] T015 [US1] Componente `.orig-zigdeps.tar.xz` en `internal/srcpkg/srcpkg.go`: tar determinista (orden fijo, `0:0`, mtime del lockfile, `xz -9`) de las dependencias verificadas, nombre `<p>_<upstream>.orig-zigdeps.tar.xz`, extraído por `dpkg-source -x` en `zigdeps/`; `debian/upstream/toolchain` con nombre, versión, URL y sha256 por arquitectura; el script de `dpkg-source -b` lo incluye. Tests sin Docker (sha256 estable entre dos ejecuciones).
- [ ] T016 [US1] `srcbuild` con toolchain (`internal/srcbuild/srcbuild.go`): monta el toolchain verificado en `/opt/toolchain` (solo lectura) y lo pone en `PATH` del usuario que compila; la compilación sigue sin red; `buildTimeout` configurable (`level2.timeout`, por defecto 90 min). Tests con el falso de `internal/build`.
- [ ] T017 [US1] Paquete `ghostty` en `testdata/fixtures/ghostty/` (y luego en `packages`, T049): manifiesto (`level: 2`, `justification`, `source` de `release.files.ghostty.org` con `signing.minisign` de `PACKAGING.md`, `toolchain` Zig 0.17.0 con su llave minisign, `deps.manager: zig` con `exclude_paths: [example/]`, `exclude.suites` sid/bookworm/noble con motivo, `test: {command: "ghostty +version", expect: "1.4.0"}`) y `debian/` (`control` con los `Build-Depends` de research D14, `rules` con `zig build --system zigdeps -fno-sys=harfbuzz -Doptimize=ReleaseFast -Dcpu=baseline -Dversion-string=$(DEB_VERSION_UPSTREAM) --prefix /usr` y `DESTDIR`, `copyright` con las licencias de las dependencias).
- [ ] T018 [P] [US1] Gate de licencias de las dependencias (research Riesgo 2) en `internal/license/`: cada dependencia de Zig con su licencia detectada (archivo LICENSE/COPYING del árbol) y SPDX en la allowlist; falla si alguna no está; el informe la lista. Tests.
- [ ] T019 [US1] E2E `TestE2EGhosttyLocal` en `internal/repotest/ghostty_test.go` (tag `e2e`, fuera de `just e2e` por duración; receta `just e2e-ghostty`): `lock` y `build --suite trixie --arch amd64` en contenedores; dos variantes idénticas; `ghostty +version` = `1.4.0` en la prueba de instalación; anota tiempo y memoria.

**Checkpoint**: ghostty compila en local; las verificaciones de toolchain, dependencias y firma bloquean lo alterado.

---

## Phase 4: User Story 4 - Detectar un upstream comprometido (Priority: P1)

**Goal**: tarball contra tag, llaves fijadas y procedencia de upstream bloquean antes de compilar.

**Independent Test**: los fixtures alterados de `testdata/fixtures/supply/` fallan con la etapa correcta; los correctos pasan.

- [ ] T020 [US4] Tarball contra tag en `internal/supply/tagdiff.go`: descarga el archivo del tag (`https://github.com/<upstream>/archive/refs/tags/<tag>.tar.gz`, sha256 al lockfile), compara archivo por archivo sin el directorio superior; añadidos o distintos fallan salvo `source.extra_files` con el sha256 del lockfile; faltantes no fallan. Tests con tarballs generados (archivo extra, archivo distinto, declarado con otro sha256, `export-ignore`).
- [ ] T021 [US4] Procedencia de upstream en `internal/supply/provenance.go`: atestaciones de GitHub por digest (`GET /repos/<upstream>/attestations/sha256:<d>`, `User-Agent: debfactory`, sin token o con `GITHUB_TOKEN` de solo lectura) verificadas con `cosign verify-blob-attestation` (identidad `https://github.com/<upstream>/`, ref del tag); sin atestaciones: "ausente" sin fallar. Tests con un bundle grabado de aqua (T001) y uno de otro repositorio que debe fallar.
- [ ] T022 [US4] Integrar en el flujo: nivel 1 (`internal/build/pipeline.go`: firma y procedencia) y nivel 2 (`level2 source`, T026: firma, tag, procedencia); etapas de error `signature`, `tagdiff`, `provenance` en `internal/report/errors.go`; informe `supply.json` (data-model §7) en `dist/`. Tests de harness con los casos de SC-003.
- [ ] T023 [P] [US4] Fijar firmas de nivel 1 en `testdata/project/packages/` y en `packages` (FR-016a, PR del mantenedor en T049): buf (`signing.minisign` sobre `sha256.txt`), aqua (`signing.sigstore` sobre los checksums y procedencia de GitHub); fnm y bob sin firma (informe "upstream no firma"). Comprobar que sus `.deb` no cambian (SC-006).

**Checkpoint**: SC-003 completo en pruebas.

---

## Phase 5: User Story 2 - GitHub compila y prueba; la PC verifica, firma y publica (Priority: P1)

**Goal**: los workflows de `packages` compilan y prueban en runners estándar con atestación; `pub` acepta con verificación independiente y publica; las PR corren enteras en GitHub.

**Independent Test**: una PR de prueba en `packages` corre `plan`, `l1-build`, `l2-source`, `l2-build`, `test`, `review` y `comment` en runners hospedados; integrar publica tras `debfactory accept`; un artefacto sin atestación o alterado no se publica.

- [ ] T024 [US2] Plan para GitHub en `internal/ci/matrix.go`: `debfactory ci matrix` imprime en JSON los paquetes afectados y la matriz (nivel 1 por arquitectura; nivel 2 por suite × arquitectura × variante; pruebas por paquete × suite × arquitectura no excluida) para `strategy.matrix`. Tests con el fixture de `internal/ci`.
- [ ] T025 [US2] Modo job hospedado en `cmd/debfactory/main.go` e `internal/build/`: `debfactory build <p> --arch A --out DIR` (nivel 1) y `debfactory level2 build <p> --suite S --arch A --variant V --source DIR --out DIR` dejan sus archivos y `build.json` en `DIR` para `upload-artifact`; usan Docker del runner (con root) sin cambiar la lógica de `srcbuild`.
- [ ] T026 [US2] `debfactory level2 source <p> --out DIR` en `internal/build/level2.go`: verifica fuente (sha256, firma, tag, procedencia), dependencias (T012) y toolchain, genera el paquete fuente de cada suite no excluida (con el componente `zigdeps`) y `supply.json`. Tests de harness.
- [ ] T027 [US2] `debfactory test <p> --suite S --arch A --deb FILE --out DIR` en `internal/installtest/`: prueba de instalación y ciclo de vida de 008 en la suite indicada con la arquitectura nativa del runner; deja `test.json` con el sha256 del `.deb`. Tests.
- [ ] T028 [US2] Verificación de atestaciones en `internal/supply/attest.go`: por cada archivo, `GET /repos/opentepache/packages/attestations/sha256:<d>` con el token del job y `cosign verify-blob-attestation` exigiendo emisor `https://token.actions.githubusercontent.com`, identidad `https://github.com/opentepache/packages/.github/workflows/main.yml@refs/heads/main` (o `pr.yml` según el modo) y `sourceRepositoryDigest` igual al commit. Tests con bundles grabados de un workflow de prueba y casos de rechazo (otro repo, workflow, ref, commit).
- [ ] T029 [US2] `debfactory accept` en `internal/ci/accept.go` (research D17): atestaciones (T028) de todos los artefactos e informes; nivel 2: dos variantes idénticas; nivel 1: reconstrucción local con el mismo sha256; lintian local de todos los `.deb`; prueba de humo amd64 (instalar y ejecutar en la primera suite no excluida); combinaciones o informes faltantes → `inputs`; copia a `dist/` lo aceptado con sus `build.json`/`test.json`. Etapas `attest`, `compare`, `inputs`. Tests de integración con artefactos simulados.
- [ ] T030 [US2] `ci main` en `internal/ci/run.go`: con `--artifacts DIR` usa `accept` en lugar de `gates`; sin él (`--local`, FR-011b) mantiene el flujo de 008; FR-012: si el repositorio de paquetes no es público (API), rechaza el modo hospedado con un mensaje claro. Tests de integración en `internal/ci/hosted_test.go`.
- [ ] T031 [US2] Plantillas de workflows en `ci/workflows/main.yml` y `ci/workflows/pr.yml` (contracts/pipeline.md): jobs `plan`, `l1-build`, `l2-source`, `l2-build`, `test` en `ubuntu-24.04`/`ubuntu-24.04-arm` con `permissions: {contents: read, id-token: write, attestations: write}`, `actions/setup-go` con versión exacta, compilación de `debfactory` desde `debfactory.version` y comprobación del commit, `actions/attest-build-provenance` por artefacto, `upload-artifact` con retención de 1 día; `publish` en `[self-hosted, debfactory-main]` que descarga y ejecuta `ci main --artifacts`; `pr.yml` sin runners propios. Acciones fijadas por sha.
- [ ] T032 [US2] `ci-lint` ampliado en `internal/ci/workflowlint.go` y la receta `just ci-lint`: falla si un job en runner hospedado usa `secrets.*`, permisos fuera de los listados, acciones sin sha, runners "larger" o etiquetas de pago, o si `publish` no es el único job en runner propio. Tests con workflows válidos e inválidos en `testdata/workflows/`.
- [ ] T033 [US2] Retiro del runner `ci` (research D20): `ci/runner/setup.sh --remove ci` (desregistra el runner, borra el servicio, el usuario `dfci` y sus directorios con confirmación); `ci doctor` deja de exigirlo y avisa si sigue registrado. Tests del doctor.
- [ ] T034 [US2] **(manual)** El mantenedor copia `ci/workflows/*.yml` a `packages/.github/workflows/`, abre una PR de prueba con un cambio de tepache-hello (copiado a `packages` solo en esa rama) y comprueba que todo corre en runners hospedados; mide tiempos.

**Checkpoint**: PR y publicación con runners hospedados; la PC solo verifica, firma y publica.

---

## Phase 6: User Story 7 - Cualquiera puede verificar lo publicado (Priority: P1)

**Goal**: página de procedencia por versión, `debfactory verify` para terceros, commits firmados.

**Independent Test**: la página de una versión enlaza commit, run, Rekor, `.buildinfo` y resultados; `debfactory verify` en un contenedor limpio da "verificado" y con un `.deb` alterado "no verificado".

- [ ] T035 [US7] `provenance.json` (data-model §8) al publicar en `internal/ci/run.go` / `internal/repo/`: se genera por versión con lo aceptado (digests, URLs de Rekor `https://search.sigstore.dev/?hash=<d>`, run, commit, resultados, `audit_seq`, verificación de la PC) y se guarda en `<data>/provenance/<p>/<versión>.json`, versionado en la rama de estado.
- [ ] T036 [US7] Sitio en `internal/site/`: página `/<p>/<versión>/` con la procedencia (es/en), enlace desde la página del paquete y desde cada versión; `provenance.json` publicado junto al sitio; plantilla y textos en `internal/site/templates/` e `internal/site/i18n/`. Tests de render y de accesibilidad (`internal/repotest/a11y_test.go`).
- [ ] T037 [US7] `debfactory verify <p> <versión> [--suite S --arch A]` en `internal/verify/verify.go` (paquete nuevo) y `cmd/debfactory/main.go`: sin credenciales; baja `provenance.json`, el `InRelease` y el `.deb` del repositorio público; verifica la firma con el keyring publicado (`gpgv`), la atestación con `cosign` y reconstruye (nivel 1) o recompila (nivel 2 siguiendo la guía) en contenedores; informa "verificado" o "no verificado: <motivo>". Tests de integración con un repositorio de prueba.
- [ ] T038 [US7] E2E `TestE2EVerify` en `internal/repotest/verify_test.go`: publicar tepache-hello y un paquete de nivel 1 en el repositorio de prueba, servir el sitio y verificar ambos desde un contenedor limpio; alterar el `.deb` servido → "no verificado" (SC-007).
- [ ] T039 [P] [US7] `ci doctor` en `internal/ci/doctor.go`: comprueba la regla "Require signed commits" de `main` en `packages` y que `debfactory` es público (API). Tests contra `FakeGitHub`.
- [ ] T040 [P] [US7] Guía de verificación en `testdata/project/docs/usuario.md` (plantilla para `packages`): qué garantiza OpenTepache (integridad, procedencia, reproducibilidad, retiro rápido) y qué no (que upstream sea de fiar), cómo leer la página de procedencia y cómo usar `debfactory verify` (FR-019, FR-024).

**Checkpoint**: SC-007 en verde.

---

## Phase 7: User Story 3 - Excluir y recuperar una arquitectura (Priority: P2)

**Goal**: `exclude.arches` publica en las demás; quitarlo amplía la versión vigente.

**Independent Test**: tepache-hello con arm64 excluido publica solo amd64; al quitarlo, la siguiente ejecución publica arm64 sin cambiar amd64.

- [ ] T041 [US3] Arquitecturas por paquete en build, pruebas e inputs (`internal/build/`, `internal/installtest/`, `internal/repo/inputs.go`, `internal/ci/matrix.go`): las excluidas no se construyen ni se prueban ni cuentan para "falla en una"; `Published.Arches` se rellena. Tests.
- [ ] T042 [US3] Ampliación por arquitectura en `internal/ci/plan.go` y `internal/repo/apply.go`: `MissingArches` (análogo a `MissingSuites`); compila y prueba solo la arquitectura nueva; `Apply` agrega sus archivos a la versión vigente sin tocar los demás; auditoría `extend` con `arches`; pendiente, notificado y reintentado si falla. Tests de integración en `internal/ci/arches_test.go`.
- [ ] T043 [P] [US3] Sitio y guía: arquitecturas de cada paquete y motivo de la exclusión en `internal/site/` y en `testdata/project/docs/usuario.md`. Tests de render.

---

## Phase 8: User Story 5 - Señales de aviso (Priority: P3)

- [ ] T044 [P] [US5] Scorecard en `internal/supply/scorecard.go`: `GET https://api.securityscorecards.dev/projects/github.com/<upstream>` (`User-Agent: debfactory`), puntuación y 3 comprobaciones más bajas al informe; sin respuesta → "no disponible". Tests con servidor falso.
- [ ] T045 [US5] Resumen del cambio (research D13) en `internal/supply/review.go` y `debfactory supply review <p> --from V1 --to V2`: diff determinista del fuente (archivos cambiados, binarios añadidos, cambios en scripts de build/instalación, URLs nuevas) y llamada a GitHub Models con el `GITHUB_TOKEN` (`models: read`, modelo en `watch.ai_model`); salida markdown con "cambios sospechosos"; sin cuota o error → "resumen no disponible"; el job `comment` (sin IA) publica el comentario. Tests con servidor falso (sin llamar al servicio real).

---

## Phase 9: Polish, US6 y cierre

- [ ] T046 [P] `docs/operacion-ci.md` § 10 "Fase 009": modelo (GitHub compila y prueba, la PC verifica y publica), modo local, toolchains y dependencias, firmas fijadas, tarball contra tag, exclusión de arquitecturas, retiro del runner `ci`, transparencia y `verify`, acciones del mantenedor (T002).
- [ ] T047 [P] Notas en specs anteriores: `specs/003-ci-pipeline/contracts/runner-host.md` y `workflows.md` (solo `pub`, jobs hospedados), `specs/008-source-builds/research.md` D1′/D2/D15 (dónde corre el nivel 2 ahora).
- [ ] T048 US6: `just check` y `just e2e` en verde; `TestLevel1Unchanged` y la E2E de tepache-hello sin cambios (SC-006); `migrate layout` aplicado a `testdata/project` sin cambiar ningún `.deb`.
- [ ] T049 **(manual)** `just tool-install`; PR en `packages`: herramienta nueva, workflows (T031), `migrate layout`, firmas de buf y aqua (T023), y ghostty (T017); comprobar en la PR los jobs hospedados, el informe de cadena de suministro, Scorecard y el resumen; integrar y comprobar la publicación de ghostty en trixie y forky (amd64, arm64), `apt install ghostty` y `ghostty +version` (SC-001, SC-002), su página de procedencia y `debfactory verify` (SC-007).
- [ ] T050 **(manual)** Retirar el runner `ci` (`ci/runner/setup.sh --remove ci`) tras una semana sin incidentes de los workflows nuevos; `ci doctor` en verde (SC-008).

---

## Dependencies & Execution Order

- Fase 1 primero (T001 alimenta T020/T021; T002 antes de T034 y T049).
- Fase 2 antes de las historias; T003 → T004; T005, T006, T007, T008 en paralelo; T009 → T010.
- US1: T011 → T012 → T013 → T014 → T015 → T016 → T017 → T019; T018 en paralelo tras T012.
- US4: T020 y T021 tras T009; T022 tras T020, T021 y T026; T023 tras T010.
- US2: T024–T027 tras US1; T028 → T029 → T030; T031 y T032 tras T024–T027; T033 en paralelo; T034 tras T031 y T002.
- US7: T035 tras T029; T036 y T037 tras T035; T038 tras T037; T039 y T040 en paralelo.
- US3: tras la fase 2; T042 tras T041.
- US5: T044 en paralelo; T045 tras T026.
- Polish al final; T049 tras T048; T050 tras T049.

### Parallel Opportunities

- T003, T005, T006, T007, T008; T011 con T013; T018; T023; T039, T040; T043; T044; T046, T047.

## Implementation Strategy

1. Mediciones y acciones del mantenedor (fase 1), base común (fase 2).
2. MVP: US1, ghostty compila en local con todo verificado.
3. US4 (cadena de suministro) y US2 (GitHub compila y prueba, la PC verifica y publica).
4. US7 (transparencia), US3 (arquitecturas), US5 (avisos).
5. Documentación, validación completa y activación en `packages` (ghostty publicado), retiro del runner `ci`.
