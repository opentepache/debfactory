---
description: "Task list for 003-ci-pipeline"
---

# Tasks: Pipeline de construcción y publicación

**Input**: Design documents from `/specs/003-ci-pipeline/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: se incluyen donde la spec los exige:

- la conciliación y el plan (US1, US2);
- las transiciones del PR de estado (FR-007a/b);
- los invariantes de la auditoría (FR-024, FR-025);
- la deduplicación de issues (FR-021);
- `Valid-Until` (FR-030);
- la recuperación (SC-007);
- la búsqueda de secretos (SC-003).

Los tests Go viven junto al código (`*_test.go`). Los que usan gpg, aptly o git reales llevan el
build tag `integration`, y los que usan Docker van en `internal/repotest` (tag `e2e`). La API de
GitHub siempre se simula con `httptest`; ningún test habla con github.com. Los escenarios del
quickstart contra el repositorio real de GitHub son tareas manuales del mantenedor, marcadas
como **(manual)**.

**Organization**: por historia de usuario. Cada fase deja algo comprobable por sí solo.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: se puede hacer en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia a la que pertenece (US1…US6)

## Path Conventions

Es el mismo módulo Go de las fases 001 y 002. Lo nuevo es (plan.md § Project Structure):

- `internal/ci/` y `internal/forge/`;
- `cmd/debfactory/ci.go`;
- `.github/workflows/{pr,main}.yml`;
- `ci/runner/` y `ci/host/`;
- `docs/operacion-ci.md`;
- `repo/audit.jsonl`.

Entorno: `PATH=/usr/lib/go-1.26/bin:$PATH`, `GOTOOLCHAIN=local`. Los scripts de `ci/` son POSIX
sh y pasan `sh -n` y `checkbashisms` (paquete `devscripts`).

Convenciones que usan varias tareas:

- **`<data>`**: la opción global `--data`. En la PC pasa a ser `/srv/debfactory/data` (dueño
  `dfpub`), con el subdirectorio nuevo `ci/` (`state.yaml`, `audit.jsonl`, `pushed.json`;
  data-model.md §3).
- **Nivel**: `ci` (usuario `dfci`, etiqueta `debfactory-pr`) o `pub` (usuario `dfpub`, etiqueta
  `debfactory-main`).
- **Errores**: formato de la fase 001 (`internal/report`). Etapas nuevas: `plan`, `state`,
  `audit`, `notify`, `expiry`, `state-pr-age`, `config`, `code` y `compare` (data-model.md §1). Códigos de salida
  0/1/2/3 de contracts/cli.md.
- **Secretos**: ningún test, script ni log imprime la subllave, la frase de paso, `rclone.conf`,
  el PAT ni `GITHUB_TOKEN`. El PAT solo entra por la entrada estándar de `setup.sh`.
- **Red**: toda llamada a GitHub usa `User-Agent: debfactory`, y `GITHUB_TOKEN` solo se envía a
  `api.github.com` (o a la URL del servidor falso en los tests) y al remoto git de GitHub.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: configuración, etapas de error y esqueleto de directorios.

- [X] T001 Añadir a `config/debfactory.yaml` la sección `repo.validity` (`days: 30`, `warn_days: 7`) y la sección `ci` exactamente como en contracts/config.md:
  - `repository: ""` (se rellena en T020, cuando exista el repositorio en GitHub; hasta entonces la validación de `ci` solo se aplica en las órdenes `ci`), `destination: r2`, `state_branch: debfactory/state`;
  - `state_pr_max_age: 7d`, `failure_label: pipeline-failure`;
  - `labels.ci: debfactory-pr`, `labels.pub: debfactory-main`;
  - `code_paths` con `cmd/`, `internal/`, `config/`, `schema/`, `ci/`, `go.mod` y `go.sum`.

  Copiar el bloque de ejemplo a `specs/002-signed-apt-repo/contracts/config.md`, con una línea que remita a 003.
- [X] T002 [P] Añadir a `internal/report/errors.go` las etapas `Plan`, `State` (ya existe: reutilizarla), `Audit`, `Notify`, `Expiry`, `StatePRAge` (`"state-pr-age"`), `Config`, `Code` y `Compare`, si no existen. Documentar en `EnvError` (código 3) que también lo usan el nivel de confianza equivocado y el entorno de GitHub incompleto.
- [X] T003 [P] Crear los directorios `ci/runner/`, `ci/runner/hooks/`, `ci/host/` y `.github/workflows/`. Añadir `ci/runner/*.tar.gz` a `.gitignore` (el tarball del runner nunca se versiona).
- [X] T004 [P] Añadir al `justfile` las recetas de contracts/cli.md § justfile:
  - `runner-image`: `docker build --target ci` y `--target pub` de `ci/runner/Dockerfile`, etiquetadas `debfactory-runner:<versión-runner>-<commit corto>-{ci,pub}`;
  - `ci-doctor`: `bin/debfactory ci doctor`;
  - `ci-local-pr`: `bin/debfactory ci pr --base origin/main`;
  - `ci-lint`: `sh -n` y `checkbashisms` sobre `ci/**/*.sh`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: los cuatro riesgos de research §3, la firma con `Valid-Until`, los runners en la PC, el cliente de GitHub y el contexto del job. Todo lo comparten US1–US6.

**⚠️ CRITICAL**: ninguna historia empieza antes de terminar esta fase. Los riesgos (T011–T014, T019) van antes que `internal/ci`.

### Configuración y contexto

- [X] T005 Ampliar `internal/config/config.go`:
  - `Repo.Validity{Days int, WarnDays int}` con valores por defecto 30 y 7;
  - el tipo `CI{Repository, Destination, StateBranch, StatePRMaxAge time.Duration, FailureLabel, Labels{CI, Pub}, CodePaths []string}`; `state_pr_max_age` admite el sufijo `d` (días) además de los de `time.ParseDuration`.

  Validar con las 6 reglas y los mensajes exactos de contracts/config.md (por ejemplo, `config: repo.validity.days debe estar entre 8 y 90` y `config: ci.state_branch no puede ser main`). Un caso por regla en `internal/config/config_test.go`.
- [X] T006 [P] Crear `internal/ci/env.go`:
  - `type Env{EventName, Ref, SHA, Repository, RunID, ServerURL, Token, StepSummary, Trust string}`, leído de `GITHUB_*`, `RUNNER_LABELS` (contiene `ci.labels.pub` → `Trust="pub"`, `ci.labels.ci` → `"ci"`) y, para pruebas locales, de `--env-file` (formato `CLAVE=valor`, una por línea);
  - `RunURL()` = `<ServerURL>/<Repository>/actions/runs/<RunID>`;
  - `RequireWriter(cfg)`: devuelve `report.EnvError` (código 3) salvo que `Trust=="pub"`, `EventName` ∈ {`push`, `workflow_dispatch`}, `Ref=="refs/heads/main"` y `Repository==cfg.CI.Repository` (data-model.md §1, segunda barrera tras el hook);
  - `Inputs()`: lee `INPUT_ACTION`, `INPUT_PACKAGE`, `INPUT_VERSION`, `INPUT_REASON` e `INPUT_LAST` y los valida (acción en {`rebuild`, `retire`, `refresh`, `restore-state-pr`}, nombre de paquete con la regex de manifiestos, versión Debian válida, motivo de una línea no vacío en `retire`); un input inválido es código 2.
  - `String()` nunca incluye `Token`.

  Tests en `internal/ci/env_test.go`: cada regla de `RequireWriter`, inputs con `;`, `$(…)` y saltos de línea rechazados, y `fmt.Sprintf("%+v", env)` sin el token.
- [X] T007 [P] Crear `internal/forge/github.go`, un cliente REST mínimo con `net/http`:
  - `New(baseURL, repo, token)`; cabeceras `Authorization: Bearer`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28` y `User-Agent: debfactory`;
  - se niega a enviar el token si el host de `baseURL` no es `api.github.com`, salvo que `baseURL` sea `http://127.0.0.1:*` (tests);
  - issues: `ListOpenIssues(label)` paginado por `Link`, `CreateIssue`, `CommentIssue`, `CloseIssue`;
  - PRs: `FindOpenPR(head, base)`, `CreatePR`, `UpdatePR`, `GetPR`;
  - refs: `GetRef`, `DeleteRef`;
  - configuración (para `ci doctor`): `GetActionsPermissions`, `GetWorkflowPermissions`, `ListRulesets`, `GetLabel`, `CreateLabel`;
  - `Dispatch(workflow, ref, inputs)`.

  Los errores HTTP incluyen método, ruta y código, nunca cabeceras ni el token; el cuerpo de error pasa por `redact.Lines` con el token como secreto. Tests en `internal/forge/github_test.go` contra `httptest`: paginación, cabeceras, rechazo de un host ajeno y un error 500 cuyo texto no contiene el token.
