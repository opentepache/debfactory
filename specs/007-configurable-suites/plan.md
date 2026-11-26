# Implementation Plan: Suites configurables y Debian forky

**Branch**: `007-configurable-suites` | **Date**: 2026-11-26 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/007-configurable-suites/spec.md`

## Summary

Cada suite se describe entera en la configuración (distribución, nombre legible, versión y si es
la suite "comodín" de su distribución), y el sitio deja su tabla fija. Publicar otra vez una versión
con los mismos `.deb` y suites nuevas aprobadas las agrega. `ci main` detecta solo las versiones
actuales a las que les falta una suite configurada, las reconstruye y prueba, y las amplía en la
misma publicación. `ci rebuild` hace lo mismo a mano. Con eso, forky es un PR de configuración en
`packages`.

| Pieza | Dónde | Cambio |
|---|---|---|
| suites | `internal/config` | campos `distro`, `label`, `version`, `catchall` (research D1) |
| ampliación | `internal/repo/apply.go` | mismo sha256 + suites nuevas → se agregan (D2) |
| plan y gates | `internal/ci/plan.go`, `run.go`, `manual.go` | lista `extend` en `main`; `rebuild` amplía (D3) |
| auditoría | `internal/ci/audit.go` | operación `extend` con `suites` (D6) |
| sitio | `internal/site` | suites desde la configuración; "Detectar" con codenames y `catchall` (D4) |
| pruebas | `internal/*_test.go`, `internal/repotest` | suite ficticia; matriz con forky (D5) |
| `packages` | config y guía | forky y los campos nuevos (D7) |

## Technical Context

**Language/Version**: Go 1.26 (`GOTOOLCHAIN=local`).

**Primary Dependencies**: ninguna nueva. aptly, rclone y Docker como hasta ahora; imagen oficial
`debian:forky` para las pruebas.

**Storage**: `repo/state.yaml` sin cambio de formato; `repo/audit.jsonl` con la operación nueva
`extend`.

**Testing**: unitarias e integración (`citest`) con una suite ficticia; E2E con forky en
`testdata/project` (6 suites × 2 arquitecturas).

**Target Platform**: la PC Debian; runners `ci` y `pub`.

**Project Type**: CLI y pipeline existentes.

**Performance Goals**: la ejecución que agrega una suite construye y prueba cada paquete una vez
(no dos: research D3); con 5 paquetes y 6 suites, del orden de la comparación de la fase 004.

**Constraints**: principio VII sobre todas las suites configuradas (FR-012); las versiones no se
reescriben (FR-005); solo la versión más alta se amplía (FR-007).

**Scale/Scope**: 6 suites, 5 paquetes; cualquier suite futura sin código.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principio | Cómo se cumple | Estado |
|---|---|---|
| I. Declarativo | Las suites y su presentación son configuración (FR-001, FR-002) | ✅ |
| II. Dos niveles | Sin cambios; `level2_suffix` sigue por suite | ✅ |
| III. Cadena de suministro | Mismos archivos, mismos gates, verificación de sha256 antes de ampliar | ✅ |
| IV. Licencias | Sin cambios: la versión ya pasó el gate | ✅ |
| V. No duplicar | No aplica | ✅ |
| VI. Versionado | La ampliación no crea ni reescribe versiones (FR-005). "Un mismo `.deb` en todas las suites no excluidas": tras agregar una suite, la versión actual queda pendiente hasta ampliarse; `main` lo reintenta en cada ejecución y el aviso sigue abierto hasta que se amplía o se excluye la suite (FR-014). Es transitorio y visible, nunca un estado aceptado | ✅ |
| VII. Reproducible | Se amplía solo si el `.deb` reconstruido es idéntico y pasa en todas las suites; forky cuenta como las demás (FR-012) | ✅ |
| VIII. Sin humanos | `main` amplía solo (FR-013) | ✅ |
| IX. Simplicidad | Ninguna operación nueva en el repositorio: es la regla de "ya publicada" (D2) | ✅ |
| X. IA | No aplica | ✅ |
| Alcance v1.8.0 | forky: Debian, imagen oficial, mismas pruebas, principio VII | ✅ |

Sin violaciones. Re-check tras la fase 1: el diseño no agrega servicios, dependencias ni secretos. ✅

## Project Structure

### Documentation (this feature)

```text
specs/007-configurable-suites/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/pipeline.md
├── checklists/requirements.md
└── tasks.md          # /speckit-tasks
```

### Source Code (repository root)

```text
internal/config/config.go          # Suite: Distro, Label, Version, Catchall; validación
internal/repo/apply.go             # Add sobre versión publicada: ampliar suites
internal/ci/plan.go                # Plan.Extend; PlanMain la calcula
internal/ci/run.go                 # gates y publicación de extend; 3b sin duplicar
internal/ci/manual.go              # rebuild amplía
internal/ci/audit.go               # OpExtend, Entry.Suites
internal/site/model.go             # sin suiteInfo: Suite desde cfg
internal/site/commands.go          # Detectar/Debian/Ubuntu desde cfg y catchall
internal/repotest/site_test.go     # sin mapa distro
testdata/project/config/debfactory.yaml   # campos nuevos y forky
docs/operacion-ci.md               # agregar una suite
```

En `opentepache/packages`: `config/debfactory.yaml` y `docs/usuario.md` (research D7).

**Structure Decision**: cambios en los paquetes existentes; nada nuevo.

## Riesgos

1. **Forky (testing) rompe una prueba por causas ajenas**: bloquea la publicación de ese paquete en
   todas las suites (FR-012). Mitigación documentada: reintentar o excluir forky en su manifiesto.
2. **El `.deb` no se reproduce al ampliar** (p. ej. la herramienta cambió desde que se publicó):
   la ampliación falla con `compare` y no toca nada; se resuelve con una revisión nueva del paquete.
3. **Configuración vieja en `packages`**: la herramienta nueva exige los campos nuevos de `suites`;
   el PR de activación los agrega junto con el pin.

## Complexity Tracking

Sin violaciones que justificar.
