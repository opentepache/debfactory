---
description: "Task list for 007-configurable-suites"
---

# Tasks: Suites configurables y Debian forky

**Input**: Design documents from `/specs/007-configurable-suites/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/pipeline.md, quickstart.md

**Tests**: se incluyen porque la spec los pide como criterios: suite ficticia sin código (SC-001),
matriz con forky (SC-002), ampliación sin tocar lo publicado (SC-004, SC-005). Como en las fases
anteriores: tests Go junto al código; `integration` para git real, `e2e` para Docker. Lo que se hace
en `packages`, en GitHub o como root es **(manual)**.

**Organization**: US1 (suites desde la configuración) es la base de las demás. US2 (ampliación) y
US3 (portada) se pueden hacer en paralelo tras la fase 2.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia de usuario de la spec (US1–US4)

---

## Phase 1: Setup

- [X] T001 Agregar a `testdata/project/config/debfactory.yaml` los campos nuevos en cada suite (`distro`, `label`, `version`; `catchall: true` en sid) y la suite `forky: {image: "debian:forky", level2_suffix: deb14, distro: debian, label: "Debian 14 (testing)", version: "14"}` entre trixie y sid (data-model §1). En `testdata/project/docs/usuario.md`, la tabla de suites con forky y la nota de sid como en FR-011. Regenerar `testdata/project/dist/` con `bin/debfactory build --root testdata/project --all` y `bin/debfactory test --root testdata/project --all`: `repo.Inputs` exige que cada `test.json` cubra forky (descarga `debian:forky`).

---

## Phase 2: Foundational

**Purpose**: la suite descrita entera en la configuración; todas las historias la usan.

- [X] T002 `Suite` en `internal/config/config.go`: campos `Distro`, `Label`, `Version string` y `Catchall bool`, leídos en `Suites.UnmarshalYAML` (claves `distro`, `label`, `version`, `catchall`; `catchall` acepta solo `true`/`false`). Validación en `Config.validate` (data-model §1): "`distro` obligatorio: `debian` o `ubuntu`"; "`label` obligatorio, una línea"; "`version` obligatorio, una línea, sin espacios"; "`level2_suffix` … único entre las suites"; "a lo sumo una suite `catchall` por `distro`". Mensajes: `suites.forky.label: obligatorio`, `suites.x.distro: "arch" no es debian ni ubuntu`, `suites: más de una suite catchall en debian (sid, forky)`.
- [X] T003 [P] Tests en `internal/config/config_test.go`: `TestLoadRepoConfig` espera 6 suites en el orden de T001 con sus campos; casos nuevos de `TestLoadRejects` para cada mensaje de T002.

**Checkpoint**: la configuración describe cada suite con todo lo que el sitio y la portada necesitan.

---

## Phase 3: User Story 1 - Agregar una suite es solo configuración (Priority: P1) 🎯 MVP

**Goal**: ningún componente tiene suites fijas; el sitio las toma de la configuración.

**Independent Test**: con una suite ficticia agregada solo en la configuración de prueba, el sitio la muestra con su nombre legible y el bloque "Detectar" la incluye.

- [X] T004 [US1] `internal/site/model.go`: quitar `suiteInfo` y `distroNames`; `Load` copia `Distro`, `Label`, `Version` y `Catchall` de `cfg.Suites`. En `site.json` el campo `short` (fase 006) pasa a llamarse `version`, igual que en la configuración; actualizar las plantillas y las pruebas que lo usan. Los grupos de `availability` siguen el orden en que aparece cada `distro` en la configuración y su nombre visible es `distro` con la primera letra en mayúscula ("Debian", "Ubuntu"). Quitar el error "suite sin distribución conocida".
- [X] T005 [P] [US1] Test `TestSuiteFromConfigOnly` en `internal/site/model_test.go`: agregar a la configuración de la fixture una suite `plucky` (`distro: ubuntu`, `label: "Ubuntu 25.04"`, `version: "25.04"`) y una versión publicada en ella; el modelo la muestra con esos datos y `Available` incluye "Ubuntu 24.04, 25.04, 26.04" en el orden de la configuración, sin cambiar código (SC-001). Actualizar las expectativas de `TestLoad` con forky ("Debian 12, 13, 14, sid · Ubuntu 24.04, 26.04").
- [X] T006 [US1] Revisar que ningún otro paquete tenga una lista fija de suites: `grep -rn 'bookworm\|trixie\|noble\|resolute\|"sid"' internal cmd --include=*.go` fuera de tests y comentarios; corregir lo que aparezca para que salga de `cfg.Suites`.
- [X] T007 [P] [US1] `internal/repotest/site_test.go`: quitar el mapa `distro` y usar `cfg.Suites[i].Distro` para elegir `add-debian`/`add-ubuntu`.

**Checkpoint**: el sitio y las pruebas no conocen ninguna suite por su nombre.

---

## Phase 4: User Story 2 - Las versiones publicadas llegan a la suite nueva (Priority: P1)

**Goal**: `main` (solo) y `rebuild` (a mano) amplían la versión actual a las suites que le faltan.

**Independent Test**: en `citest`, publicar, agregar una suite y ejecutar `main`: la versión está en la suite nueva con el mismo sha256 y una entrada `extend`; un fallo en esa suite no cambia nada.

### Tests for User Story 2

- [X] T008 [P] [US2] Tests de la ampliación en `internal/repo/apply_test.go` (research D2): mismo sha256 y suites nuevas → se agregan, sube el serial y el cambio es `add` de esas suites; mismo sha256 y mismas suites (o menos) → sin cambios; sha256 distinto → "ya publicada con otro sha256"; versión que no es la más alta → error "no es la versión más alta; no se amplía" sin cambios; tras ampliar, la retención (`keep`) se aplica en la suite nueva; una versión anterior conservada nunca se amplía aunque traiga la suite (FR-007).
- [X] T009 [P] [US2] Tests de integración en `internal/ci/extend_test.go` (tag `integration`, con `citest` y `mainFixture`): publicar con 5 suites; agregar una a `f.cfg.Suites`; `f.main()` amplía todos los paquetes (log `plan: … ampliar N`, `repo: … + <suite>`), el estado tiene la suite en la versión actual con los mismos `debs`, la auditoría tiene entradas `extend` con `"suites":["<suite>"]` y el sitio (paso `site`) muestra la suite; una segunda ejecución no hace nada; con `failGate` en un paquete, ese paquete no se amplía, los demás sí, y llega la notificación (SC-004, SC-005); `rebuild` de una versión a la que le faltan suites la amplía y de una que no, termina en "nada que publicar"; `rebuild` de una versión retirada no amplía nada; tras un fallo, la ejecución siguiente de `main` reintenta la ampliación y, al lograrla, cierra el issue (FR-014).

### Implementation for User Story 2

- [X] T010 [US2] `internal/repo/apply.go`, caso `Add` con la versión ya en el estado: si todos los `debs` coinciden y `p.Suites` trae suites que la publicada no tiene, agregarlas en el orden de `cfg.Suites` (Change `KindAdd` con solo esas suites) siempre que sea la versión más alta del paquete; si no lo es, `fail(…, "no es la versión más alta; no se amplía")`. Nunca quitar suites. La retención (`retain`) se aplica después como siempre.
- [X] T011 [US2] `internal/ci/audit.go`: `OpExtend = "extend"` y `Entry.Suites []string` (`json:"suites,omitempty"`), escrito en `MarshalJSON` justo después de `version`; `ParseAudit` y `CheckAudit` aceptan `extend` (no cambia qué versiones existen). Test en `internal/ci/audit_test.go`.
- [X] T012 [US2] `internal/ci/plan.go`: `Plan.Extend []AffectedPackage` (con un campo `Suites []string` en `AffectedPackage` o un tipo propio, data-model §3) y `Reason` `extend`. En `PlanMain`, si la versión del manifiesto ya está publicada (`skip == "publicado"`), es la más alta (`ref.Packages[name][0]`) y le faltan suites de `cfg.Suites` que el manifiesto de `HEAD` no excluye, va a `Extend` en vez de a `Skipped`. `Plan.Summary` agrega "ampliar N (pkgs → suites)".
- [X] T013 [US2] `internal/ci/run.go`, `Main`: los paquetes de `p.Extend` entran a `runGates` junto con `p.Publish` (paso 4); los que pasan, a `x.publish` (una sola publicación). Si el sha256 de los gates no es el publicado, `x.fail` con etapa `compare` y mensaje con los dos sha256, y no se publica. Al ampliar, `x.notify.Published` cierra los issues abiertos de ese paquete y versión (FR-014). En `x.publish`, una versión que ya estaba publicada escribe `Entry{Op: OpExtend, Suites: <las agregadas>}` en lugar de `OpPublish` (comparar el estado antes y después). Paso 3b (`checkCode`): no comparar las versiones de `p.Extend`.
- [X] T014 [US2] `internal/ci/manual.go`, `Rebuild`: si la versión está publicada, es la más alta, el sha256 coincide y le faltan suites configuradas no excluidas, seguir por el camino de publicación (copiar `dist/` al checkout y `x.publish`) en vez de "nada que publicar" (contracts/pipeline.md § ci rebuild). Si no es la más alta y le faltan suites: "no es la versión más alta; no se amplía", sin publicar.

**Checkpoint**: agregar una suite a la configuración y ejecutar `main` la llena sola.

---

## Phase 5: User Story 3 - Agregar el repositorio en Debian forky (Priority: P2)

**Goal**: "Detectar" elige forky; la pestaña Debian ofrece las suites de Debian configuradas y sid como comodín.

**Independent Test**: en contenedores de forky y sid, el bloque "Detectar" deja `Suites: forky` e instala los paquetes.

- [X] T015 [US3] `internal/site/commands.go` (research D4): el `case` de "Detectar" lista los codenames de todas las suites sin `catchall`; si no coincide, para cada suite `catchall` de distribución `D`, `[ "$ID" = D ]` la elige (en Debian, además, `grep -qs sid /etc/debian_version`); el de cada distribución lista sus codenames sin `catchall` y cae en su `catchall` si lo tiene. Sin el nombre `sid` escrito en el código.
- [X] T016 [P] [US3] Tests en `internal/site/commands_test.go`: con forky, el `case` de "Detectar" es `bookworm|trixie|forky|noble|resolute` y el de Debian `bookworm|trixie|forky`, con `*) echo sid`; con una configuración sin `catchall`, no hay rama de comodín. Simulación con `sh` (como en la fase 006) de `os-release` de forky, sid (dice forky), Linux Mint 22 (noble), Ubuntu 22.04 (no soportada) y Kali (no soportada).
- [X] T017 [US3] `internal/repotest/site_test.go`: con forky en `testdata/project`, la matriz es 6 × 2; en forky y sid, "Detectar" deja `Suites: forky` (en sid la suite esperada es forky, no sid), y la pestaña Debian deja forky en forky y en sid (SC-002).

**Checkpoint**: la portada funciona en forky, amd64 y arm64.

---

## Phase 6: User Story 4 - Documentación al día (Priority: P3)

- [X] T018 [P] [US4] `docs/operacion-ci.md`: sección "Agregar una suite" (los campos de data-model §1, la imagen oficial, qué hace `main` al integrar el PR, cómo excluir una suite en un manifiesto si testing rompe una prueba, y que retirar una suite exige anunciarlo antes en la guía de usuario).
- [X] T019 [P] [US4] Notas en `specs/003-ci-pipeline/contracts/cli.md` (`ci main` amplía y reintenta; `ci rebuild` amplía), en `specs/002-signed-apt-repo/contracts/cli.md` § publish (publicar otra vez con suites nuevas las agrega) y en `specs/006-repo-website/data-model.md` §1 (las suites del sitio salen de la configuración: `label`, `version`, `catchall`).

---

## Phase 7: Polish & activación

- [X] T020 `just check` y `just e2e` en verde (la E2E con forky: 6 suites × 2 arquitecturas, incluidos `TestE2ERepo`, `TestE2ESite` y `TestE2EA11y`).
- [X] T021 `just tool-install`; rama en `packages` con el pin nuevo, los campos nuevos en las suites existentes, forky entre trixie y sid, y `docs/usuario.md` (tabla de suites con "Debian 14 (testing)" y la nota de que en sid el sistema declara el codename de testing). **(manual)** commit, push y merge del PR (research D7).
- [X] T022 **(manual)** Quickstart §2–§3 en producción: el log de `main.yml` amplía los 5 paquetes a forky; `apt install` en un contenedor de forky instala las versiones publicadas; la portada muestra "Debian 12, 13, 14, sid · Ubuntu 24.04, 26.04"; el PR de estado tiene las entradas `extend`; un `ci rebuild` de cualquiera termina en "nada que publicar" (SC-003, SC-004).

---

## Dependencies & Execution Order

- Fase 1 → Fase 2 (T002 antes que T003) → US1, US2 y US3.
- US1: T004 antes que T005; T006 y T007 en paralelo.
- US2: T008 y T009 primero (fallan); T010 → T011 → T012 → T013 → T014.
- US3: tras T004 (usa `Suite` del modelo); T015 → T016 → T017.
- US4 en cualquier momento. Polish al final.

### Parallel Opportunities

- T003 con T004.
- T005, T007 y T008/T009.
- T018 y T019.

## Implementation Strategy

1. Fases 1–2 y US1: sitio sin suites fijas, demostrado con la suite ficticia.
2. US2: la ampliación en `main` y `rebuild` (lo que hace útil agregar forky).
3. US3: la portada en forky.
4. Documentación, validación completa y activación en `packages`.