- [X] T008 Crear `cmd/debfactory/ci.go` con el despachador de `debfactory ci <orden>`: registra `plan`, `pr`, `main`, `rebuild`, `retire`, `refresh`, `restore-state-pr`, `notify`, `audit check` y `doctor`, con las opciones de contracts/cli.md y las globales después de la orden. Las órdenes no implementadas todavía salen con código 2 y `no implementado`. Añadir `--env-file`. Test de uso en `cmd/debfactory/main_test.go`.

### Riesgo 3: `Valid-Until` con firma propia (FR-030)

- [X] T009 Crear `internal/sign/release.go`:
  - `AddValidUntil(releasePath string, signedAt time.Time, days int) error` inserta `Valid-Until: <RFC 1123 UTC>` justo después de `Date:` y conserva el resto byte a byte;
  - `SignRelease(ctx, gnupgHome, subkey, dir)` escribe `InRelease` (`gpg --clearsign`) y `Release.gpg` (`gpg --detach-sign --armor`) con `--batch --no-tty --pinentry-mode loopback --passphrase-file <home>/passphrase --local-user <subkey>!`, y la salida pasa por `redact`.

  Tests en `internal/sign/release_test.go`: inserción (unitario, con un `Release` de fixture en `testdata/repo/release/`) y firma verificable con `gpgv` y la llave de `testutil.TestKey` (tag `integration`).
- [X] T010 Cambiar `internal/repo/generate.go`: `aptly publish` pasa a `-skip-signing` (quitar `-gpg-key` y `-passphrase-file`). Después de publicar, para cada suite: `sign.AddValidUntil` con `cfg.Repo.Validity.Days` y `sign.SignRelease`. La fecha de firma es una sola para toda la generación y sale de un reloj inyectable (`now func() time.Time`, por defecto `time.Now`), para que los tests puedan firmar en el pasado sin código especial en producción. Mantener la atomicidad: si falla la firma de una suite, la generación se descarta como hasta ahora (etapa `sign`).
- [X] T011 Cambiar `internal/repo/verify.go` (sustituye FR-004a de la fase 002):
  - falla si un `Release` no tiene `Valid-Until` (`Release sin Valid-Until (FR-030)`);
  - falla (código 1) si `Valid-Until` ya pasó;
  - avisa si quedan menos de `repo.validity.warn_days`; con `--json` emite `{"event":"expiry","valid_until":…,"days":…}`.

  Actualizar las pruebas que exigían la ausencia de `Valid-Until` (`grep -rn 'Valid-Until\|FR-004a' internal/`) y añadir a `internal/repo/expiry_test.go` (tag `integration`) los casos "con `Valid-Until`", "caducada" (reloj adelantado) y "aviso a 6 días".
- [X] T012 Ampliar `internal/repotest/lifecycle_test.go` (tag `e2e`, `just e2e`): en las 5 suites, `apt-get update` acepta la generación sin advertencias. Para la caducidad, el test publica una generación firmada con fecha de ahora − 31 días (reloj inyectado de T010) y comprueba que `apt-get update` la rechaza con `is expired`. **Nunca se cambia el reloj del host ni se usa `--cap-add SYS_TIME`**: los contenedores comparten el reloj del kernel. **Riesgo 3 cerrado** cuando pasa.

### Runners en la PC (riesgos 1 y 2)

- [X] T013 Crear `ci/runner/Dockerfile` (contracts/runner-host.md § Imagen):
  - base `debian:trixie@sha256:<digest>`;
  - `trixie-backports` para `golang-1.26-go`; `git`, `just`, `aptly`, `gnupg`, `rclone`, `curl`, `ca-certificates`, `docker-cli` y `jq` de trixie;
  - `ARG RUNNER_VERSION` y `ARG RUNNER_SHA256`; descarga `actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz` de `github.com/actions/runner/releases`, `sha256sum -c` y falla si no coincide;
  - `ENV RUNNER_ALLOW_RUNASROOT=1 PATH=/usr/lib/go-1.26/bin:$PATH GOTOOLCHAIN=local`; sin `sudo`;
  - `ENTRYPOINT` que ejecuta `./run.sh --jitconfig "$(cat /run/jit/config)"` y nunca `config.sh`. Si una configuración JIT impide la actualización automática del runner se comprueba en T021. Un comentario documenta que actualizar es un cambio revisado de `RUNNER_VERSION` y `RUNNER_SHA256`;
  - dos `target`: `ci` y `pub`; `pub` copia `ci/runner/hooks/trusted-gate.sh` y fija `ACTIONS_RUNNER_HOOK_JOB_STARTED`.
