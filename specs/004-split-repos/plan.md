# Implementation Plan: Separar la herramienta y los paquetes en dos repositorios

**Branch**: `004-split-repos` | **Date**: 2026-11-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/004-split-repos/spec.md`

## Summary

**Dos repositorios.** `opentepache/debfactory` (privado, el actual) se queda con la
herramienta. `opentepache/packages` (público, nuevo) recibe lo que debfactory lee al ejecutarse
(`packages/`, `config/`, `repo/`, `docs/usuario.md`) y los workflows. La protección de `main` y
la aprobación de forks quedan en el repositorio público, gratis (research D1).

**La herramienta llega compilada, nunca como código.**

| Pieza | Dónde | Qué hace |
|---|---|---|
| `debfactory.version` | raíz de `packages` | commit fijado de la herramienta; cambia solo por PR |
| `just tool-install` | `debfactory` | árbol limpio + todas las pruebas + compilación con el commit embebido + instalación en `/srv/debfactory/tools/<commit>/` (root) |
| `/srv/debfactory/tools` | PC | varias versiones a la vez; montado `readonly` en los dos runners |
| lanzador `/usr/local/bin/debfactory` | imagen del runner | lee `debfactory.version` y ejecuta esa versión, o falla nombrando la que falta |
| autocomprobación | binario | en CI, se niega si su commit no es el fijado (etapa `tool`) |

**Subir la herramienta** es instalar la versión nueva (la anterior sigue) y abrir un PR en
`packages` que cambia `debfactory.version`. El check reconstruye todo lo publicado, compara los
sha256 y enlaza las diferencias entre versiones. Hasta el merge, `main` sigue con la anterior
(research D3, D6).

**Auditoría**: campo `tool` en cada entrada nueva (D7).

**Pruebas de la herramienta**: copia congelada en `testdata/project/` (D9).

**Migración**: `git filter-repo` para `packages`, conservando la autoría; un commit de borrado
en `debfactory`, sin reescribir su historia (D8, contracts/migracion.md).

## Technical Context

**Language/Version**: Go 1.26 (`/usr/lib/go-1.26`, `GOTOOLCHAIN=local`), igual que 001–003.
POSIX `sh` para el lanzador y los scripts del host.

**Primary Dependencies**: sin dependencias Go nuevas. En la PC, `git-filter-repo` (Debian) solo
para la migración.

**Storage**: archivos. `debfactory.version` (una línea), `/srv/debfactory/tools/<commit>/`,
`repo/audit.jsonl` con el campo `tool`.

**Testing**: `go test` (unitarias), `-tags integration` (`internal/ci`, `internal/repo`,
`internal/sign`), `just e2e`, todas contra `testdata/project/`. `sh` para el lanzador (prueba
como la de `trusted-gate_test.sh`).

**Target Platform**: la PC de build (Debian trixie amd64) y GitHub (`opentepache`, plan Free).

**Project Type**: CLI y pipeline de CI.

**Performance Goals**: un PR que fija una versión no instalada falla en < 1 min (SC-004). Sin
Go en la imagen, los jobs dejan de descargar módulos y compilar (≈ 30–60 s menos por job).

**Constraints**: ningún job con credenciales para el repositorio privado (FR-010); sin plan de
pago (FR-003); el estado publicado no cambia al migrar (FR-017).

**Scale/Scope**: 2 repositorios, 4 paquetes, 1 mantenedor. Unas 20 pruebas cambian de raíz.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principio | Cómo se cumple | Estado |
|---|---|---|---|
| I | Declarativo primero | Los manifiestos no cambian de formato; solo de repositorio. | ✅ |
| II | Dos niveles | Sin cambios. | ✅ |
| III | Cadena de suministro | **Herramienta**: fijada por commit en un archivo revisado por PR, compilada por el mantenedor desde un árbol limpio con las pruebas en verde, instalada como root y montada de solo lectura; doble comprobación (lanzador y autocomprobación). **Secretos**: sin cambios; ningún job recibe credenciales del repositorio privado. **Llave**: no está en ningún repositorio. | ✅ |
| IV | Licencias | `config/licenses.yaml` va al repositorio público y solo cambia por PR revisado. | ✅ |
| V | No duplicar | Sin cambios. | ✅ |
| VI | Versionado | Sin cambios. | ✅ |
| VII | Reproducible | La auditoría registra commit de `packages` y de la herramienta: todo `.deb` se puede reconstruir. Subir la herramienta compara todo lo publicado antes de publicar. | ✅ |
| VIII | Automatización | El camino feliz no cambia. Subir la herramienta añade un PR (como cualquier cambio de código en 003). | ✅ |
| IX | Simplicidad | Sin servicios nuevos. Piezas nuevas explicables en un párrafo y operables con una orden: `just tool-install`, el lanzador (≈ 20 líneas de `sh`). `git-filter-repo` es de Debian. | ✅ |
| X | La IA propone | Sin cambios: las mismas validaciones para todo PR. | ✅ |

**Re-check tras el diseño**: sin violaciones. Complexity Tracking vacío.

## Project Structure

### Documentation (this feature)

```text
specs/004-split-repos/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── herramienta.md      # debfactory.version, tool-install, lanzador, autocomprobación
│   ├── migracion.md        # pasos y comprobaciones
│   └── cambios-003.md      # qué cambia en los contratos de la fase 003
├── checklists/requirements.md
└── tasks.md                # /speckit-tasks
```

### Source Code

Repositorio `opentepache/debfactory` (privado), tras la fase:

```text
cmd/debfactory/              # + orden version; autocomprobación en ci
internal/version/            # nuevo: Commit (ldflags)
internal/ci/                 # sin etapa code; etapa tool; campo tool en la auditoría; enlace de diferencias; doctor
internal/config/             # ci.tool_repository; ci.code_paths por defecto nuevo
internal/testroot/           # nuevo: constante con la ruta a testdata/project para las pruebas
ci/runner/                   # Dockerfile sin Go y con el lanzador; launcher.sh (+ prueba); embed.go (runner.env)
ci/host/                     # run-runner.sh con el montaje de tools; setup.sh crea /srv/debfactory/tools
testdata/project/            # nuevo: copia congelada de config/, packages/, repo/, docs/usuario.md
docs/operacion-ci.md         # dos repositorios, tool-install, subir la herramienta
justfile                     # root := ../packages; tool-install; check/e2e con testdata/project
specs/
```

Desaparecen de este repositorio: `packages/`, `config/`, `repo/`, `docs/usuario.md`,
`.github/`.

Repositorio `opentepache/packages` (público, nuevo):

```text
packages/                    # manifiestos y lockfiles (historia conservada)
config/debfactory.yaml       # ci.repository, ci.code_paths, ci.tool_repository nuevos
config/licenses.yaml
repo/state.yaml              # y audit.jsonl cuando el pipeline publique
docs/usuario.md
debfactory.version
.github/workflows/{pr,main}.yml
README.md                    # qué es, cómo contribuir (lockfiles: D10), enlace a docs/usuario.md
LICENSE                      # MIT (research D8)
```

**Structure Decision**: dos repositorios hermanos en `~/Develop`. El código no cambia de
organización interna; solo se mueve qué datos lee y desde dónde.

## Complexity Tracking

Sin violaciones de la constitución.

## Riesgos que se atacan primero

1. **Pruebas sin la raíz real** (research §3.1): cambiar las ~20 referencias a `"../.."` por
   `testdata/project` y comprobar en un clon limpio sin `packages`. Va antes que cualquier otra
   tarea porque valida que la herramienta es separable.
2. **Lanzador y montaje** con el Docker rootless de `dfci` (como el spike T019): el binario
   montado de solo lectura se ejecuta y `touch` en el montaje falla.
3. **Primera ejecución en `packages`**: `ci plan` sobre el clon filtrado antes de crear el
   repositorio en GitHub (contracts/migracion.md, paso 4).
