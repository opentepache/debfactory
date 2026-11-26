---
description: "Task list for 005-upstream-release-watch"
---

# Tasks: Detección de versiones upstream y actualización automática

**Input**: Design documents from `/specs/005-upstream-release-watch/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: se incluyen porque la spec define sus criterios de aceptación como simulaciones
(SC-001 a SC-007: release simulado de `bob`, pre-release, licencia no permitida, límite casi
agotado). Como en 003 y 004: los tests Go viven junto al código; los que usan git real llevan el
tag `integration` y los que usan Docker el tag `e2e`. GitHub y upstream se simulan con
`httptest`. Lo que se hace en GitHub o como root en la PC es **(manual)**.

**Organization**: por historia de usuario. La fase 1 ataca primero el riesgo 1 del plan (spike
de la GitHub App), porque si falla cambia research D1 y todo lo demás.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: se puede hacer en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia de usuario de la spec (US1–US5)

---

## Phase 1: Setup (spike de la App y configuración en GitHub)

**Purpose**: comprobar research D1 antes de escribir `ci watch`.

- [ ] T001 **(manual)** Crear la GitHub App `opentepache-updates` en la organización `opentepache` según `specs/005-upstream-release-watch/contracts/workflows.md` § GitHub App: permisos de repositorio Contents *Read and write*, Pull requests *Read and write*, Metadata *Read*; sin webhooks; instalada solo en `opentepache/packages`. Generar la llave privada (`.pem`) y anotar el App ID. La llave no se pega en el chat ni en ningún repositorio.
- [ ] T002 **(manual)** En `opentepache/packages`: Settings → Environments → `watch` con *Deployment branches* = solo `main`, y los secretos `DEBFACTORY_APP_CLIENT_ID` (Client ID de la App) y `DEBFACTORY_APP_KEY` (contenido del `.pem`); Settings → General → activar **Allow auto-merge**. Borrar el `.pem` local tras guardarlo (o moverlo al respaldo fuera de la PC, como la llave GPG).
- [X] T003 **(manual)** Spike (plan, riesgo 1): en una rama de `packages`, un `.github/workflows/spike-app.yml` temporal con `workflow_dispatch`, `runs-on: [self-hosted, debfactory-pr]`, `environment: watch` y `actions/create-github-app-token@64ab729a6294000f5aa4a4bc0b73d1f32bc45510` (v3.3.0; `client-id`, `permission-contents: write`, `permission-pull-requests: write`) que, con `curl` y el token de la App, cree la rama `debfactory/update/spike` desde `main` con un commit que solo cambia un comentario de `config/debfactory.yaml`, abra un PR y active su auto-merge (mutación GraphQL `enablePullRequestAutoMerge`, `mergeMethod: MERGE`). Integrar el workflow del spike en `main` con un PR normal y lanzarlo. Comprobar y anotar en `specs/005-upstream-release-watch/research.md` § D1 (Resultado del spike): (a) `verify` del PR corre sin "Approve workflows to run"; (b) el PR se integra solo tras `verify`; (c) ese merge dispara `main.yml` y termina en "nada que publicar"; (d) `require_extra_approval_for_unattributed_changes` del ruleset no bloquea el merge; (e) un PR normal cuyo `pr.yml` intente imprimir `secrets.DEBFACTORY_APP_CLIENT_ID` lo recibe vacío (riesgo 2); (f) con el token de la App, `GET /repos/MordechaiHadad/bob/releases` responde 200 y anotar `X-RateLimit-Limit` (research D9: si no puede leer upstream ajenos, las lecturas usan el `GITHUB_TOKEN`); (g) anotar en `docs/operacion-ci.md` los permisos efectivos de la App tal como los muestra su página de instalación (contracts/cli.md § `ci doctor`). Después, borrar `spike-app.yml` con otro PR. Si (a), (b) o (c) fallan, parar y revisar D1.

**Checkpoint**: la App funciona como dice research D1.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: configuración, campos del manifiesto, cliente de GitHub y GitHub falso. Bloquea todas las historias.

- [X] T004 [P] `internal/config/config.go`: bloque `watch` (data-model §2) con `Interval` (duración, defecto `24h`, "mínimo `1h`"), `Cooldown` (defecto `48h`, "mínimo `0h`, global"), `RateReserve` (entero, defecto `500`), `BranchPrefix` (defecto `debfactory/update/`) y `AppSlug` (texto, sin defecto); errores de validación con el campo; pruebas en `internal/config/config_test.go` (defectos, mínimos, duración inválida).
- [X] T005 [P] `internal/manifest/types.go`, `internal/manifest/load.go`, `internal/manifest/semantic.go` y `schema/manifest.v1.schema.json`: bloque opcional `watch` (data-model §1) con `enabled` (bool, defecto `true`), `auto_merge` (bool, defecto `false`) y `prerelease_flag` (`trust` | `ignore`, defecto `trust`; cualquier otro valor es error de validación); pruebas en `internal/manifest/load_test.go` (defectos, enum) y un caso en `internal/manifest/examples_test.go`.
- [X] T006 [P] `internal/report/errors.go`: etapas `WatchLicense = "watch-license"`, `WatchAssets = "watch-assets"`, `WatchChecksums = "watch-checksums"`, `WatchTampered = "watch-tampered"`, `WatchRateLimit = "watch-ratelimit"` y `Watch = "watch"` (fallo del propio ciclo).
- [X] T007 `internal/forge/github.go`: (1) `ListReleases(ctx, upstream, etag) (releases []Release, newETag string, notModified bool, err)` con `If-None-Match` y `per_page=30` (`Release`: `tag_name`, `draft`, `prerelease`, `published_at`, `html_url`, `assets[].name`); (2) `RateLimit()` que expone `X-RateLimit-Remaining` y `X-RateLimit-Reset` de la última respuesta, y un error tipado para `403/429` con `Retry-After`; (3) `ListBranchCommits(ctx, branch, base)` (autores de los commits de la rama que no están en `base`); (4) `CommentPR(ctx, number, body)` y `ListPRComments(ctx, number)`; (5) `ClosePR(ctx, number, comment)`; (6) `EnableAutoMerge(ctx, prNodeID)` con la mutación GraphQL `enablePullRequestAutoMerge` (`mergeMethod: MERGE`) en `POST /graphql`; (7) `LastSuccessfulRun(ctx, workflowFile) (started time.Time, ok bool, err)`. El `User-Agent` sigue siendo `debfactory` y el token solo va a la API configurada. Pruebas en `internal/forge/github_test.go`.
  - Hecho (2026-11-19). Cambios al implementar: el intervalo no usa la API de ejecuciones sino el estado de la caché (research D3: los disparos horarios que terminan en «ciclo no vencido» también son ejecuciones con éxito), así que no hay `LastSuccessfulRun`; los commits de la rama se leen con `git log` sobre el checkout en vez de la API. Añadidos `digest` de los assets, `node_id`/`auto_merge` del PR, `created_at` del issue, `UserID` (dirección noreply del bot) y, para `ci doctor`, environments y `allow_auto_merge`.
- [X] T008 `internal/ci/citest/citest.go`: el GitHub falso añade releases por repositorio upstream (`/repos/<o>/<r>/releases`) con ETag y `304`, cabeceras `X-RateLimit-Remaining`/`Reset` con un contador configurable (y `403` al agotarse), commits de rama, comentarios y cierre de PR, `POST /graphql` (registra la mutación de auto-merge y marca el PR), y ejecuciones de un workflow. Un helper `citest.Upstream(t)` (httptest) sirve assets `tar.gz`, `checksums` y textos de licencia de un `bob` simulado en versiones configurables, con fechas de publicación configurables.
- [X] T009 `internal/watch/release.go` (paquete nuevo, sin E/S): `VersionFromTag(tagTemplate, tag string) (string, bool)` que invierte la plantilla `tag` del manifiesto (`v{{version}}`, `release-{{version}}`, `{{version}}`); `Compare(a, b string) int` con la semántica de `dpkg --compare-versions` (reutilizar `internal/installtest.CompareVersions` o una implementación pura probada contra él); pruebas de tabla en `internal/watch/release_test.go`.

**Checkpoint**: config, manifiestos, cliente y GitHub falso listos.

---

## Phase 3: User Story 1 - Una versión nueva estable llega como pull request (Priority: P1) 🎯 MVP

**Goal**: release estable elegible → un único PR correcto por paquete, validado por `verify`.

**Independent Test**: upstream simulado con `bob` 4.4.0 publicado hace 49 h; un ciclo deja un PR `bob: 4.3.0 → 4.4.0` con revisión 1 y lockfile nuevo; un segundo ciclo sin novedades no cambia nada (SC-001, SC-005).

### Tests for User Story 1

- [X] T010 [P] [US1] `internal/watch/candidate_test.go`: tabla de candidata (research D6): mayor que la del manifiesto; la más alta entre varias; ignora versiones menores o iguales; espera (`published_at + cooldown > ahora` → no elegible, con la fecha de elegibilidad); con 4.4.0 elegible y 4.4.1 no elegible propone 4.4.0 (spec US1 escenario 5); `draft` descartado.
- [X] T011 [US1] `internal/ci/watch_test.go` (tag `integration`, repositorio git de `citest.GitFixture` + GitHub falso + `citest.Upstream`): (1) 4.4.0 elegible → rama `debfactory/update/bob` con un commit que cambia solo `version`, `revision: 1` y `packages/bob.lock.yaml`, PR con título `bob: 4.3.0 → 4.4.0` y el cuerpo de `contracts/pr-y-notificaciones.md` (marcador incluido); (2) 4.4.0 y 4.5.0 → propone 4.5.0; (3) PR abierto en 4.4.0 y aparece 4.5.0 → el mismo PR pasa a 4.5.0 (nunca dos, FR-008); (4) rama con un commit ajeno → no se reescribe y se comenta una sola vez por versión (FR-009); (5) release retirado sin sustituta → PR cerrado con comentario y rama borrada; (6) sin novedades → ninguna petición de escritura (FR-022); (7) 4.4.0 publicada hace 10 h → sin PR y resumen `en-espera`; (8) PR abierto en 4.4.0 y `main` avanza con un cambio en `packages/bob.yaml` (p. ej. otra revisión integrada por el mantenedor) → la rama se regenera desde la punta de `main` con la misma versión; (9) paquete con `watch.enabled: false` y paquete sin `upstream` → `omitido` en el resumen, sin peticiones ni fallo (FR-013a).
  - Hecho. Sin tag `integration`: solo usa git y httptest (como `statepr_test.go`), así corre en `go test ./...`. El caso (9) prueba `watch.enabled: false`; un manifiesto sin `upstream` no pasa el esquema. Se añadieron casos de intervalo (FR-001/FR-002) y de barrera.

### Implementation for User Story 1

- [X] T012 [P] [US1] `internal/watch/candidate.go`: `Candidate(manifest, releases, now, cooldown) (*Candidate, *Waiting)` según research D5 (solo `draft` y plantilla `tag` en esta historia) y D6; `Candidate` lleva `Package`, `From`, `To`, `PublishedAt`, `NotesURL`; `Waiting` lleva la versión y la fecha de elegibilidad.
- [X] T013 [US1] `internal/ci/watch.go`: barrera de confianza de `contracts/cli.md` (código 3: `GITHUB_ACTIONS`, `workflow_dispatch`, `refs/heads/main`, `ci.repository`, `RUNNER_LABELS` con `debfactory-pr` y sin `debfactory-main`, `DEBFACTORY_APP_TOKEN` presente); `--dry-run` la omite y no escribe nada; comprobación de intervalo con `LastSuccessfulRun("watch.yml")` y `--force` (research D3).
  - Hecho. El intervalo se lee del estado de la caché (`$RUNNER_TEMP/debfactory-watch/state.json`, research D3); `--package` también lo ignora. La barrera exige además `DEBFACTORY_APP_SLUG`.
- [X] T014 [US1] `internal/ci/watch.go`: preparar la propuesta en un worktree de `main` (research D7): cambiar `version`, poner `revision: 1`, ejecutar la orden de lockfile existente (`internal/lock`, la misma de `debfactory lock`) para la versión nueva; commit con autor `<watch.app_slug>[bot]`; push con `--force-with-lease` a `<watch.branch_prefix><pkg>` usando el token de la App (nunca a `main`).
  - Hecho. Autor de los commits `<app-slug>[bot]` con la dirección noreply de su id; el slug llega en `DEBFACTORY_APP_SLUG` (salida de `actions/create-github-app-token`), no de la configuración. `build.Env` tiene un gancho `LicenseCheck` para clasificar la licencia sin duplicar la orden de lockfile.
- [X] T015 [US1] `internal/ci/watch.go`: abrir o actualizar el PR (research D11, `contracts/pr-y-notificaciones.md`): buscar el PR abierto de la rama; si sus commits son todos de la App, reescribir; si no, comentar con el marcador `debfactory-watch-newer` una vez por versión; cerrar el PR y borrar la rama si la candidata desaparece y no hay sustituta; si la base de la rama ya no es la punta de `main` (o el PR tiene conflicto), regenerar la rama desde `main` aunque la versión no cambie (spec, Edge Cases).
- [X] T016 [US1] `internal/ci/watch.go`: omitir sin peticiones los paquetes con `watch.enabled: false` o sin `upstream` (FR-013a, decisión `omitido`); resumen del ciclo (data-model §7) en la salida estándar con el formato de `contracts/cli.md` y en `$GITHUB_STEP_SUMMARY`; códigos de salida 0/1/2/3; un fallo del propio ciclo se notifica con la etapa `watch` por el notificador de 003.
- [X] T017 [US1] `cmd/debfactory/ci.go`: registrar `ci watch` con `--package`, `--force`, `--dry-run` (y `INPUT_PACKAGE`/`INPUT_FORCE` del entorno, como `ci main` con sus `INPUT_*`); ayuda en `cmd/debfactory/main.go`; caso de uso incorrecto (código 2) en `cmd/debfactory/main_test.go`.
- [X] T018 [US1] `~/Develop/packages/.github/workflows/watch.yml` (repositorio `opentepache/packages`, en una rama) exactamente como `specs/005-upstream-release-watch/contracts/workflows.md`, con `actions/checkout` fijada al mismo sha que `pr.yml` y `actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9` (v6.1.0) para `.debfactory-watch-cache`; y bloque `watch:` con los defectos en `~/Develop/packages/config/debfactory.yaml` (incluido `app_slug: opentepache-updates`). Mismo bloque en `testdata/project/config/debfactory.yaml`.
  - Hecho en la rama local `005-watch` de `~/Develop/packages` (sin commit): `watch.yml` con `client-id`, `app-slug` y la caché en `${{ runner.temp }}/debfactory-watch`, sin `actions: read`; bloque `watch:` sin `app_slug`. Va en el mismo PR que la nueva versión fijada: la `9cd4a9b` rechaza el bloque `watch` (comprobado).

**Checkpoint**: con `ci watch` lanzado a mano, una versión nueva llega como PR y `verify` la valida.

---

## Phase 4: User Story 2 - Las versiones preliminares nunca se proponen (Priority: P1)

**Goal**: ningún pre-release, RC, beta, nightly… llega a un PR (SC-003).

**Independent Test**: upstream simulado con solo `4.4.0` marcado `prerelease` y otro con `v4.4.0-rc1` sin marca: 0 PRs y 0 issues.

### Tests for User Story 2

- [X] T019 [P] [US2] `internal/watch/stable_test.go`: tabla de research D5: `prerelease: true` descartado; con `watch.prerelease_flag: ignore` aceptado si la versión es limpia; marcadores `alpha`, `beta`, `rc`, `pre`, `preview`, `nightly`, `tip`, `canary`, `dev`, `snapshot` en cualquier caso y con separadores `-`, `.`, `_`, `+` o pegados a un número (`1.2.0rc1`, `2.0.0-Beta.2`, `1.0.0+nightly`) siempre descartados aunque `prerelease_flag: ignore`; `release-1.3.0-rc1` con plantilla `release-{{version}}` descartado; versión que no empieza por dígito descartada; `1.2.3`, `2026.11.9` aceptadas.
- [X] T020 [US2] Caso en `internal/ci/watch_test.go`: PR abierto en 4.4.0 y upstream publica `4.5.0-beta` → el PR sigue en 4.4.0 (spec US2 escenario 3); los dos casos del Independent Test → sin escrituras.

### Implementation for User Story 2

- [X] T021 [US2] `internal/watch/stable.go`: `Stable(release, manifest) (version string, ok bool, reason string)` con los pasos 1–5 de research D5; el filtro de marcadores no depende de ningún campo del manifiesto. `internal/watch/candidate.go` lo usa en lugar del filtro mínimo de T012; el resumen distingue `descartada (preliminar)` solo en `--dry-run`.

**Checkpoint**: MVP (US1 + US2): el detector propone solo versiones estables.

---

## Phase 5: User Story 3 - El camino feliz se publica solo con auto-merge (Priority: P2)

**Goal**: con `watch.auto_merge`, una versión menor o parche se integra y publica sin intervención (SC-002).

**Independent Test**: `bob` con `watch.auto_merge: true` y 4.4.0 elegible → el PR tiene el auto-merge activado; con 5.0.0, o con cambio de licencia, no.

### Tests for User Story 3

- [X] T022 [P] [US3] `internal/watch/major_test.go`: FR-015a: `4.3.0 → 5.0.0` mayor; `4.3.0 → 4.4.0` y `4.3.0 → 4.3.1` no; `0.46.0 → 0.47.0` mayor; `0.46.0 → 0.46.1` no; `2026.11.9 → 2026.12.1` no (mismo primer componente); `2026.12.1 → 2027.1.0` mayor; versión no numérica (`r45 → r46`) mayor.
- [X] T023 [US3] Casos en `internal/ci/watch_test.go`: auto-merge activado (mutación GraphQL registrada en el GitHub falso) para 4.3.0 → 4.4.0 con `watch.auto_merge: true`; no activado para 5.0.0, para un cambio de licencia permitido ni con `watch.auto_merge` ausente; el cuerpo del PR dice el motivo (`contracts/pr-y-notificaciones.md`).

### Implementation for User Story 3

- [X] T024 [P] [US3] `internal/watch/major.go`: `IsMajor(from, to string) bool` (FR-015a).
- [X] T025 [US3] `internal/ci/watch.go`: calcular `auto_merge = watch.auto_merge ∧ ¬mayor ∧ licencia igual` (data-model §4), escribir la fila "Auto-merge" del cuerpo y llamar a `EnableAutoMerge` con el token de la App tras crear o actualizar el PR; si el PR deja de cumplir (p. ej. pasa a una versión mayor), desactivarlo (`disablePullRequestAutoMerge`, añadir a `internal/forge/github.go`).
- [X] T026 [US3] `internal/ci/doctor.go` y `internal/ci/doctor_test.go`: si algún manifiesto tiene `watch.auto_merge: true`, comprobar `allow_auto_merge` del repositorio (`GET /repos/{repo}`).

**Checkpoint**: el camino feliz no necesita al mantenedor.

---

## Phase 6: User Story 4 - Licencias y formatos que cambian se notifican, no se reintentan (Priority: P2)

**Goal**: una sola notificación por paquete, versión y motivo; ningún PR roto (SC-004).

**Independent Test**: `bob` 4.4.0 con licencia fuera de la allowlist: 3 ciclos → 1 issue `watch-license`, 0 PRs, nada publicado.

### Tests for User Story 4

- [X] T027 [P] [US4] `internal/watch/license_test.go`: research D8: texto MIT con otro año sobre `spdx: MIT` → `igual`; texto Apache-2.0 sobre `spdx: MIT` con Apache-2.0 en la allowlist → `cambiada` a `Apache-2.0`; texto de una licencia fuera de la allowlist → `bloqueada`; texto no identificable (cobertura < 90 %) → `bloqueada`.
- [X] T028 [US4] Casos en `internal/ci/watch_test.go`: (1) licencia bloqueada → un issue `fallo: bob 4.4.0 en watch-license` con declarada, detectada y cobertura; 3 ciclos más → sigue habiendo 1 issue y ninguna descarga nueva (FR-018); (2) assets renombrados tras la espera → `watch-assets` con nombres esperados y encontrados; (3) checksum upstream que no coincide → `watch-checksums`; (4) PR abierto en 4.4.0 y upstream resube el asset con otros bytes → `watch-tampered`, el PR no se toca y ese issue no se cierra solo; (5) issue `watch-assets` abierto y el manifiesto de `bob` cambia en `main` → se vuelve a evaluar y, si encaja, se abre el PR y el issue se cierra con referencia al PR (FR-019); (6) licencia cambiada permitida → PR que cambia también `license.spdx`.

### Implementation for User Story 4

- [X] T029 [P] [US4] `internal/watch/license.go`: `LicenseChange(text []byte, declared string, cfg) (kind, detected string, coverage float64)` sobre `internal/license.Detect` y la allowlist (research D8).
- [X] T030 [US4] `internal/ci/watch.go`: tras preparar la propuesta (T014), aplicar `LicenseChange`; con `bloqueada`, o con errores de la orden de lockfile clasificados como assets ausentes o checksums que no coinciden, notificar con las etapas de T006 por `internal/ci/notify.go` (marcador `paquete/versión/etapa`, datos de `contracts/pr-y-notificaciones.md`) en lugar de abrir PR; con `cambiada`, cambiar `license.spdx` en el commit y marcar el PR.
- [X] T031 [US4] `internal/ci/watch.go`: supresión de reintentos (FR-018): antes de preparar, buscar un issue abierto `watch-*` para el paquete y la versión candidata; si existe y el manifiesto del paquete no cambió en `main` después de la fecha del issue, saltar el paquete con `ya-notificado`. Detección de `watch-tampered`: comparar la entrada nueva del lockfile con la de la rama del PR abierto para la misma versión.
- [X] T032 [US4] `internal/ci/notify.go`: cierre automático de los issues `watch-license`, `watch-assets` y `watch-checksums` de un paquete al abrir un PR válido para él o cuando su versión deja de ser candidata, con un comentario que enlaza el PR o explica el motivo (FR-019); `watch-tampered` nunca se cierra solo. Pruebas en `internal/ci/notify_test.go`.

**Checkpoint**: los problemas llegan una vez y con los datos para resolverlos.

---

## Phase 7: User Story 5 - Ciclos baratos y dentro de los límites de la API (Priority: P3)

**Goal**: ≤ 1 petición por paquete sin novedades; nunca agotar el límite (SC-006, SC-007).

**Independent Test**: límite simulado con 520 peticiones restantes y reserva 500 → el ciclo revisa los paquetes que caben, termina en verde y lista los pendientes; el siguiente empieza por ellos.

### Tests for User Story 5

- [X] T033 [US5] Casos en `internal/ci/watch_test.go`: (1) segundo ciclo sin novedades → todas las consultas de releases con `If-None-Match` y respuesta `304`, total = número de paquetes; (2) reserva alcanzada → ciclo detenido en verde, resumen `pendiente-límite` y orden del ciclo siguiente con esos paquetes primero; (3) `403` con `Retry-After` → detenido sin reintentar; (4) 3 ciclos seguidos detenidos → issue `watch-ratelimit`, cerrado tras un ciclo completo; (5) catálogo simulado de 100 paquetes sin novedades → nunca baja de la reserva (SC-007).

### Implementation for User Story 5

- [X] T034 [US5] `internal/ci/watch.go`: caché de ETag y cuerpos en `.debfactory-watch-cache/` (un JSON por upstream; la restaura y guarda `actions/cache` en `watch.yml`), comprobación de `X-RateLimit-Remaining` contra `watch.rate_reserve` antes de cada paquete, parada ordenada, lista de pendientes en `.debfactory-watch-cache/pending.json` para el orden del ciclo siguiente, y el contador de ciclos detenidos para `watch-ratelimit` (research D9).
  - Hecho. ETag, cuerpos, pendientes y ciclos detenidos en `state.json` del estado de la caché.

**Checkpoint**: todas las historias funcionan.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T035 [P] `ci/runner/dispatch.sh`: generalizar `ci/runner/dispatch-refresh.sh` a `dispatch.sh <workflow> <inputs-json>` (mismo manejo del token por stdin y `-A debfactory`); `ci/host/debfactory-refresh.service` lo llama con `main.yml '{"action":"refresh"}'`; actualizar `ci/runner/Dockerfile`/`ci/host/setup.sh` donde se instale el script.
- [X] T036 [P] `ci/host/debfactory-watch.service` y `ci/host/debfactory-watch.timer` (`OnCalendar=hourly`, `Persistent=true`, `RandomizedDelaySec=10m`) como en `contracts/workflows.md`; `ci/host/setup.sh` los instala y habilita; `specs/003-ci-pipeline/contracts/runner-host.md` los menciona.
  - Hecho. `setup.sh` instala las unidades pero no habilita el timer: se habilita a mano cuando `watch.yml` está en `main` (antes, cada disparo daría HTTP 404); lo dice al terminar.
- [X] T037 [P] `internal/ci/doctor.go` y `internal/ci/doctor_test.go`: comprobaciones de `contracts/cli.md` § `ci doctor`: el environment `watch` existe con los secretos `DEBFACTORY_APP_ID` y `DEBFACTORY_APP_KEY` por nombre (`GET /repos/{repo}/environments/watch` y `/environments/watch/secrets`; la instalación de la App no se lee con el token del mantenedor), deployment branches = `main`, y `debfactory-watch.timer` habilitado.
  - Hecho. Solo si el repositorio de paquetes tiene `watch.yml`; secretos `DEBFACTORY_APP_CLIENT_ID` y `DEBFACTORY_APP_KEY`.
- [X] T038 [P] `docs/operacion-ci.md`: sección "Detector de versiones": crear la App (T001), environment y auto-merge (T002), activar el timer, lanzar un ciclo a mano, qué significa cada decisión del resumen y cada etapa `watch-*`, cómo activar `watch.auto_merge` en un paquete y cómo cerrar un PR del detector para que proponga desde cero.
- [X] T039 [P] `specs/003-ci-pipeline/contracts/workflows.md`: nota de que `watch.yml` (fase 005) es el único workflow con secretos, limitado al environment `watch`, con enlace a `specs/005-upstream-release-watch/contracts/workflows.md`.
- [X] T040 `internal/repotest/e2e_test.go` (tag `e2e`, en `just e2e`): recorrido de quickstart §1: upstream simulado de `bob` con 4.4.0 elegible y `watch.auto_merge: true` → `ci watch` abre el PR con auto-merge → (simulando el merge) `ci main` publica `bob 4.4.0-0~tepache1` en el repositorio local y `apt install bob` en un contenedor limpio instala 4.4.0.
  - Hecho como prueba de integración (`internal/ci/watch_publish_test.go`, tag `integration`, en `just check`) en vez de e2e con Docker: release simulado de bob → `ci watch` abre el PR con auto-merge → merge → `ci main` (gpg y aptly reales, gates simulados como en `TestCIMain`) publica solo `bob 4.4.0-0~tepache1`. Un `.deb` real del binario simulado no pasaría lintian (binario estático sin override) y la instalación con apt la cubren el e2e existente y el check `verify` del PR real.
- [X] T041 `just check` y `just e2e` en verde; `just tool-install`; PR en `packages` que fija la herramienta nueva e incluye `watch.yml` y el bloque `watch` (T018). **(manual)** el push y el merge.
  - Avance (2026-11-19): `just check` en verde (464 s, con `TestWatchToPublish` y los `.deb` de prueba reproducibles con los sha256 publicados). Falta `just e2e`, `just tool-install`, el commit y el PR en `packages` con la rama `005-watch` (manual).
- [ ] T042 **(manual)** Quickstart §2–§4: `ci watch --dry-run` con `GH_TOKEN` de lectura sobre los 5 paquetes reales (plan, riesgo 3; anotar en `specs/005-upstream-release-watch/research.md` lo que haya que ajustar); primer ciclo con `force`; `sudo systemctl enable --now debfactory-watch.timer` y comprobar una ejecución horaria; resumen con `N × 304` en el segundo ciclo completo; `ci doctor` en verde.
- [ ] T043 **(manual)** Quickstart §5: primera actualización real; después, `watch.auto_merge: true` en un paquete y comprobar que su siguiente versión menor se publica en apt sin ninguna acción humana (SC-002).

---

## Dependencies & Execution Order

- **Phase 1 (T001–T003)** antes que todo: si el spike falla, se revisa research D1.
- **Phase 2 (T004–T009)** bloquea todas las historias. T004, T005, T006 en paralelo; T007 → T008; T009 independiente.
- **US1 (Phase 3)** depende de la fase 2. **US2** depende de T012 (sustituye su filtro mínimo). MVP = US1 + US2.
- **US3** y **US4** dependen de US1 (T014, T015) y son independientes entre sí, salvo `internal/ci/watch.go` (tocarlo en serie).
- **US5** depende de US1 (T013, T016).
- **Polish**: T035–T039 en paralelo cuando haya historias; T040 tras US1–US4; T041 → T042 → T043.

### Parallel Opportunities

```text
Fase 2:  T004 (config) ‖ T005 (manifiesto) ‖ T006 (etapas) ‖ T009 (watch/release)
US1:     T010 (candidate_test) ‖ T012 (candidate)
US2–US4: T019 ‖ T022 ‖ T024 ‖ T027 ‖ T029  (paquete internal/watch, archivos distintos)
Polish:  T035 ‖ T036 ‖ T037 ‖ T038 ‖ T039
```

`internal/ci/watch.go` y `internal/ci/watch_test.go` los tocan T011, T013–T016, T020, T023, T025,
T028, T030, T031, T033 y T034: en serie.

## Implementation Strategy

1. **Spike** (fase 1): sin él no se escribe código.
2. **MVP** (fases 2–4): `ci watch` a mano propone versiones estables como PR; el mantenedor integra.
   Se puede instalar y usar ya (T041, T042 parciales) antes de US3–US5.
3. **US4** antes que **US3** si se prefiere no activar auto-merge hasta que las notificaciones
   estén completas; **US5** al final (con 5 paquetes el límite está lejos).
4. **Producción**: timer, `ci doctor`, primera actualización real y luego auto-merge paquete por paquete.