- [X] T014 [P] Crear `ci/runner/hooks/trusted-gate.sh` (contracts/runner-host.md § Hook): compara `GITHUB_REPOSITORY`, `GITHUB_EVENT_NAME`, `GITHUB_REF` y `GITHUB_WORKFLOW_REF` con los valores esperados (el repositorio llega por `DEBFACTORY_REPOSITORY`, fijado por `run-runner.sh`, no por el workflow). Si algo no coincide, imprime `trusted-gate: rechazado (<variable>=<valor>)` y sale con 1. Nunca lee `GITHUB_EVENT_PATH`. Variable `TRUSTED_GATE_MODE=log` para el spike (imprime y sale con 0). Test en `ci/runner/hooks/trusted-gate_test.sh` con una tabla de casos (push a main ok; `pull_request` con `refs/pull/1/merge`, otro workflow, otra rama y otro repositorio rechazados), ejecutado por `just ci-lint`.
- [X] T015 [P] Crear `ci/runner/jitconfig.sh <nivel>` (corre como root en `ExecStartPre=+`): lee el PAT de `$CREDENTIALS_DIRECTORY/gh`, hace `POST /repos/<repo>/actions/runners/generate-jitconfig` con `{"name":"debfactory-<nivel>-<uuid>","runner_group_id":1,"labels":["self-hosted","<etiqueta>"],"work_folder":"/srv/debfactory/<nivel>/work/_work"}`, y escribe `encoded_jit_config` en `$RUNTIME_DIRECTORY/config` con dueño `df<nivel>` y permisos `600`. El PAT nunca va en la línea de órdenes (`curl -H @-` o `--config -` desde stdin) ni en la salida. `User-Agent: debfactory`.
- [X] T016 [P] Crear `ci/runner/run-runner.sh <nivel>` (contracts/runner-host.md § Contenedor): `DOCKER_HOST=unix://$XDG_RUNTIME_DIR/docker.sock docker run --rm --name debfactory-runner-<nivel>` con:
  - `--mount` de `/srv/debfactory/<nivel>/work` en la misma ruta y `TMPDIR` dentro de ella;
  - el socket rootless del usuario en `/var/run/docker.sock`;
  - `$RUNTIME_DIRECTORY/config` en `/run/jit/config` de solo lectura;
  - solo en `pub`: `/srv/debfactory/data`, `/srv/debfactory/rclone` y `/srv/debfactory/cache-pub` en la misma ruta, y `DEBFACTORY_DATA`, `DEBFACTORY_CACHE` y `RCLONE_CONFIG`;
  - `DEBFACTORY_REPOSITORY` leído de `/etc/default/debfactory-runner`;
  - la etiqueta de imagen fijada en `/etc/default/debfactory-runner`.

  En esta tarea sin `--read-only` (lo añade T041).
- [X] T017 [P] Crear `ci/runner/cleanup.sh <nivel>` (`ExecStopPost=+`): borra `/srv/debfactory/<nivel>/work/*` y el archivo JIT. En `ci`, además, `docker system prune -af --volumes` con el Docker rootless de `dfci` y `rm -rf ~dfci/.cache/debfactory`. Nunca toca `/srv/debfactory/data`.
- [X] T018 Crear las unidades y `ci/host/setup.sh` (idempotente, POSIX sh, como root):
  - `ci/host/debfactory-runner@.service` exactamente como en contracts/runner-host.md § Unidades systemd (`User=df%i`, `LoadCredential=gh:/etc/credstore/debfactory-gh-runner`, `RuntimeDirectory` con `RuntimeDirectoryMode=0700`, `ExecStartPre=+`, `ExecStart`, `ExecStopPost=+`, `Restart=always`, `CPUQuota=1400%`, `MemoryMax=24G`, `TasksMax=4096`);
  - `setup.sh`: crea `dfci` y `dfpub` (`adduser --system --group --home /var/lib/df<nivel> --shell /usr/sbin/nologin df<nivel>`, home `700`), sus rangos en `/etc/subuid` y `/etc/subgid` y `loginctl enable-linger`; instala Docker rootless con `machinectl shell df<nivel>@ /usr/bin/dockerd-rootless-setuptool.sh install` (necesita la sesión de usuario de systemd) y comprueba que `systemctl --user -M df<nivel>@ is-active docker` da `active`; crea `/srv/debfactory/{ci,pub}/work`, `/srv/debfactory/data`, `/srv/debfactory/rclone` y `/srv/debfactory/cache-pub` con los dueños y permisos de contracts/runner-host.md § Usuarios y directorios; comprueba que `/home/pav0n` tiene `700` (si no, avisa y para);
  - instala `ci/runner/*.sh` en `/usr/local/lib/debfactory/` y las unidades en `/etc/systemd/system/`;
  - `--token-stdin`: lee el PAT de la entrada estándar (sin eco, nunca como argumento) y lo escribe en `/etc/credstore/debfactory-gh-runner` (`root`, `600`);
  - escribe `/etc/default/debfactory-runner` con `DEBFACTORY_REPOSITORY` y la etiqueta de imagen.

  No arranca las unidades: eso lo hace el mantenedor tras `just runner-image`.
- [ ] T019 **(manual)** Spike del riesgo 1 (research §3): como `dfci`, con la imagen `ci` construida por `just runner-image` y sin registrarla en GitHub (`docker run --rm --entrypoint sh` con los montajes de `run-runner.sh`), ejecutar `go build ./cmd/debfactory && bin/debfactory test fnm` dentro del contenedor. Debe pasar en las 5 suites y en `arm64` (qemu con flag `F`), con `--network none` en las pruebas de instalación y los `--mount` de lintian y completions funcionando. Anotar el resultado y los ajustes en research.md §3. Si falla, corregir T013/T016 antes de seguir.
- [ ] T020 **(manual)** Crear el repositorio público en GitHub y subir `main`. Crear el fine-grained PAT (solo este repositorio, `Administration: write` y `Actions: write`) y pasarlo a `sudo ci/host/setup.sh --token-stdin`. Nunca pegarlo en el chat ni en un archivo del repositorio. Rellenar `ci.repository` en `config/debfactory.yaml` con el `owner/nombre` real.
- [ ] T021 **(manual)** Spike del riesgo 2: arrancar `debfactory-runner@pub` con `TRUSTED_GATE_MODE=log` **sin** los montajes de `data` y `rclone` (variable `DEBFACTORY_SPIKE=1` en `run-runner.sh`). Con un workflow temporal `.github/workflows/spike.yml` en una rama propia que imprime `env | grep ^GITHUB_ | sort` y pide `debfactory-main`, lanzarlo por `pull_request`, `push` y `workflow_dispatch`. Confirmar en el log del hook que `GITHUB_WORKFLOW_REF`, `GITHUB_REF` y `GITHUB_EVENT_NAME` tienen los valores de contracts/runner-host.md. Comprobar además en el log del job que el runner no descarga una versión nueva de sí mismo. Si la descargara, la copia se pierde con el contenedor (`--rm`), pero sería un binario sin verificar: documentarlo como desviación en plan.md § Complexity Tracking y buscar la variable que lo desactive. Pasar el hook a modo rechazo, comprobar que el `pull_request` falla en "Set up runner" sin pasos, y borrar `spike.yml`. **Riesgo 2 cerrado**. Anotar en research.md §3.

