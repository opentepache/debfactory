# Implementation Plan: Detección de versiones upstream y actualización automática

**Branch**: `005-upstream-release-watch` | **Date**: 2026-11-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/005-upstream-release-watch/spec.md`

## Summary

Una orden nueva, `debfactory ci watch`, revisa una vez al día los releases de cada paquete en
GitHub. Elige la versión estable más alta que lleve al menos 48 h publicada. Prepara el cambio
(versión, revisión 1, lockfile, gate de licencias) y abre o actualiza **un** PR por paquete. Con
`watch.auto_merge`, y salvo cambio mayor o de licencia, activa el auto-merge nativo. Lo demás
(build, lintian, pruebas, publicación, PR de estado) es el pipeline de 003 sin cambios.

| Pieza | Dónde | Qué hace |
|---|---|---|
| `ci watch` | herramienta (`internal/watch`, `internal/ci`) | ciclo: releases → candidata → propuesta → PR / notificación |
| GitHub App `opentepache-updates` | organización | identidad del detector; sus PR disparan `verify` sin aprobación y su merge dispara `main.yml` (research D1) |
| `watch.yml` | `packages` | job en el runner `ci` (sin llave ni `<data>`), con el token de la App desde el environment `watch` (D2) |
| `debfactory-watch.timer` | PC | dispara `watch.yml` cada hora; el intervalo real (24 h) lo decide la configuración (D3) |
| bloque `watch` | manifiesto y `config/debfactory.yaml` | auto-merge, marca de pre-release, intervalo, espera, reserva de peticiones |

## Technical Context

**Language/Version**: Go 1.26 (`GOTOOLCHAIN=local`), como 001–004. POSIX `sh` para el host.

**Primary Dependencies**: sin dependencias Go nuevas (HTTP de la stdlib, como `internal/forge`;
la mutación GraphQL de auto-merge es un POST JSON). Acciones nuevas fijadas por sha:
`actions/create-github-app-token` v3.3.0, `actions/cache` v6.1.0 (research D14).

**Storage**: ninguno propio (research D7). Campos nuevos en manifiestos y configuración; estado
implícito en PRs, issues y ejecuciones de GitHub; ETags en la caché de Actions.

**Testing**: `go test` con el GitHub falso de `internal/ci/citest` (releases con ETag, límites,
GraphQL de auto-merge) y un upstream simulado en httptest; `-tags integration` para el ciclo
completo sobre un repositorio git de prueba; `just e2e` hasta la publicación local (research
D13).

**Target Platform**: la PC de build (Debian trixie amd64), el runner `ci` (rootless, JIT) y
GitHub (`opentepache`, plan Free).

**Project Type**: CLI y pipeline de CI.

**Performance Goals**: ciclo sin novedades ≤ 1 petición por paquete, casi todas `304` (SC-006);
un ciclo con una propuesta, < 5 min (descarga de dos artefactos + lockfile).

**Constraints**: el detector nunca ve `<data>` ni la llave (FR-012); nada de IA (FR-024); sin
servicios nuevos; sin plan de pago; la App no tiene bypass del ruleset.

**Scale/Scope**: 5 paquetes hoy, diseño para 100 (SC-007). 1 mantenedor.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principio | Cómo se cumple | Estado |
|---|---|---|---|
| I | Declarativo | Auto-merge y regla de pre-release son campos del manifiesto; intervalo, espera y reserva, de la configuración. | ✅ |
| II | Dos niveles | Solo nivel 1 (los manifiestos de nivel 2 se omitirán hasta su fase). | ✅ |
| III | Cadena de suministro | La propuesta usa la orden de lockfile existente: descarga solo desde las URLs del manifiesto, verifica checksums upstream y fija hashes. Un asset resubido con la misma etiqueta se notifica como posible manipulación y bloquea esa versión (`watch-tampered`). La espera de 48 h reduce la exposición a releases comprometidos. El detector no tiene acceso a la llave. | ✅ |
| IV | Licencias | Gate existente sobre el texto exacto de la versión nueva antes de abrir el PR; fuera de la allowlist o no identificable → notificación, sin PR. Un cambio entre licencias permitidas nunca se integra solo. La allowlist no la toca el detector. | ✅ |
| V | No duplicar | Sin cambios; repetir la comprobación por versión queda fuera de alcance (spec, Assumptions). | ✅ |
| VI | Versionado | Versión nueva → `revision: 1` (`<upstream>-0~tepache1`); comparación con `dpkg --compare-versions`; nunca reescribe lo publicado (el pipeline lo impide). | ✅ |
| VII | Reproducible | El PR pasa por `verify` completo (build, lintian, instalación en 5 suites × 2 arquitecturas). | ✅ |
| VIII | Automatización | Cierra la cadena "detectar → … → publicar". Auto-merge por paquete; los fallos se notifican con paquete, etapa y motivo. | ✅ |
| IX | Simplicidad | Sin servicios nuevos: una App de GitHub (configuración), un workflow, un timer como el del refirmado. `ci watch` se explica en un párrafo y se lanza con un botón o una orden. | ✅ |
| X | La IA propone | El detector es determinista y no usa IA; sus PR pasan por las mismas validaciones que uno humano; sin IA disponible todo funciona igual. | ✅ |
| — | Solo versiones estables | Filtro de marcadores en la versión que ningún manifiesto puede desactivar (research D5). | ✅ |

**Re-check tras el diseño**: sin violaciones de la constitución. Hay una desviación de un
contrato de la fase 003 (secretos en un workflow), justificada en Complexity Tracking.

## Project Structure

### Documentation (this feature)

```text
specs/005-upstream-release-watch/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md                    # ci watch, barrera, salida, ci doctor
│   ├── workflows.md              # watch.yml, GitHub App, environment, timer
│   └── pr-y-notificaciones.md    # rama, título, cuerpo, comentarios, etapas de issue
├── checklists/requirements.md
└── tasks.md                      # /speckit-tasks
```

### Source Code

Repositorio `opentepache/debfactory`:

```text
internal/watch/            # nuevo: releases → estables → candidata (D5, D6), mayor (FR-015a), licencia (D8)
internal/forge/            # releases con ETag y límite; ramas/commits de la rama; GraphQL auto-merge; ejecuciones de un workflow
internal/ci/watch.go       # orden ci watch: barrera, ciclo, PR (D11), notificaciones (D12), resumen
internal/ci/doctor.go      # App, environment, auto-merge, timer
internal/ci/citest/        # GitHub falso: releases, ETag/304, límites, refs, GraphQL
internal/manifest/         # bloque watch (tipos, validación, esquema)
internal/config/           # bloque watch
internal/report/           # etapas watch-*
schema/manifest.v1.schema.json
ci/runner/dispatch.sh      # dispatch-refresh.sh generalizado
ci/host/debfactory-watch.{service,timer}
ci/host/setup.sh           # instala y habilita el timer
testdata/project/          # bloque watch en config y un manifiesto con auto-merge
docs/operacion-ci.md       # crear la App, environment, auto-merge, timer
cmd/debfactory/            # registrar ci watch
```

Repositorio `opentepache/packages`:

```text
.github/workflows/watch.yml
config/debfactory.yaml     # bloque watch
packages/*.yaml            # watch.auto_merge cuando el mantenedor lo decida
```

**Structure Decision**: la lógica pura (qué es estable, cuál es la candidata, si es mayor, si la
licencia cambió) va en un paquete nuevo `internal/watch`, sin E/S, para probarla en tabla. La
orquestación con GitHub y git vive en `internal/ci`, junto al resto del pipeline.

## Complexity Tracking

| Desviación | Por qué | Alternativa más simple rechazada |
|---|---|---|
| `watch.yml` lee secretos (la llave de la App), contra el contrato de 003 "ningún workflow lee `secrets.*`" | Sin un token que no sea el `GITHUB_TOKEN`, los PR del detector quedan esperando aprobación y su merge no dispara `main.yml` (research D1): el camino feliz exigiría dos acciones humanas por versión | `GITHUB_TOKEN` + aprobación manual (rompe VIII); PAT del mantenedor (más permisos, personal, caduca). Mitigación: environment limitado a `main`, runner `ci` sin `<data>`, token de 1 h reducido a Contents y Pull requests |
| Una cuenta nueva no humana (la App) con escritura en `packages` | Misma razón | — La App no tiene bypass: no puede tocar `main` ni integrar sin `verify` |

## Riesgos que se atacan primero

1. **Spike de la App** (antes de escribir `ci watch`): crear la App, el environment y un
   `watch.yml` mínimo que abra un PR de prueba con el token de la App sobre una rama
   `debfactory/update/spike`. Comprobar:
   - `verify` corre sin "Approve workflows to run";
   - el auto-merge activado con el token de la App integra tras `verify`;
   - ese merge dispara `main.yml` (que no publica nada: el PR solo cambia un comentario);
   - el ajuste del ruleset `require_extra_approval_for_unattributed_changes` no bloquea el merge.
   Si alguno falla, se revisa research D1 antes de seguir.
2. **Secretos en el runner `ci`**: el environment `watch` no se expone a un job de
   `pull_request` (probar con un PR que intente leerlo: vacío).
3. **Upstreams reales**: `ci watch --dry-run` sobre los 5 paquetes actuales antes de activar el
   timer, para comprobar la inversión de la plantilla `tag` y el filtro de estables con datos
   reales (incluido el caso de `aqua`, que publica muchas versiones seguidas).
