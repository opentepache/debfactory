# Implementation Plan: Pipeline de construcción y publicación

**Branch**: `003-ci-pipeline` | **Date**: 2026-11-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/003-ci-pipeline/spec.md`

## Summary

**Forja y runners.** El código pasa a GitHub, en un repositorio público. GitHub Actions ejecuta
dos workflows sobre dos runners efímeros en la PC del mantenedor:

| Workflow | Disparo | Runner | Usuario | Secretos |
|---|---|---|---|---|
| `pr.yml` | cada pull request | `debfactory-pr` | `dfci` | ninguno |
| `main.yml` | push a `main` y ejecuciones manuales | `debfactory-main` | `dfpub` | subllave, frase de paso, R2 y datos del repositorio |

**Aislamiento.**

- Cada runner es un usuario de sistema propio, con su Docker rootless.
- Cada job corre en un contenedor nuevo, con una configuración JIT de un solo uso. La pide
  systemd como root con un PAT que nunca entra en los contenedores.
- Un hook previo al job rechaza en el runner confiable todo lo que no sea `main.yml` sobre
  `refs/heads/main`. Esto cubre una limitación de las cuentas personales: no tienen runner
  groups.
- Los PR de forks esperan aprobación en cada push.

**Lógica.** Toda la lógica vive en Go, en `debfactory ci …`; los workflows solo hacen checkout,
compilan y llaman a esas órdenes.

- **`ci main`**: concilia los manifiestos de `HEAD` con el estado publicado. Así es idempotente
  y recupera las ejecuciones perdidas. Después construye y prueba solo lo que falta (gates
  1–6), publica en una generación, sincroniza con R2, verifica y añade la auditoría a
  `repo/audit.jsonl`.
- **PR de estado**: cada publicación abre o actualiza `debfactory/state`, que integra el
  mantenedor. El token del job no puede escribir en `main`, porque un ruleset lo impide.
- **Notificaciones**: los fallos abren o comentan un issue por paquete, versión y etapa.

**Caducidad de los índices.** Los índices pasan a caducar a los 30 días. debfactory firma por su
cuenta, porque aptly no tiene una opción para `Valid-Until`. Un temporizador systemd lanza cada
semana `workflow_dispatch` con `refresh`. Se lanza desde la PC porque `on: schedule` se desactiva
en repos públicos tras 60 días sin actividad.

## Technical Context

**Language/Version**: Go 1.26 (el mismo módulo `debfactory`), `GOTOOLCHAIN=local`. Los scripts
del host (`ci/host/*.sh`, `ci/runner/*.sh`) son POSIX sh.

**Primary Dependencies**:

- **Las de las fases 001 y 002**: aptly, gnupg, rclone y Docker. Sin bibliotecas Go nuevas: el
  cliente de GitHub es `net/http`.
- **En la PC**:
  - Docker rootless (`docker-ce-rootless-extras`, ya instalado; `uidmap`);
  - systemd (credenciales, unidades plantilla y temporizador);
  - nftables.
- **En la imagen del runner** (`debian:trixie`):
  - `golang-1.26-go` (trixie-backports), `git`, `just`, `aptly`, `gnupg`, `rclone`, `curl` y
    `docker-cli`;
  - el runner de `actions/runner`, en una versión fijada por sha256.
- **En los workflows**: solo `actions/checkout`, fijada por sha de commit.

**Storage**:

- **En git**: `repo/state.yaml` (fase 002) y `repo/audit.jsonl` (nuevo). Llegan a `main` por el
  PR de estado.
- **En la PC**: `/srv/debfactory/data` (el antiguo `<data>`, más `ci/` con la copia del
  pipeline), `/srv/debfactory/rclone`, `/srv/debfactory/cache-pub` y los directorios de trabajo.
- **En GitHub**: issues, PRs, logs y resúmenes.

**Testing**: `go test`.

- **Unitarias** (sin red ni Docker):
  - plan y conciliación;
  - transiciones del PR de estado;
  - invariantes de la auditoría;
  - deduplicación de issues, contra un `httptest` que simula la API de GitHub;
  - validación de inputs;
  - inserción de `Valid-Until` y firma.
- **Integración** (build tag `integration`): firma con `Valid-Until` contra gpg y aptly reales;
  `ci main` completo contra un servidor GitHub falso, un remoto git local y el destino `local`.
- **E2E**:
  - `just e2e` actualizado: apt acepta `Valid-Until` y rechaza una generación caducada;
  - el quickstart sobre el repositorio real de GitHub con `r2test`.

**Target Platform**: la PC Debian trixie amd64 (16 núcleos, 31 GB) y GitHub.com. Los clientes
apt no cambian (5 suites × 2 arquitecturas).

**Project Type**: CLI de un solo binario (amplía las fases 001 y 002), más la configuración de
la PC y de los workflows.

**Performance Goals**:

- SC-001: merge → disponible vía apt en menos de 30 min, para un paquete con las cachés de
  `pub` en frío.
- La ejecución de un PR que sube un paquete: menos de 30 min, con imágenes reconstruidas.
- `ci refresh`: menos de 5 min.

**Constraints**:

- Ningún secreto en la forja ni en los logs (FR-015, FR-016).
- Ningún código de PR en el runner `pub` (FR-014).
- Ningún binario upstream en el host: el runner corre en un contenedor.
- `main` solo cambia por PR (FR-017a).
- Las ejecuciones que escriben se serializan (FR-018).
- Idempotencia ante cancelaciones y cortes (FR-029).

**Scale/Scope**: 4 paquetes, pensados para decenas; unos pocos PR al mes; un único mantenedor
y un único runner por nivel. Fuera de alcance:

- detección automática de versiones;
- agentes de IA;
- sitio web;
- nivel 2;
- avisar de que la PC está caída.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Contra la constitución v1.6.0.

| # | Principio | Cómo lo cumple el diseño | Estado |
|---|---|---|---|
| I | Declarativo primero | El pipeline no tiene datos por paquete. Los paquetes afectados salen de los manifiestos y del estado, y los workflows son iguales para cualquier paquete. Añadir uno es añadir su manifiesto y su lockfile. | ✅ |
| II | Dos niveles | Solo se publica nivel 1, como en la fase 002. El plan no hace suposiciones que impidan añadir el nivel 2 después. | ✅ |
| III | Cadena de suministro | **Verificación**: los mismos gates de verificación, con lockfile revisado en git y escrito por una persona. **Firma**: la hace debfactory con la subllave (`Valid-Until` añadido antes de firmar). **Secretos**: solo en `/srv/debfactory`, propiedad de `dfpub`, nunca en la forja (FR-015). **Cadena del pipeline**: runner fijado por sha256, sin actualizaciones automáticas (a verificar con JIT en T021) y actualizado antes de 30 días (aviso de `ci doctor`); `actions/checkout` fijada por sha; ninguna otra action. **Token**: `GITHUB_TOKEN` solo hacia GitHub, con `User-Agent: debfactory`. **Logs**: la salida pasa por `internal/redact`. | ✅ |
| IV | Gate de licencias | Sin cambios: el gate 3 corre en cada ejecución, también en PRs, antes de construir. | ✅ |
| V | No duplicar | Sin cambios. | ✅ |
| VI | Versionado | Una versión publicada no se reescribe: FR-010 en PRs y la comparación en `rebuild`. Una retirada no se reutiliza (etapa `state`). La auditoría deja constancia de cada versión. | ✅ |
| VII | Reproducible y verificable | `main` repite los gates 1–6 en el runner confiable; ningún artefacto de PR llega a publicarse. Cada paquete es todo o nada en sus suites. `rebuild` y la auditoría permiten reconstruir byte a byte desde el commit de cada entrada (SC-006). | ✅ |
| VIII | Automatización | Construir → probar → publicar → sincronizar → verificar sin intervención. Los fallos se notifican con paquete, etapa y motivo (issues). La detección automática de versiones queda para otra fase, y su automatización solo tendrá que abrir un PR. El merge del PR de estado es posterior a la publicación y no la bloquea. | ✅ (detección pendiente) |
| IX | Simplicidad operativa | Todo corre en la PC más el hosting estático; la forja es el único servicio externo, y la spec lo exige. Herramientas de Debian: Docker rootless, systemd, nftables, y en la imagen solo paquetes de trixie. Todo componente se explica en un párrafo y se opera con una orden (`setup.sh`, `just runner-image`, `just ci-doctor`). **Desviación**: el runner de GitHub no está empaquetado en Debian (ver Complexity Tracking). | ⚠️ justificado |
| X | La IA propone | Ningún componente usa IA. La base que pide X ya está: los PR de cualquier origen pasan por los mismos gates, los jobs sin confianza no tienen secretos, y solo el mantenedor integra en `main`. | ✅ |

**Gate (pre-research)**: PASS, con una desviación justificada (IX).

**Re-check (post-design)**: PASS, con la misma desviación. Notas:

- **Fase 002, FR-004a**: los índices dejaban de no caducar ya en la spec de esta fase (aclaración
  2026-11-13, FR-030). Cambian `generate.go` y `verify.go`, y las pruebas de la fase 002 que
  exigían la ausencia de `Valid-Until`. La constitución no fija nada sobre la caducidad.
- **III, frase de paso en archivo**: sigue aceptada (re-check de la fase 002). Su exposición se
  reduce, porque ya no vive bajo el home del mantenedor y solo la ven los jobs de `main`.
- **VIII, notificar fallos**: el único fallo que no se notifica con un issue es "la PC está
  apagada". Queda fuera de alcance por la spec, y el temporizador con `Persistent=true` se pone
  al día al encender. Mitigación sin coste: el correo de GitHub para ejecuciones fallidas o
  canceladas cubre los jobs cancelados tras 24 h en cola (docs/operacion-ci.md).

## Project Structure

### Documentation (this feature)

```text
specs/003-ci-pipeline/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md               # debfactory ci …
│   ├── workflows.md         # pr.yml y main.yml; configuración de GitHub
│   ├── runner-host.md       # usuarios, unidades, hook, nftables, imagen
│   ├── audit.md             # repo/audit.jsonl
│   ├── notificaciones.md    # issues de fallo
│   └── config.md            # secciones ci y repo.validity
└── tasks.md                 # /speckit-tasks
```

### Source Code (repository root)

```text
cmd/debfactory/               # + ci {plan,pr,main,rebuild,retire,refresh,restore-state-pr,notify,audit,doctor}
internal/
├── ci/
│   ├── env.go                # contexto del job (GITHUB_*), nivel de confianza, inputs validados
│   ├── plan.go               # conciliación main/rebuild; diff de PR; FR-010
│   ├── run.go                # orquestación de main, rebuild, retire y refresh (orden de cli.md)
│   ├── pr.go                 # ci pr y resumen por paquete, suite y arquitectura
│   ├── statepr.go            # copia en <data>/ci, rama debfactory/state, transiciones (D8)
│   ├── audit.go              # lectura, escritura e invariantes de audit.jsonl
│   ├── notify.go             # issues con marcador; abrir, comentar, cerrar
│   └── doctor.go             # comprobación de GitHub y del host
├── forge/
│   └── github.go             # cliente REST mínimo (issues, PRs, refs) con net/http; redacta el token
├── repo/                     # + firma propia con Valid-Until (generate.go); verify exige y avisa
├── config/                   # + secciones ci y repo.validity
└── repotest/                 # + E2E de Valid-Until (aceptación y caducidad)
.github/workflows/
├── pr.yml
└── main.yml
ci/
├── runner/
│   ├── Dockerfile            # variantes ci y pub
│   ├── hooks/trusted-gate.sh
│   ├── jitconfig.sh          # ExecStartPre (root): PAT → configuración JIT
│   ├── run-runner.sh         # docker run --rm de la imagen con los montajes del nivel
│   └── cleanup.sh
└── host/
    ├── setup.sh              # usuarios, Docker rootless, unidades, temporizador, nftables, credstore
    ├── migrate-data.sh       # ~/.local/share/debfactory → /srv/debfactory/data
    ├── debfactory-runner@.service
    ├── debfactory-refresh.service
    ├── debfactory-refresh.timer
    └── debfactory-ci.nft
docs/
└── operacion-ci.md           # instalación, operación diaria, recuperación y rotación del PAT
repo/
├── state.yaml
└── audit.jsonl               # nuevo
config/debfactory.yaml        # + ci, repo.validity
justfile                      # + runner-image, ci-doctor, ci-local-pr
```

**Structure Decision**: el mismo módulo y binario.

- `internal/ci` orquesta las etapas que ya existen; no reimplementa build, test ni publish.
- `internal/forge` aísla la API de GitHub, para que cambiar de forja toque un solo paquete.
- `ci/` agrupa lo que corre fuera del binario (host e imagen) y queda separado de `.github/`,
  que solo describe los disparadores.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| IX: el runner de GitHub Actions (`actions/runner`) no está empaquetado en Debian | Es el único agente que ejecuta jobs de GitHub Actions en una máquina propia, y la spec exige runners en la PC | **Un servicio propio que consulte la API y ejecute a mano**: reimplementaría el protocolo de jobs, los logs y los checks. **GitHub-hosted runners**: los prohíbe FR-012. **Otra forja con un runner empaquetado**: el mantenedor eligió GitHub. **Mitigación**: corre dentro de un contenedor desechable, en una versión fijada por sha256, sin actualizaciones automáticas (a verificar con JIT en T021; si las hubiera, la copia se pierde con el contenedor) y sin acceso al host más allá de los montajes de su nivel. |

## Riesgos que se atacan primero

Del [research §3](research.md#3-riesgos-que-se-atacan-primero):

1. Docker-out-of-Docker con Docker rootless: montajes en la misma ruta, `--network none` y qemu
   arm64 desde el contenedor del runner.
2. Las variables que ve el hook `job started` en `push`, `workflow_dispatch` y `pull_request`.
3. `Valid-Until` con firma propia, aceptado sin advertencias por apt en las 5 suites.
4. El tiempo de SC-001 con las cachés de `pub` en frío.

Los cuatro van en las primeras tareas, antes de construir `internal/ci`.