### Repositorio de pruebas para `internal/ci`

- [X] T022 Crear `internal/ci/citest/` (solo para tests): `FakeGitHub(t)`, un `httptest.Server` con estado en memoria para issues (con etiquetas y comentarios), PRs, refs y dispatch, que registra cada petición; y `GitFixture(t)`, que crea un remoto `git init --bare` en `t.TempDir()` con un `main` que contiene `config/`, `packages/` (fixtures de `testdata/manifests/`), `repo/state.yaml` y un clon de trabajo. Lo usan las pruebas de US1–US6.

**Checkpoint**: firma con `Valid-Until` en verde en `just e2e`; los dos runners atienden jobs efímeros; el hook rechaza lo que no es `main.yml` en `main`; `go test ./...` pasa.

---

## Phase 3: User Story 1 - Un merge publica solo (Priority: P1) 🎯 MVP

**Goal**: un merge que sube un paquete termina, sin intervención, en el repositorio público, con el PR de estado abierto; el refirmado semanal mantiene los índices válidos.

**Independent Test**: quickstart §1 con `ci.destination: r2test` y datos desechables: subir la `revision` de fnm, integrar el PR y comprobar en contenedores limpios que `apt-get install fnm` instala la versión nueva, que los otros paquetes no cambian y que el merge del PR de estado no publica nada.

- [X] T023 [P] [US1] Crear `internal/ci/plan.go` para los modos `main` y `rebuild`:
  - tipos `Plan{Mode, Reference{Source, SHA256}, Publish, Test, Compare, Skipped, Errors}` y `AffectedPackage{Package, Version, Commit, Reason}` con `reason` ∈ `new-package|new-version|new-revision|rebuild` (data-model.md §2);
  - `PlanMain(cfg, root, ref *repo.State, head string)` aplica la tabla de conciliación de data-model.md §2 literalmente, incluidos los mensajes `versión retirada; sube revision`, `versión anterior a la publicada` y `regenera el lockfile con \`debfactory lock\``;
  - los paquetes son independientes: un error en uno no lo quita a los demás (FR-004);
  - salida de texto y `--json` (un objeto).

  Tests en `internal/ci/plan_test.go`: una fila de la tabla por caso, con fixtures en `testdata/ci/plan/`.
- [X] T024 [P] [US1] Crear `internal/ci/audit.go` (lectura y escritura; los invariantes completos son de US5): tipo `Entry` con las claves y el orden exacto de contracts/audit.md (`seq`, `time`, `op`, `package`, `version`, `commit`, `debs`, `serial`, `run`, `reason`), `run` como `*string` para emitir `null`; `Read(path)` estricta (clave desconocida = error con número de línea); `Append(path, entries…)` atómica con `seq` consecutivo; `IsPrefix(a, b)` por bytes. Tests en `internal/ci/audit_test.go`: ida y vuelta byte a byte y orden de claves.
- [X] T025 [US1] Crear `internal/ci/statepr.go` (research D8, data-model.md §3):
  - copia local en `<data>/ci/`: `state.yaml`, `audit.jsonl` y `pushed.json` `{state_sha256, audit_sha256, branch_commit, pr_number, pushed_at}`, escritos de forma atómica;
  - `Reconcile(ctx)` aplica la tabla de transiciones: coincide → seguir; `main` contiene la copia → seguir y borrar la rama; la rama falta o va por detrás y su `audit.jsonl` es prefijo del de la copia → volver a empujar y seguir (FR-029); cualquier otro caso → `report.Error` de etapa `state` (FR-007b);
  - `Reference()` devuelve el estado del PR si hay uno abierto y si no el de `main` (FR-007a);
  - `Push(ctx, title, body)`: regenera la rama `ci.state_branch` como `origin/main` + 1 commit (autor `github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com>`) con `repo/state.yaml` y `repo/audit.jsonl`, `git push --force-with-lease=<rama>:<branch_commit esperado>`, y abre o actualiza el único PR (título `repo: estado tras serial <N> (<paquetes>)`, cuerpo con la tabla de operaciones y enlaces);
  - el remoto git usa `GITHUB_TOKEN` vía `http.extraheader` en un `GIT_CONFIG_*` del proceso, nunca en la URL ni en la línea de órdenes visible.

  Tests en `internal/ci/statepr_test.go` (tag `integration`, con `citest.GitFixture` y `FakeGitHub`): una prueba por fila de la tabla, "nunca dos PR abiertos" y "PR cerrado sin integrar → etapa `state`".
- [X] T026 [US1] Crear `internal/ci/run.go` con `Main(ctx, cfg, env, jobs)` en el orden exacto de contracts/cli.md (`ci main`):
  1. `env.RequireWriter`; si `GITHUB_SHA` ya no es la punta de `origin/main` (`git ls-remote`), termina con código 0 y el evento `superseded`;
  2. `statepr.Reconcile`; si el destino tiene un `serial` sincronizado menor que el local (`sync/<dest>.json`), `repo sync` + `repo verify --dest` sin reconstruir; si la rama va por detrás, push (FR-029);
  3. `PlanMain` contra `statepr.Reference()`;
     3b. si `git diff --name-only <last_main_sha>..HEAD` (`last_main_sha` de `pushed.json`) toca `ci.code_paths`, las comprobaciones de código de `ci pr` (`gofmt`, `go vet`, `go test`, `validate`; etapa `code`) y la comparación de FR-010 (etapa `compare`). Si cualquiera falla, no publica nada y notifica (FR-005). Al terminar una ejecución completa, `last_main_sha` = `HEAD`;
  4. gates 1–6 por paquete, en paralelo con `--jobs`, reutilizando `build`, `lint` e `installtest` de la fase 001 (sin reimplementarlos);
  5. `repo publish` de los que pasaron, en una sola generación; justo después del swap, `audit.Append` de una entrada `publish` por paquete y escritura de la copia en `<data>/ci/`;
  6. `repo sync` a `ci.destination` y `repo verify --dest`;
  7. `statepr.Push`;
  8. notificaciones: un punto de extensión `Notifier` que en esta historia solo registra en el log (lo implementa US4).

  Al empezar cada etapa escribe su nombre en `$RUNNER_TEMP/debfactory-stage` (lo lee `ci notify --stage-file`, T052). Toma el `flock` de `<data>/lock` de la fase 002 durante los pasos 2–7. Escribe la salida de contracts/cli.md § Salida y el resumen en `GITHUB_STEP_SUMMARY`. Código 1 si falló algún paquete o etapa, aunque otros se publicaran (FR-004).
