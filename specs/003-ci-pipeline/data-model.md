# Data Model: Pipeline de construcción y publicación

Las entidades de la spec, tal como las implementa el diseño de [research.md](research.md). Los
formatos que se escriben en disco o en la forja tienen su propio contrato en `contracts/`.

## 1. Ejecución (`Run`)

Una corrida de un workflow. No se persiste en git: vive en GitHub (log, resumen y checks). debfactory la
construye a partir del entorno del job.

| Campo | Origen | Notas |
|---|---|---|
| `trigger` | `GITHUB_EVENT_NAME` | `push`, `pull_request` o `workflow_dispatch` |
| `action` | input `action` en `workflow_dispatch`; si no, `main` o `pr` | `main`, `pr`, `rebuild`, `retire`, `refresh`, `restore-state-pr` |
| `commit` | `GITHUB_SHA` | En `pr`, el merge commit del PR |
| `trust` | etiqueta del runner | `ci` o `pub` (research D2) |
| `url` | `GITHUB_SERVER_URL/GITHUB_REPOSITORY/actions/runs/GITHUB_RUN_ID` | Va en la auditoría, los issues y los mensajes |
| `results[]` | etapas de debfactory | Un `StageResult` por paquete, etapa, suite y arquitectura |

**Reglas**:

- `trust: pub` solo con `trigger` en {`push`, `workflow_dispatch`} y `ref == refs/heads/main`.
  Lo comprueba el hook (D3); `ci` vuelve a comprobarlo al empezar y sale con código 3 si no se
  cumple.
- Una ejecución `main` cuyo `commit` ya no es `origin/main` termina como `superseded` (D6).

### StageResult

`{package, version, stage, suite?, arch?, status: pass|fail|skip, reason?}`.

- `stage` toma los valores de las fases 001 y 002 (`manifest`, `fetch`, `verify`, `license`,
  `build`, `lint`, `test`, `inputs`, `sign`, `publish`, `sync`, `verify-remote`) y los nuevos
  `plan`, `state`, `audit`, `notify`, `expiry` (FR-032), `state-pr-age` (FR-007c), `code` y
  `compare` (comprobaciones de código y FR-010 en `main`, FR-005).
- Fallos que no son de un paquete: se registran con `package: pipeline`.

## 2. Plan (`Plan`)

La salida de `debfactory ci plan` ([contracts/cli.md](contracts/cli.md)). Dice qué hay que hacer
en esta ejecución.

| Campo | Tipo | Notas |
|---|---|---|
| `mode` | `main` \| `pr` \| `rebuild` | |
| `reference` | `{source: main\|state-pr, sha256}` | Estado de referencia (D6, D8) |
| `publish[]` | `AffectedPackage` | Solo en `main` y `rebuild` |
| `test[]` | `AffectedPackage` | En `pr`, los paquetes con manifiesto o lockfile cambiados |
| `compare[]` | `{package, version}` | En `pr` con cambios de código o configuración: versiones publicadas que se reconstruyen y comparan (FR-010) |
| `skipped[]` | `{package, reason}` | Por ejemplo: ya publicado, sin cambios |
| `errors[]` | `{package, stage, reason}` | Por ejemplo: versión retirada o lockfile desactualizado |

### AffectedPackage

`{package, version, commit, reason: new-package|new-version|new-revision|rebuild}`.

- `version` es la versión Debian completa (`<upstream>-0~tepache<revision>`).
- `commit` es aquel cuyo manifiesto y lockfile se construyen:
  - en `main`, `HEAD`;
  - en `rebuild`, el que devuelve `restore.FindCommit` (FR-026).

**Reglas de conciliación** (modo `main`, D6):

| Versión del manifiesto en `HEAD` respecto del estado de referencia | Resultado |
|---|---|
| No hay ninguna versión publicada del paquete | `publish`, `new-package` |
| Mayor que la más alta publicada, distinta `upstream` | `publish`, `new-version` |
| Misma `upstream`, `revision` mayor | `publish`, `new-revision` |
| Ya publicada | `skipped: publicado` |
| Retirada | `errors`, etapa `state`: "versión retirada; sube revision" |
| Menor que la más alta publicada y no publicada | `errors`, etapa `state`: "versión anterior a la publicada" |
| El lockfile no tiene la versión del manifiesto | `errors`, etapa `manifest`: "regenera el lockfile con `debfactory lock`" |

Los paquetes son independientes entre sí (FR-004): un error en uno no quita a los demás del
plan.

## 3. Estado de referencia y PR de estado

El **estado del repositorio** sigue siendo `repo/state.yaml` (contrato de la fase 002, sin
cambios de formato). Se añaden tres cosas:

### Copia del pipeline (`/srv/debfactory/data/ci/`)

| Archivo | Contenido |
|---|---|
| `state.yaml` | El último estado que publicó el pipeline |
| `audit.jsonl` | El registro completo tras la última operación |
| `pushed.json` | `{state_sha256, audit_sha256, branch_commit, pr_number, pushed_at, last_main_sha}`: el último push de la rama y el último commit de `main` procesado entero (para el paso 3b de `ci main`) |

### PR de estado

| Campo | Valor |
|---|---|
| Rama | `debfactory/state` (configurable en `ci.state_branch`) |
| Contenido | `origin/main` + 1 commit que escribe `repo/state.yaml` y `repo/audit.jsonl` |
| Autor del commit | `github-actions[bot]` |
| Título | `repo: estado tras serial <N> (<paquetes>)` |
| Cuerpo | tabla de las operaciones pendientes y enlace a cada ejecución |

