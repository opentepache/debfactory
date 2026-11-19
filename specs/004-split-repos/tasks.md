---
description: "Task list for 004-split-repos"
---

# Tasks: Separar la herramienta y los paquetes en dos repositorios

**Input**: Design documents from `/specs/004-split-repos/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: se incluyen donde la spec los exige:

- la autocomprobación de la versión y la etapa `tool` (FR-005);
- el lanzador (FR-005, FR-006);
- el campo `tool` de la auditoría (FR-014, FR-015);
- la comparación al subir la herramienta y el enlace de diferencias (FR-008, FR-009);
- que las pruebas pasen sin el repositorio de paquetes (FR-019).

Como en 003: los tests Go viven junto al código; los que usan gpg, aptly o git reales llevan el
tag `integration` y los que usan Docker el tag `e2e`. La API de GitHub se simula con `httptest`.
Lo que se hace contra GitHub o como root en la PC es **(manual)**.

**Organization**: por historia de usuario. La fase 2 ataca primero el riesgo 1 del plan (pruebas
sin la raíz real), porque valida que la herramienta es separable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: se puede hacer en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia de usuario de la spec (US1–US5)

---

## Phase 1: Setup

**Purpose**: decisiones y herramientas que la migración necesita.

- [X] T001 **(manual)** Elegir la licencia del repositorio público `packages` y anotarla en `specs/004-split-repos/research.md` D8: MIT (2026-11-16).
- [X] T002 **(manual)** `sudo apt install git-filter-repo` en la PC y comprobar `git filter-repo --version`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: la herramienta se prueba con datos propios y conoce su versión. Bloquea todas las historias.

- [X] T003 Crear `testdata/project/` con una copia literal de `config/debfactory.yaml`, `config/licenses.yaml`, `packages/*.yaml`, `repo/state.yaml` y `docs/usuario.md` del commit actual, y un `testdata/project/README.md` de dos líneas: "copia congelada de opentepache/packages en la migración (004 D9); no se sincroniza".
- [X] T004 Crear `internal/testutil/project.go` (el paquete `internal/testutil` ya existe) con `func ProjectRoot(t testing.TB) string`, que devuelve la ruta absoluta de `testdata/project` calculada desde `runtime.Caller` (no depende del directorio de trabajo del test).
- [X] T005 [P] Cambiar a `testutil.ProjectRoot(t)` las pruebas unitarias que leen la raíz real: `internal/config/config_test.go` (`Load("../..")`, `"../../" + MainFile`, `"../../" + LicensesFile`), `internal/manifest/semantic_test.go`, `internal/manifest/examples_test.go` (`"../../packages/*.yaml"`), `internal/license/gate_test.go`, `internal/sync/plan_test.go`, `internal/ci/env_test.go`, `internal/ci/doctor_test.go` (`Root`), `internal/ci/plan_test.go` (`ReadState`, `PlanMain`, el `cp -a`), `internal/ci/audit_test.go` (`ReadState`), `internal/ci/statepr_test.go` (`"../../repo/state.yaml"`), `internal/repo/state_test.go`, `cmd/debfactory/main_test.go` (`tempRoot`). Las rutas a `testdata/...` que ya usan `"../../testdata/..."` no cambian.
- [X] T006 [P] Cambiar a `testutil.ProjectRoot(t)` las pruebas de integración y e2e: `internal/ci/run_test.go` (`newMainFixture` copia `config`, `packages` y `schema` desde la raíz: copiar `config` y `packages` desde `testdata/project`; no copiar `schema/`: el esquema va embebido en el binario, `schema/embed.go`), `internal/repo/publish_test.go`, `internal/sign/keyring_test.go`, `internal/installtest/run_test.go`, `internal/build/harness_test.go` (`const repo`), `internal/repotest/env_test.go` (`e2eEnv` copia `config`, `packages` y `docs` desde `testdata/project`; `dist` sigue en la raíz de la herramienta), `internal/repotest/guide_test.go`. `internal/repotest/zz_secrets_test.go` sigue escaneando el repositorio real de la herramienta (`realRoot`).
- [X] T007 `internal/repotest/rebuild_test.go`: hoy clona la raíz real para reconstruir desde la historia de git. Sustituirlo por un repositorio git temporal creado en el test (`git init`, copia de `testdata/project`, commit) y clonar ese.
- [X] T008 `justfile`: variable `root := env("DEBFACTORY_ROOT", "../packages")` y `--root {{root}}` en `build`, `build-all`, `lint`, `test`, `lock`, `validate`, `publish`, `retire`, `sync`, `ci-doctor`, `ci-local-pr`; en `check` y `e2e`, `validate`, `build --all --check-reproducible` y `test --all` usan `--root testdata/project` y `repo verify` solo corre si existe `{{root}}`.
- [X] T009 Crear `internal/version/version.go` (`var Commit = "dev"`) y la orden `debfactory version` en `cmd/debfactory/main.go` (imprime `version.Commit`, código 0), con su caso en `cmd/debfactory/main_test.go` y en el texto de ayuda.
- [X] T010 Comprobar el riesgo 1: `git clone` del repositorio en un directorio temporal sin `../packages` y ejecutar allí `go vet ./...`, `go test ./...`, `go test -tags integration ./...` y `just check`; todo en verde (FR-019, SC-008). Anotar el resultado en `specs/004-split-repos/research.md` §3.

**Checkpoint**: la herramienta es separable.

---

## Phase 3: User Story 1 - El repositorio de paquetes publica con la herramienta que declara (Priority: P1) 🎯 MVP

**Goal**: `ci` usa la versión fijada en `debfactory.version`, lo comprueba y lo registra en la auditoría.

**Independent Test**: TestCIMain con `debfactory.version` en la fixture: publica, y cada entrada de auditoría lleva `tool`; con otra versión, falla en la etapa `tool` antes de hacer nada.

- [X] T011 [P] [US1] `internal/report/errors.go`: etapa nueva `Tool Stage = "tool"`.
- [X] T012 [P] [US1] `internal/config/config.go`: `CI.ToolRepository string` (`yaml:"tool_repository"`, validado como `ci.repository`, vacío permitido) con su caso en `config_test.go`; en `testdata/project/config/debfactory.yaml`, `ci.code_paths: [config/, debfactory.version]` y `ci.tool_repository: opentepache/debfactory`.
- [X] T013 [US1] Autocomprobación (contracts/herramienta.md § Autocomprobación): `internal/ci/tool.go` con `func CheckTool(root string, env *Env) error` — si `GITHUB_ACTIONS=true`, lee `<root>/debfactory.version`, valida `^[0-9a-f]{40}$` y lo compara con `version.Commit`; error en la etapa `tool` con `soy <c1> y packages fija <c2>`. Llamarla al principio de `PR`, `Main`, `Dispatch` y `Refresh`, como `InputError` (código 2). `NotifyCmd` no la llama: tiene que poder avisar justo de ese fallo. `CheckTool` lee siempre `<root>/debfactory.version` de la raíz del checkout, nunca de un worktree de otro commit (`planRebuild`, `worktree` en `internal/ci/manual.go`). Pruebas unitarias en `internal/ci/tool_test.go`: igual, distinto, `dev`, formato inválido, archivo ausente, fuera de CI.
- [X] T014 [US1] Auditoría (data-model.md §3): campo `Tool string` en `Entry` (`internal/ci/audit.go`), serializado justo después de `commit` y omitido si está vacío; `ParseAudit` lo acepta opcional; `CheckAudit` falla si está y no es 40 hex; todas las entradas que crea `ci` (publish, rebuild, retire, import) lo rellenan con `version.Commit`. Casos nuevos en `internal/ci/audit_test.go` (entrada antigua sin `tool` válida, `tool` inválido rechazado, orden de campos).
- [X] T015 [US1] Fixture de integración: `internal/ci/run_test.go` escribe `debfactory.version` en el repositorio de la fixture con un commit de prueba, fija `version.Commit` a ese valor durante el test y pone `GITHUB_ACTIONS=true` en el `Env`. `TestCIMain` comprueba `tool` en cada entrada de la auditoría y un caso nuevo `TestCIMainWrongTool` (otro commit) que falla en la etapa `tool` sin tocar `<data>` ni la forja.
- [X] T016 [P] [US1] Lanzador `ci/runner/launcher.sh` (contracts/herramienta.md § Lanzador; base de las herramientas por `DEBFACTORY_TOOLS`, por defecto `/opt/debfactory/tools`, para poder probarlo) y `ci/runner/launcher_test.sh` (sin archivo, formato inválido, versión no instalada con la lista y la orden, versión instalada que recibe los argumentos intactos); añadir la prueba a `just ci-lint`.
- [X] T017 [US1] `ci/runner/Dockerfile`: quitar `golang-1.26-go`, `GOTOOLCHAIN`, `GOFLAGS` y la ruta de Go del `PATH`; copiar `launcher.sh` a `/usr/local/bin/debfactory` (755).
- [X] T018 [P] [US1] `ci/runner/run-runner.sh`: montaje `--mount type=bind,src=/srv/debfactory/tools,dst=/opt/debfactory/tools,readonly` en los dos niveles. `ci/host/setup.sh`: `install -d -m 755 -o root -g root /srv/debfactory/tools`.
- [X] T019 [US1] `justfile`: receta `tool-install` (contracts/herramienta.md § tool-install): árbol limpio, `just check`, `just e2e`, `go build -trimpath -ldflags "-X debfactory/internal/version.Commit=$(git rev-parse HEAD)"`, `sudo install -D -m 755 -o root -g root` en `/srv/debfactory/tools/<commit>/debfactory`, y los mensajes del contrato. Si ya existe con el mismo sha256, no hace nada; si existe distinto, lo reemplaza y avisa.
- [X] T020 [US1] `.github/workflows/pr.yml` y `main.yml`: quitar el paso "compilar debfactory" y llamar a `debfactory` (el lanzador) en lugar de `bin/debfactory`; quitar `specs/**` de `paths-ignore`. Se editan aquí para que la historia que hereda `packages` (T036) ya los tenga.
- [X] T021 [US1] **(manual)** Spike del riesgo 2 del plan con el Docker rootless de `dfci`, como T019 de 003: `just runner-image`, `sudo ci/host/setup.sh --runner-tag <nueva>`, `just tool-install`, y en un contenedor de la imagen `ci` con el montaje de `run-runner.sh`, desde un directorio con `debfactory.version`: `debfactory version` imprime el commit, `touch /opt/debfactory/tools/x` falla y con un commit no instalado sale el mensaje del contrato. Anotar en research.md §3.

**Checkpoint**: la herramienta fijada funciona de punta a punta en local.

---

## Phase 4: User Story 2 - Actualizar la herramienta es un cambio revisado (Priority: P1)

**Goal**: subir `debfactory.version` reconstruye y compara todo lo publicado y enlaza las diferencias.

**Independent Test**: TestCIPR con un PR que solo cambia `debfactory.version`: compara todo, pasa si los `.deb` son iguales, falla nombrando el paquete si no, y el resumen tiene el enlace.

- [X] T022 [US2] Quitar la etapa `code`: `codeChecks` de `internal/ci/run.go` y sus llamadas en `PR` (`internal/ci/pr.go`) y en el paso 3b de `Main`; el paso 3b solo compara (`compare`) cuando `CodeChanges` encuentra rutas de `ci.code_paths`. Quitar `report.Code`. Ajustar `internal/ci/plan_test.go` y `pr_test.go`.
- [X] T023 [US2] Enlace de diferencias (contracts/herramienta.md § Enlace): en `ci pr`, si `debfactory.version` cambia entre `--base` y `HEAD`, el resumen empieza con `Herramienta: <a12> → <b12>` y, si `ci.tool_repository` no está vacío, `Diferencias: https://github.com/<repo>/compare/<a>...<b>`. Función pura en `internal/ci/pr.go` con prueba unitaria.
- [X] T024 [US2] Integración en `internal/ci/pr_test.go`: `TestCIPRToolBump` (PR que solo cambia `debfactory.version`, con `version.Commit` igual al nuevo: compara todos los publicados, pasa, resumen con el enlace) y `TestCIPRToolBumpChanged` (con `f.alter` en un paquete: falla nombrándolo, como FR-010 de 003).
- [X] T025 [US2] Integración en `internal/ci/run_test.go`: `TestCIMainToolBump` — tras publicar, un merge que cambia `debfactory.version` compara en el paso 3b y no publica nada; la siguiente ejecución sin cambios no vuelve a comparar.
- [X] T025a [US2] Integración en `internal/ci/manual_test.go`: `TestCIRebuildAfterToolBump` — publicar fnm, integrar un cambio de `debfactory.version` (con `version.Commit` igual al nuevo) y lanzar `rebuild` de la versión publicada antes del cambio: usa la herramienta de `HEAD`, la autocomprobación no falla por el `debfactory.version` antiguo del worktree y termina con "sha256 iguales; nada que publicar".
- [X] T026 [US2] **(manual)** Quickstart §4 en `packages` tras la migración: versión no instalada (< 1 min, SC-004), subida que no cambia nada (con push a `main` mientras el PR está abierto: SC-005), y subida que cambia un `.deb`. Anotar la duración del check frente a la de 003 (plan, Performance Goals).
  - Resultado (2026-11-17, packages #2–#5): versión no instalada → falla en 9 s con «no está instalada» (#2); H2 `4f1fb36` sin cambios de Go → pasa en 5 min 34 s, enlace de diferencias correcto (#3); push a `main` con #3 abierto reconstruye con `4e258de` y no publica (SC-005); H3 `da15ea8` (comentario en `debian/copyright`) → falla en 5 min 40 s nombrando los 8 `.deb` con «sube revision» (#5). Duración: sin medición equivalente de 003 en este runner; queda 5½ min como referencia (`main` con cachés calientes: 1 min 53 s).
  - Hallazgo: el check del #4 dio verde sin probar nada porque se integró antes de que corriera y `pr.yml` compara contra `origin/main` en vez de `pull_request.base.sha`; además el ruleset no exige `verify` (y exigirlo hoy bloquearía los PR de estado y de solo docs). Arreglo en T026a.
- [ ] T026a [US2] Check `verify` obligatorio sin bloqueos: `pr.yml` compara contra `github.event.pull_request.base.sha` y sin `paths-ignore`; `ci main` publica `verify=success` en la punta de la rama de estado (`internal/ci/statepr.go`, `statuses: write` en `main.yml`); `ci doctor` exige la regla en el ruleset; `docs/operacion-ci.md` y `specs/003-ci-pipeline/contracts/workflows.md`. Cierra al fijar la nueva herramienta en `packages` y ver un PR de solo docs en verde.
  - Avance (2026-11-19): herramienta `9cd4a9b` fijada (packages #6); el PR de estado de k9s (#8) recibió `verify=success` de la ejecución de `main` y se integró. Sin `paths-ignore`, `pr.yml` también se disparó en #8 (ejecución sin jobs esperando aprobación, `failure` al integrar); `pr.yml` vuelve a ignorar solo `repo/state.yaml` y `repo/audit.jsonl`. Falta: un PR de solo docs en verde.

**Checkpoint**: subir la herramienta es un PR revisado con red de seguridad.

---

## Phase 5: User Story 3 - El código de la herramienta nunca sale de la PC (Priority: P1)

**Goal**: ningún job puede obtener el código ni modificar la herramienta; `ci doctor` lo comprueba.

**Independent Test**: `TestDoctorHost` detecta un `/srv/debfactory/tools` escribible y la versión fijada sin instalar; quickstart §5 desde un fork.

- [X] T027 [P] [US3] Versión del runner embebida (research D12): `ci/runner/embed.go` (`package runner`, `//go:embed runner.env`, `var Env string`) y `internal/ci/doctor.go` la usa en lugar de leer `<root>/ci/runner/runner.env`; ajustar `doctor_test.go`.
- [X] T028 [US3] `ci doctor`, comprobaciones nuevas del host en `internal/ci/doctor.go` (`HostChecks`, con la `Host` inyectable): `/srv/debfactory/tools` existe, es de root y no lo pueden escribir el grupo ni otros; la versión de `<root>/debfactory.version` está instalada; aviso (no fallo) con las versiones instaladas que no fija `<root>/debfactory.version`; el aviso no consulta los PRs abiertos y lo dice en el mensaje (`no fijada por main; revisa los PRs abiertos antes de borrarla`). Casos en `doctor_test.go`.
- [X] T029 [P] [US3] `ci/runner/hooks/trusted-gate_test.sh`: caso explícito de un job de otro repositorio (`GITHUB_REPOSITORY=opentepache/debfactory` con `DEBFACTORY_REPOSITORY=opentepache/packages`) rechazado (FR-012), si no existe ya.
- [ ] T030 [US3] **(manual)** Quickstart §5 desde la cuenta secundaria contra `packages` (SC-003). Sustituye a T047 de 003 y anota allí que se hizo en esta fase.

**Checkpoint**: aislamiento verificado.

---

## Phase 6: User Story 4 - Migrar sin que el usuario final note nada (Priority: P2)

**Goal**: dos repositorios con su historia; nada publicado cambia.

**Independent Test**: contracts/migracion.md pasos 1–9 con sus comprobaciones; quickstart §6.

- [X] T031 [P] [US4] `ci/migrate/packages/README.md`: qué es OpenTepache y este repositorio; cómo contribuir (abrir un PR con el manifiesto; mientras la herramienta sea privada, el mantenedor genera el lockfile en la rama del PR, research D10); el principio V de la constitución 1.7.0 (repositorios comunitarios gratuitos, sin registro y al día; lista en `config/debfactory.yaml`) y la regla de solo versiones estables; enlace a `docs/usuario.md`. Y `ci/migrate/packages/LICENSE` con la licencia de T001.
- [X] T032 [P] [US4] `ci/migrate/split.sh` (contracts/migracion.md pasos 2–4): clona el repositorio actual en un directorio vacío dado, ejecuta `git filter-repo --path packages/ --path config/ --path repo/ --path docs/usuario.md --path .github/`, copia `ci/migrate/packages/*`, escribe `debfactory.version` con el commit pedido, ajusta en `config/debfactory.yaml` `ci.repository: opentepache/packages`, `ci.code_paths` y `ci.tool_repository`, hace el commit con la identidad configurada del mantenedor (sin reescribir la autoría anterior) y ejecuta `debfactory validate --root <dir>` y `ci plan`. No crea nada en GitHub ni hace push.
- [X] T033 [P] [US4] `ci/host/migrate-data.sh`: opción `--root DIR` (checkout de `packages`) para el `repo verify` como `dfpub`, en lugar de `git archive HEAD` del repositorio de la herramienta.
- [X] T034 [P] [US4] Documentación: `docs/operacion-ci.md` (dos repositorios, `tool-install`, cómo subir la herramienta, cómo podar versiones —antes de borrar una, comprobar que ningún PR abierto la fija—, contribuciones externas y lockfiles, y en § Recuperación la fila "un job falla en el lanzador o en la etapa `tool`: `ci notify` tampoco puede correr porque usa el mismo lanzador; solo llega el correo de GitHub por la ejecución fallida; instala la versión fijada con `just tool-install` en ese commit y relanza"), `README.md` de la herramienta (qué vive dónde) y los contratos de 003 según contracts/cambios-003.md (`cli.md`, `workflows.md`, `runner-host.md`, `notificaciones.md`, `data-model.md` §4), con una nota que remite a 004.
- [X] T035 [US4] **(manual)** Migración, pasos 1–4 de contracts/migracion.md: `just tool-install` desde `main` y `ci/migrate/split.sh ~/Develop/packages <commit>`; revisar `git log --stat` (solo las rutas conservadas, autoría intacta) y que `ci plan` no tenga nada que publicar (SC-002).
- [X] T036 [US4] **(manual)** Pasos 5–6: crear `opentepache/packages` público, `git push`, ruleset en `main` (PR, sin force push, sin borrado, sin bypass), "Require approval for all external contributors", workflows en solo lectura con "Allow GitHub Actions to create and approve pull requests", etiqueta con `just ci-doctor --fix`. PAT nuevo solo para `packages` y `sudo ci/host/setup.sh --repository opentepache/packages --token-stdin`; revocar el PAT anterior si existía. `GH_TOKEN=$(gh auth token) just ci-doctor` en verde (SC-007). Sustituye a T020 y T045 de 003.
- [X] T037 [US4] Paso 7 en `debfactory`: commit que borra `packages/`, `config/`, `repo/`, `docs/usuario.md` y `.github/` (lo que queda en `testdata/project` no se toca), y `ci.repository`/`code_paths` dejan de existir en este repositorio. Repetir T010 en un clon limpio.
- [X] T038 [US4] **(manual)** Pasos 8–9: completar T021 de 003 (spike del hook) contra `packages`, T046 de 003 (`migrate-data.sh --root ~/Develop/packages`, `ci.destination: r2`), arrancar los runners; la primera ejecución de `main` no publica nada; `repo verify --dest r2` con el mismo serial y `apt update` sin avisos en las 5 suites (SC-001, quickstart §6).

**Checkpoint**: estructura nueva en producción.

---

## Phase 7: User Story 5 - La herramienta se prueba sin el repositorio de paquetes (Priority: P2)

**Goal**: `tool-install` es la barrera de calidad del repositorio privado.

**Independent Test**: quickstart §1 y §2.

- [X] T039 [US5] **(manual)** Quickstart §2: `tool-install` se niega con un cambio sin commit y con una prueba rota, y no instala nada; con árbol limpio instala con dueño root y permisos 755.
- [X] T040 [US5] **(manual)** Quickstart §1 en un clon limpio desde GitHub (SC-008).

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T041 [P] `specs/003-ci-pipeline/tasks.md`: nota al principio de las tareas pendientes (T020, T021, T033, T039, T045–T047, T053, T058, T064, T068–T070) de que se ejecutan contra `opentepache/packages` tras la fase 004, con la correspondencia de las que esta fase sustituye (T030, T036, T038).
- [X] T042 [P] `gofmt -l`, `go vet ./...` (también con `-tags integration` y `-tags e2e`), `go test ./...`, `go test -tags integration ./...` y `just ci-lint` en verde.
- [X] T043 **(manual)** Quickstart §3 (US1, SC-006) y §7 (SC-007) en producción.
  - Resultado (2026-11-19): §3 con un paquete nuevo en lugar de la revisión de fnm: k9s 0.51.0 (packages #7; zellij descartado por `embedded-library curl`, etiqueta que no se silencia). Check del PR en 7 min 28 s; `main` (44433391047, 5 min 30 s) publicó solo k9s con `9cd4a9b`, serial 1 → 2; apt instala k9s en las 5 suites; el PR de estado #8 se integró y la siguiente ejecución no publica nada. §7: `ci doctor` en verde salvo los avisos esperados (PAT sin root, versiones de herramienta sin fijar).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: sin dependencias; T001 solo bloquea T031 y la migración.
- **Foundational (Phase 2)**: bloquea todo. T003 → T004 → (T005, T006, T007 en paralelo) → T008 → T010; T009 en paralelo con T005–T007.
- **US1 (Phase 3)**: depende de Foundational. T011, T012, T016, T018 en paralelo; T013 → T014 → T015; T017 después de T016; T019 después de T009; T021 al final.
- **US2 (Phase 4)**: depende de T013–T015 (US1). T022 → T023 → T024 → T025 → T025a; T026 tras la migración (T038).
- **US3 (Phase 5)**: T027 y T029 independientes; T028 tras T027. T030 tras T036.
- **US4 (Phase 6)**: T031–T034 en paralelo cuando US1–US3 estén en código; T035 → T036 → T037 → T038 en serie.
- **US5 (Phase 7)**: tras T019 (T039) y T037 (T040).
- **Polish**: al final; T041 puede hacerse en cualquier momento.

### Parallel Opportunities

- Foundational: T005, T006 y T007; T009.
- US1: T011, T012, T016, T018.
- US3 en paralelo con US2 (archivos distintos: `doctor.go` frente a `pr.go`/`run.go`).
- US4: T031, T032, T033, T034.

## Parallel Example: Foundational

```text
# A la vez, tras T004:
T005 pruebas unitarias → testutil.ProjectRoot
T006 pruebas de integración y e2e → testutil.ProjectRoot
T007 rebuild_test con un repositorio git temporal
T009 internal/version + debfactory version
```

## Implementation Strategy

1. **Riesgo primero**: Phase 2 completa y T010 en verde demuestran que la herramienta es separable sin tocar GitHub.
2. **MVP (US1)**: versión fijada, autocomprobación, auditoría, lanzador, montaje y `tool-install`, probados en local (T021).
3. **US2 y US3** en paralelo: red de seguridad al subir la herramienta y aislamiento.
4. **US4**: la migración es lo último y lo único irreversible en GitHub; hasta T037 se puede volver atrás borrando `opentepache/packages`.
5. **US5 y Polish**: verificaciones finales en producción.