- [X] T027 [US1] Conectar `ci plan --mode main` y `ci main` en `cmd/debfactory/ci.go`.
- [X] T028 [US1] Prueba de integración `internal/ci/run_test.go` (tag `integration`, gpg y aptly reales, destino `local`, `citest`):
  - publicar una `revision` nueva de un paquete de fixture: serial +1, entrada `publish` en `audit.jsonl`, PR de estado abierto y los `.deb` de los demás paquetes iguales byte a byte (US1 escenario 2);
  - volver a ejecutar sobre el commit del merge del PR de estado: `nada que publicar` y el serial no cambia (escenario 6);
  - un paquete cuya prueba falla en una suite no se publica en ninguna y el otro sí (escenario 4, FR-004);
  - ejecución superada: código 0 sin construir;
  - un push que solo cambia `internal/` y altera el `.deb` de una versión publicada falla en la etapa `compare` y no publica (FR-005, paso 3b).
- [X] T029 [US1] Prueba de recuperación (SC-007, FR-029) en `internal/ci/recovery_test.go` (tag `integration`): inyectar un fallo justo después del swap (`ci.testHookAfterSwap`), relanzar y comprobar que sync, verify y push se completan, que no se genera otra generación y que no hay entradas de auditoría duplicadas; repetir con el fallo después del sync y antes del push.
- [X] T030 [US1] Crear `.github/workflows/main.yml` exactamente como el esbozo de contracts/workflows.md, con `actions/checkout` fijada por sha de commit (`ref: main`, `fetch-depth: 0`), `concurrency: {group: publish, cancel-in-progress: false, queue: max}`, `timeout-minutes: 120` y `permissions` `contents`, `pull-requests` e `issues` en `write`. Los inputs pasan por `env:` (`INPUT_ACTION: ${{ inputs.action }}`, etc.) y nunca se interpolan en `run:`. En esta historia el paso `if: failure()` queda comentado (lo activa T052).
- [X] T031 [US1] Crear `internal/ci/refresh.go` (depende de T025) y conectar `ci refresh` (FR-031): `RequireWriter`, `statepr.Reconcile`, `repo rebuild` (misma `serial`, generación nueva con `genName`), `repo sync` y `repo verify --dest`. No toca `state.yaml` ni la auditoría. Tests en `internal/ci/refresh_test.go` (tag `integration`): tras `refresh`, el `Valid-Until` es posterior, `state.yaml` es idéntico byte a byte y `audit.jsonl` no cambia.
- [X] T032 [P] [US1] Crear `ci/host/debfactory-refresh.service` y `ci/host/debfactory-refresh.timer` (contracts/runner-host.md § Temporizador): `OnCalendar=weekly`, `Persistent=true`, `RandomizedDelaySec=1h`; el servicio con `DynamicUser=yes` y `LoadCredential=gh:/etc/credstore/debfactory-gh-runner` hace `POST /repos/<repo>/actions/workflows/main.yml/dispatches` con `{"ref":"main","inputs":{"action":"refresh"}}` por `curl` con el token desde stdin y `User-Agent: debfactory`. `setup.sh` (T018) los instala y habilita.
- [ ] T033 **(manual)** [US1] Medir el riesgo 4 (SC-001) con quickstart §1 sobre `r2test` y las cachés de `dfpub` vacías: anotar la hora del merge, la del `verify --dest` en verde y la de la primera instalación en un contenedor limpio. Debe ser < 30 min; anotar el desglose por etapa en research.md §3. **Riesgo 4 cerrado**.

**Checkpoint**: US1 funciona contra `r2test`. No migrar los datos reales ni apuntar `ci.destination` a `r2` hasta cerrar US3.

---

## Phase 4: User Story 2 - Los pull requests se prueban, nunca se publican (Priority: P1)

**Goal**: cada PR pasa los gates 1–6 de lo que cambia y muestra el resultado por paquete, suite y arquitectura, sin escribir nada.

**Independent Test**: quickstart §2: un PR con un sha256 roto falla en `verify` nombrando el artefacto y los dos hashes, y `repo status` muestra el mismo serial y `current`.

- [X] T034 [P] [US2] Ampliar `internal/ci/plan.go` con `PlanPR(cfg, root, base)`:
  - `git diff --name-only <base>...HEAD`;
  - `test[]`: paquetes con `packages/<p>.yaml` o `packages/<p>.lock.yaml` cambiados;
  - si algún archivo empieza por un prefijo de `ci.code_paths`, `compare[]` = todas las versiones publicadas en el `state.yaml` de la base (FR-010);
  - un paquete en `test[]` cuya versión ya está publicada con el mismo `.deb` es un error de etapa `state` (`versión publicada; sube revision`).

  Tests en `internal/ci/plan_test.go`: solo manifiesto, solo lockfile, solo código, código + paquete y nada relevante.
- [X] T035 [US2] Crear `internal/ci/pr.go` con `PR(ctx, cfg, env, base, jobs)`:
  - si `env.Trust == "pub"` sale con código 3 (un PR nunca corre en el nivel confiable);
  - si cambió código: `gofmt -l` vacío, `go vet ./...`, `go test ./...` y `debfactory validate`;
  - gates 1–6 de `plan.test[]` con la caché del nivel `ci`;
  - para `plan.compare[]`: reconstruye la versión publicada con el manifiesto y el lockfile de la base y compara el sha256 por arquitectura con `state.yaml`; si difiere y el PR no sube su `revision`, falla nombrando los paquetes (FR-010);
  - nunca abre `<data>` ni llama a `sign`, `publish`, `sync` ni a la API de escritura (FR-009); `<data>` apunta a un directorio temporal vacío;
  - escribe en `GITHUB_STEP_SUMMARY` una tabla Markdown paquete × suite × arquitectura con ✅/❌ y el motivo (FR-008, FR-022).
- [X] T036 [US2] Conectar `ci plan --mode pr --base REF` y `ci pr` en `cmd/debfactory/ci.go`.
- [X] T037 [US2] Pruebas en `internal/ci/pr_test.go` (tag `integration`, `citest.GitFixture`):
  - lockfile con un sha256 cambiado → falla en `verify` antes de construir, con el artefacto y los dos hashes en el mensaje (US2 escenario 1, SC-002);
  - un cambio en `internal/` que altera el changelog sin subir revisiones → falla nombrando los paquetes (escenario 3);
  - el árbol `<data>` real (un directorio de fixture) queda idéntico byte a byte tras cada caso (FR-009);
  - el resumen contiene una fila por suite y arquitectura.
- [X] T038 [US2] Crear `.github/workflows/pr.yml` exactamente como el esbozo de contracts/workflows.md: `paths-ignore` (`docs/**`, `specs/**`, `**.md`, `repo/state.yaml`, `repo/audit.jsonl`), `permissions: contents: read`, `concurrency: pr-<número>` con `cancel-in-progress: true`, `runs-on: [self-hosted, debfactory-pr]`, `timeout-minutes: 90` y `actions/checkout` fijada por sha con `fetch-depth: 0` y `persist-credentials: false`.
- [ ] T039 **(manual)** [US2] Ejecutar las cuatro filas de quickstart §2 con PR desde ramas propias y anotar el resultado en research.md §3, junto con la duración del PR que sube un paquete (meta del plan: < 30 min).