**Transiciones** (al empezar cada ejecución `pub` que escribe, D8):

```text
            publicar/retirar                     merge del mantenedor
 [sin PR] ─────────────────────▶ [PR abierto] ─────────────────────────▶ [sin PR]
    ▲                               │  ▲   publicar/retirar: actualizar
    │                               │  └──────────────┘
    │     cerrar sin integrar,      ▼
    │     editar o divergir    [discrepancia] ── restore-state-pr ──▶ [PR abierto]
    └────── (no se publica; issue de etapa state hasta resolver) ─────┘
```

| Situación detectada | Acción |
|---|---|
| La rama coincide con la copia (`state_sha256` y `audit_sha256`) | seguir |
| `main` contiene la copia | seguir; borrar la rama si quedó |
| La rama falta o tiene un `serial` menor y su `audit.jsonl` es prefijo del de la copia | volver a empujar y seguir (FR-029) |
| Cualquier otra | etapa `state`: no publicar y notificar (FR-007b) |
| PR abierto hace más de `ci.state_pr_max_age` (7 días) | notificar (FR-007c) y seguir |

## 4. Entrada de auditoría

Una línea de `repo/audit.jsonl`. El contrato completo está en
[contracts/audit.md](contracts/audit.md).

| Campo | Tipo | Notas |
|---|---|---|
| `seq` | entero | Consecutivo desde 1, sin huecos |
| `time` | RFC 3339 UTC | Momento en que la generación se hizo `current` |
| `op` | `publish` \| `retire` \| `import` | `import`: las versiones publicadas a mano antes de esta fase |
| `package`, `version` | texto | |
| `commit` | sha de 40 hex | Commit cuyo manifiesto y lockfile se construyeron. En `retire`, el commit de la ejecución |
| `debs` | `{amd64, arm64}` | sha256. Obligatorio en `publish` e `import` |
| `serial` | entero | Serial del estado tras la operación |
| `run` | URL | Ejecución de GitHub. En `import`, `null` |
| `reason` | texto | Obligatorio en `retire` |

**Invariantes**:

- **Solo añadir**: el registro de `main` es prefijo exacto del de la rama de estado, y este lo
  es de la copia del pipeline (FR-024).
- **Cobertura**: todo `(package, version, arch, sha256)` de `state.yaml` tiene una entrada
  `publish` o `import` con el mismo sha256 (FR-025).
- **Retiros**: un `retire` solo puede referirse a una versión que tiene un `publish` o un
  `import` previo.

## 5. Notificación de fallo

Es un issue de GitHub. El contrato está en
[contracts/notificaciones.md](contracts/notificaciones.md).

| Campo | Valor |
|---|---|
| Clave | `(package, version, stage)`; `version` es `-` en los fallos de `pipeline` |
| Marcador | `<!-- debfactory:failure package=… version=… stage=… -->` en la primera línea del cuerpo |
| Etiqueta | `pipeline-failure` |
| Título | `fallo: <package> <version> en <stage>` |
| Cuerpo | motivo exacto, suite y arquitectura si aplican, enlace al log, commit |

**Ciclo de vida**:

- **Abierto**: se crea con el primer fallo. Cada fallo repetido añade un comentario con el
  nuevo enlace.
- **Cerrado**: cuando una ejecución publica esa versión, se cierran todos los issues abiertos
  de `(package, version)`. Los de etapa `state`, `pipeline` o `expiry` se cierran cuando una
  ejecución pasa esa etapa.

## 6. Runner

No se persiste en git: lo define la configuración del host
([contracts/runner-host.md](contracts/runner-host.md)).

| Campo | `ci` | `pub` |
|---|---|---|
| Usuario del host | `dfci` | `dfpub` |
| Etiqueta | `debfactory-pr` | `debfactory-main` |
| Docker | rootless de `dfci` | rootless de `dfpub` |
| Montajes | `/srv/debfactory/ci/work` | `/srv/debfactory/pub/work`, `/srv/debfactory/data`, `/srv/debfactory/rclone`, `/srv/debfactory/cache-pub` |
| Hook `job started` | no | `trusted-gate.sh` (D3) |
| Limpieza al terminar | trabajo, imágenes y caché | trabajo |
| Red | sin redes privadas (D14) | normal |

## 7. Secretos de publicación

| Secreto | Dónde | Dueño y permisos | Quién lo ve |
|---|---|---|---|
| Subllave de firma | `/srv/debfactory/data/gnupg/` | `dfpub`, `700` | jobs `pub` |
| Frase de paso | `/srv/debfactory/data/gnupg/passphrase` | `dfpub`, `600` | jobs `pub` (vía gpg) |
| Credenciales de R2 | `/srv/debfactory/rclone/rclone.conf` | `dfpub`, `600` | jobs `pub` (vía rclone) |
| PAT de runners | `/etc/credstore/debfactory-gh-runner` | `root`, `600` | `ExecStartPre` de las unidades y el temporizador de refresh |
| `GITHUB_TOKEN` | lo da GitHub en cada job | efímero | el job; solo se envía a `api.github.com` y al remoto git de GitHub |

## 8. Configuración nueva (`config/debfactory.yaml`)

Ver [contracts/config.md](contracts/config.md). Resumen:

- `ci.repository`, `ci.state_branch`, `ci.state_pr_max_age`;
- `ci.labels.{ci,pub}`;
- `ci.failure_label`;
- `ci.code_paths`, que activan FR-010;
- `repo.validity.{days,warn_days}`, de D11.