**Checkpoint**: los PR muestran el check y el resumen; ninguno escribe.

---

## Phase 5: User Story 3 - Los secretos nunca llegan al código no confiable (Priority: P1)

**Goal**: los PR, incluso aprobados, corren sin secretos, sin red local y sin dejar nada; solo `main.yml` en `main` llega al runner confiable; los datos reales pasan a `dfpub`.

**Independent Test**: quickstart §3 desde una cuenta secundaria: ningún secreto, ninguna escritura fuera del trabajo, la red local rechazada y el job que pidió `debfactory-main` rechazado por el hook.

- [X] T040 [P] [US3] Crear `ci/host/debfactory-ci.nft` exactamente como en contracts/runner-host.md § Red (tabla `inet debfactory_ci`, `meta skuid "dfci"` contra RFC 1918, `169.254.0.0/16`, `100.64.0.0/10`, `fc00::/7` y `fe80::/10`). `setup.sh` lo instala en `/etc/nftables.d/`, se asegura de que `/etc/nftables.conf` incluye ese directorio y lo carga con `nft -f`.
- [ ] T041 [P] [US3] Endurecer `ci/runner/run-runner.sh`: `--read-only`, `--tmpfs /tmp` y `--tmpfs /root` (el `HOME` del root del contenedor), `--security-opt no-new-privileges`, `--cap-drop ALL` salvo lo que el runner necesite (anotar cuáles), y en `ci` ningún montaje fuera de su `work`. Repetir T019 con la imagen endurecida.
- [X] T042 [US3] Crear `internal/ci/doctor.go` y conectar `ci doctor` (contracts/cli.md; research §2). Con un token del mantenedor (`GH_TOKEN` en el entorno, nunca impreso) comprueba por la API:
  - fork PR workflows = "Require approval for all external contributors";
  - permisos por defecto del token = solo lectura, y "Allow GitHub Actions to create and approve pull requests" activado;
  - un ruleset activo sobre la rama por defecto que exige PR, bloquea force push y borrado, y no tiene `bypass_actors`;
  - la etiqueta `ci.failure_label` existe (si falta, ofrece `--fix` para crearla);
  - `RUNNER_VERSION` (de `/etc/default/debfactory-runner`) no está más de 14 días por detrás de `GET /repos/actions/runner/releases/latest` (GitHub deja de aceptar runners atrasados unos 30 días).

  En local comprueba: usuarios `dfci` y `dfpub` sin los grupos `docker` ni `sudo`; `/home/pav0n` con `700`; dueños y permisos de contracts/runner-host.md § Usuarios y directorios; el credstore en `root:600`; las unidades y el temporizador habilitados; la tabla `debfactory_ci` cargada (`nft list table inet debfactory_ci`). Imprime una línea por comprobación con ✅/❌ y sale con 1 si falta algo. Tests en `internal/ci/doctor_test.go` contra `FakeGitHub` (la parte de la API) y con un `fs.FS` falso (la parte local).
- [X] T043 [P] [US3] Ampliar `internal/repotest/zz_secrets_test.go` (tag `e2e`; depende de T026 y T035): ejecutar `ci main`, `ci refresh` y `ci pr` contra `citest` con la llave de prueba y un `rclone.conf` de prueba con valores centinela, y comprobar que ninguna salida, resumen, cuerpo de issue o PR enviado a `FakeGitHub` contiene la frase de paso, `PRIVATE KEY`, las claves de `rclone.conf` ni el token.
- [X] T044 [US3] Crear `ci/host/migrate-data.sh` (research D17): con el runner `pub` parado, copia `~pav0n/.local/share/debfactory` a `/srv/debfactory/data` y el `rclone.conf` de R2 a `/srv/debfactory/rclone/rclone.conf` con dueño `dfpub` y los permisos de contracts/runner-host.md; ejecuta como `dfpub` `debfactory repo verify --data /srv/debfactory/data` y `repo status`; solo si pasan, mueve los originales a `~/.local/share/debfactory.migrated-<fecha>` (no los borra). Nunca imprime el contenido de `gnupg/` ni de `rclone.conf`.
- [ ] T045 **(manual)** [US3] En GitHub: activar "Require approval for all external contributors", permisos del token en solo lectura, "Allow GitHub Actions to create and approve pull requests", el ruleset de `main` sin bypass y la etiqueta. `just ci-doctor` en verde.
- [ ] T046 **(manual)** [US3] Ejecutar `sudo ci/host/migrate-data.sh`, cambiar `ci.destination` a `r2` (ya lo es en `config/debfactory.yaml`; quitar la anulación de prueba de US1) y relanzar `debfactory-runner@pub`.
- [ ] T047 **(manual)** [US3] Ejecutar quickstart §3 completo desde la cuenta secundaria (SC-003) y anotar el resultado de cada comprobación, incluida la búsqueda en los logs descargados.

**Checkpoint**: P1 completo. A partir de aquí el pipeline publica en `r2` y es el único escritor.

---

## Phase 6: User Story 4 - Notificación de fallos (Priority: P2)

**Goal**: cada fallo de una ejecución `pub` abre o comenta un issue con paquete, versión, etapa, motivo y log, y se cierra al resolverse.

**Independent Test**: quickstart §5: provocar un fallo de prueba de instalación en `main`, ver el issue, repetirlo (un comentario, no otro issue) y corregirlo (el issue se cierra con el enlace).

- [X] T048 [P] [US4] Crear `internal/ci/notify.go` (contracts/notificaciones.md):
  - `Key{Package, Version, Stage}`; `version` es `-` en `pipeline`, `state`, `audit` y `expiry`;
  - marcador `<!-- debfactory:failure package=… version=… stage=… -->` en la primera línea; título `fallo: <package> <version> en <stage>`; cuerpo con paquete, versión, etapa, motivo, suite y arquitectura si aplican, log y commit, en el formato exacto del contrato;
  - `Fail(key, reason, suite, arch)`: busca con `ListOpenIssues(ci.failure_label)` paginado (nunca la API de búsqueda) y comenta o crea;
  - `ResolvePublished(package, version, run, serial)`: cierra todos los issues abiertos de ese paquete y esa versión con `resuelto por <run>: publicado en serial N`;
  - `ResolveStage(stage)`: cierra los de `state`, `audit`, `expiry`, `state-pr-age` y `pipeline` cuando una ejecución pasa esa etapa;
  - los motivos pasan por `redact` antes de enviarse;
  - si la API falla, devuelve el error para el log y el resumen, sin cambiar el código de salida de la ejecución.

  Tests en `internal/ci/notify_test.go` contra `FakeGitHub`: crear, comentar al repetir (FR-021), cerrar al publicar, cerrar por etapa, issues de otra etiqueta ignorados, paginación con 150 issues y API caída.
- [X] T049 [US4] Implementar el `Notifier` de `run.go` con `notify.go` y conectarlo en `Main` y `Refresh` (`Rebuild` y `Retire` lo usan al crearse en T059 y T060): cada `report.Error` se convierte en `Fail` con su etapa, suite y arquitectura; cada paquete publicado llama a `ResolvePublished`; cada etapa global que pasa llama a `ResolveStage`. Ampliar `internal/ci/run_test.go` para comprobar las peticiones registradas en `FakeGitHub`.
- [X] T050 [US4] Aviso de caducidad (FR-032): en `ci main` y `ci refresh`, tras `repo verify --dest`, si el evento `expiry` indica menos de `repo.validity.warn_days`, `Fail({pipeline, -, expiry})` con la fecha `Valid-Until`; si no, `ResolveStage(expiry)`. Test con el reloj adelantado.
- [X] T051 [US4] PR de estado antiguo (FR-007c): en `statepr.Reconcile`, si el PR abierto tiene más de `ci.state_pr_max_age`, `Fail({pipeline, -, state-pr-age})` con el enlace al PR y seguir; al integrarse, `ResolveStage(state-pr-age)`. Ampliar `internal/ci/statepr_test.go`.
- [X] T052 [US4] Conectar `ci notify --package P --version V (--stage S | --stage-file FILE) --reason TEXTO`; con `--stage-file`, si el archivo no existe la etapa es `pipeline`. Activar en `.github/workflows/main.yml` el paso `if: failure()` exactamente como en contracts/workflows.md (`--stage-file "$RUNNER_TEMP/debfactory-stage"`).
- [ ] T053 **(manual)** [US4] Ejecutar las siete filas de quickstart §5 y medir que cada issue aparece en menos de 5 minutos (SC-005). Restaurar la configuración real al terminar.

**Checkpoint**: los fallos de `main` llegan por correo de GitHub y se cierran solos.

---

## Phase 7: User Story 5 - Registro auditable de cada publicación (Priority: P2)

**Goal**: `repo/audit.jsonl` tiene una entrada por publicación y retiro, solo crece, y cubre todo `.deb` servido.

**Independent Test**: quickstart §6: `ci audit check --against origin/main` en verde, y reconstruir un `.deb` desde el commit de su entrada da el mismo sha256.

- [X] T054 [P] [US5] Completar `internal/ci/audit.go` con `Check(entries, state, againstPrefix []byte)` y las 5 reglas y mensajes exactos de contracts/audit.md § Reglas (clave desconocida, `seq` no consecutivo, no es prefijo, falta la publicación de `<pkg> <ver> <arch>`, retiro de una versión nunca publicada). Tests en `internal/ci/audit_test.go` con fixtures en `testdata/ci/audit/`: un archivo por regla con un comentario `# want:` al lado (en un `.want` hermano, porque JSONL no admite comentarios).
- [X] T055 [US5] Primera ejecución (contracts/audit.md § Primera ejecución): si `repo/audit.jsonl` no existe y `state.yaml` tiene `serial ≥ 1`, `ci main` crea una entrada `import` por cada versión publicada, en orden de paquete y versión, con `time` = `published` de `state.yaml`, `run: null` y `commit = git log -1 --format=%H -- repo/state.yaml`. Test: con el `state.yaml` actual (4 paquetes, serial 1) se generan 4 entradas `import` con el commit de `9306cdd` completo.
- [X] T056 [US5] Conectar `ci audit check [--against REF]` y añadir la etapa `audit` a `ci main`, `ci rebuild` y `ci retire`, antes de publicar: compara el registro de la referencia (`statepr.Reference`) con el de `REF` y `state.yaml`; si falla, no publica y notifica (etapa `audit`).
- [X] T057 [US5] Prueba en `internal/ci/run_test.go` (tag `integration`): editar una línea antigua de `repo/audit.jsonl` en `main` hace fallar la siguiente ejecución en la etapa `audit` sin publicar (quickstart §6), y tras dos publicaciones el registro tiene dos entradas nuevas con el sha256 de cada `.deb` de `state.yaml`.
- [ ] T058 **(manual)** [US5] Ejecutar quickstart §6 sobre el repositorio real (SC-006) para un `.deb` publicado por el pipeline y para uno importado.

**Checkpoint**: cada `.deb` servido tiene su entrada y es reconstruible.

---

## Phase 8: User Story 6 - Ejecuciones manuales: reconstruir y retirar (Priority: P3)

**Goal**: reconstruir, retirar, refirmar y restaurar el PR de estado desde Actions → main → Run workflow, sin terminal.

**Independent Test**: quickstart §7: `rebuild` de `fnm 1.39.0-0~tepache1` termina en verde con "sha256 iguales; nada que publicar".

- [X] T059 [US6] Crear `Rebuild(ctx, cfg, env, pkg, version, jobs)` en `internal/ci/run.go` (FR-026): `restore.FindCommit(ctx, root, pkg, version)` da el commit cuyo manifiesto y lockfile se usan (en un `git worktree` temporal); si ningún commit la declaró, falla sin hacer nada. Gates 1–6. Si la versión ya está publicada, compara el sha256 por arquitectura con `state.yaml`: iguales → `sha256 iguales; nada que publicar`, código 0 y `ResolvePublished`; distintos → falla sin publicar. Si no está publicada, aplica la tabla de conciliación de data-model.md §2: si es menor que la más alta publicada falla en la etapa `state` con `versión anterior a la publicada`, sin construir; si no, sigue los pasos 5–8 de `Main`. Conectar `ci rebuild` y `ci plan --mode rebuild`.
- [X] T060 [US6] Crear `Retire(ctx, cfg, env, pkg, version, reason, last)` en `internal/ci/run.go` (FR-027): `RequireWriter`, `statepr.Reconcile`, `repo retire` de la fase 002 (con `--last` solo si `INPUT_LAST=true`; sin él, la única versión falla sin cambios y pide `last`), `audit.Append` de una entrada `retire` con el commit de la ejecución y `reason`, sync, verify y `statepr.Push`. Conectar `ci retire`.
- [X] T061 [P] [US6] Crear `RestoreStatePR(ctx, cfg, env)` en `internal/ci/statepr.go` y conectar `ci restore-state-pr`: comprueba que `<data>/ci/state.yaml` coincide con el estado de la generación `current` y vuelve a crear la rama y el PR desde la copia.
- [X] T062 [US6] Despachar `workflow_dispatch` en `ci main` según `env.Inputs().Action` (`rebuild`, `retire`, `refresh`, `restore-state-pr`), de modo que `main.yml` siempre llama a `bin/debfactory ci "${INPUT_ACTION:-main}"`, y validar los inputs antes de tomar el bloqueo.
- [X] T063 [US6] Pruebas en `internal/ci/manual_test.go` (tag `integration`): `rebuild` de una versión publicada con hashes iguales y con un hash alterado en `state.yaml`; `rebuild` de una versión que no está en ningún commit; `retire` con y sin `last`; `restore-state-pr` tras cerrar el PR en `FakeGitHub`, y la ejecución siguiente pasa la etapa `state` (quickstart §8.4).
- [ ] T064 **(manual)** [US6] Ejecutar las cinco filas de quickstart §7 desde la interfaz de GitHub (SC-008) y comprobar que la cuenta secundaria no ve el botón "Run workflow" (FR-028).

**Checkpoint**: las seis historias funcionan de forma independiente.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: documentación, mediciones pendientes y validación final.

- [X] T065 [P] Crear `docs/operacion-ci.md`:
  - instalación: `setup.sh`, PAT por stdin, `just runner-image`, `just ci-doctor` y `migrate-data.sh`;
  - avisos: activar en GitHub (Settings → Notifications → Actions) el correo de ejecuciones fallidas o canceladas, que cubre los jobs que GitHub cancela tras 24 h en cola con la PC apagada (constitución VIII);
  - operación diaria: integrar el PR de estado, leer issues y lanzar ejecuciones manuales;
  - recuperación: `restore-state-pr`; PC apagada más de una semana; runner que no arranca (`journalctl -u debfactory-runner@pub`); PC perdida con el PR de estado abierto: `git checkout debfactory/state` antes de `repo rebuild`, porque ese PR tiene el `state.yaml` que corresponde a lo que sirve R2;
  - actualización del runner: cuando `ci doctor` avise, cambiar `RUNNER_VERSION` y `RUNNER_SHA256` en un PR revisado, siempre antes de 30 días;
  - rotación del PAT.
- [X] T066 [P] Actualizar la documentación de las fases anteriores:
  - `README.md`: componente **Pipeline** de un párrafo y enlace a `docs/operacion-ci.md`;
  - `docs/operacion-llave.md`: la subllave y la frase de paso viven ahora en `/srv/debfactory/data/gnupg` (dueño `dfpub`);
  - `specs/002-signed-apt-repo/spec.md`: nota en FR-004a "Sustituido por FR-030 de 003";
  - `docs/usuario.md`: una línea sobre `Valid-Until` y qué hacer si `apt update` dice `is expired`.
- [X] T067 [P] Añadir a `justfile` `ci-lint` a `check` y ejecutar `gofmt -l`, `go vet ./...`, `go test ./...`, `go test -tags integration ./...` y `just ci-lint` en verde.
- [ ] T068 **(manual)** Ejecutar quickstart §4 (serialización, SC-004) y §8.1–8.3 (caducidad y recuperación con `systemctl kill debfactory-runner@pub`, SC-007). Medir la duración de una ejecución `refresh` (meta del plan: < 5 min).
- [ ] T069 **(manual)** Con la primera publicación real del pipeline (quickstart §1 sobre `r2`), medir SC-001 y SC-007 de la fase 002 (T062 paso 5 de 002) y marcar esa tarea en `specs/002-signed-apt-repo/tasks.md`.
- [ ] T070 **(manual)** SC-009: dejar pasar 5 semanas sin cambios de paquetes y comprobar 5 ejecuciones `refresh` en verde y que la generación servida nunca bajó de 23 días de validez (quickstart §8.2).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: sin dependencias.
- **Foundational (Phase 2)**: depende de Setup. Bloquea todas las historias. Dentro de ella, los spikes van en este orden: T009–T012 (riesgo 3) en paralelo con T013–T018 (imagen y host); después T019 (riesgo 1), T020 y T021 (riesgo 2).
- **US1 (Phase 3)**: depende de Foundational. Trabaja contra `r2test` y datos desechables.
- **US2 (Phase 4)**: depende de Foundational; comparte `plan.go` con US1 (T034 amplía el archivo de T023, así que va después).
- **US3 (Phase 5)**: depende de Foundational, salvo T043 (depende de T026 de US1 y T035 de US2) y T046 (migrar los datos reales y publicar en `r2`, depende de US1).
- **US4 (Phase 6)**: depende de US1 (el `Notifier` de `run.go`).
- **US5 (Phase 7)**: depende de US1 (`audit.go` básico y el paso 5 de `Main`).
- **US6 (Phase 8)**: depende de US1; T063 usa `notify` (US4) si está, pero no lo exige.
- **Polish (Phase 9)**: depende de las historias que se quieran entregar.

### Within Each User Story

- Plan y modelos antes que la orquestación (`plan.go`/`audit.go`/`statepr.go` → `run.go`).
- La orquestación antes que la CLI y el workflow.
- Las pruebas de integración antes de las tareas manuales contra GitHub.

### Parallel Opportunities

- Setup: T002, T003 y T004.
- Foundational: T006 y T007; T009 en paralelo con T013–T017; T014, T015, T016 y T017 entre sí.
- US1: T023 y T024; T032 en paralelo con T025–T031.
- US2 en paralelo con US3 una vez cerrada Foundational (salvo T046).
- US4, US5 y US6 en paralelo entre sí una vez cerrada US1 (tocan `run.go` en funciones distintas; integrar en serie).

---

## Parallel Example: User Story 1

```text
# A la vez:
T023 internal/ci/plan.go (conciliación)
T024 internal/ci/audit.go (lectura y escritura)
T032 ci/host/debfactory-refresh.{service,timer}

# Después, en serie:
T025 statepr.go → T026 run.go → T027 CLI → T028/T029 pruebas → T030 main.yml → T031 refresh.go → T033 medición
```

## Parallel Example: User Story 3

```text
# A la vez:
T040 ci/host/debfactory-ci.nft
T041 ci/runner/run-runner.sh endurecido
T043 internal/repotest/zz_secrets_test.go (cuando existan T026 y T035)

# Después:
T042 ci doctor → T044 migrate-data.sh → T045–T047 (manual)
```

---

## Implementation Strategy

### MVP First

1. Phase 1 + Phase 2, con los riesgos 1–3 cerrados.
2. US1 contra `r2test` (incluye el refirmado semanal y el riesgo 4).
3. **Antes de publicar en `r2` de verdad**: US3 completa (aislamiento, `ci doctor`, migración de datos). US1 + US3 es el mínimo que se puede poner en producción.
4. US2, para poder integrar PR con confianza.

### Incremental Delivery

1. Foundational → firma con `Valid-Until` y runners listos.
2. US1 → publicación automática en `r2test`.
3. US3 → producción en `r2`; el pipeline es el único escritor.
4. US2 → PR probados.
5. US4 → fallos notificados.
6. US5 → auditoría completa e invariantes.
7. US6 → ejecuciones manuales.
8. Polish → documentación y mediciones de larga duración (SC-009).

### Notas

- Commit solo cuando lo pida el mantenedor.
- Ninguna tarea pega el PAT, la frase de paso ni credenciales de R2 en el chat, en un archivo del repositorio o en un log.
- Ningún binario upstream corre en el host: `actions/runner` solo dentro del contenedor (T013).
- No se silencian etiquetas de lintian de seguridad, licencia o copyright.
