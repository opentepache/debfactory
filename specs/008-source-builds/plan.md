# Implementation Plan: Paquetes de nivel 2 (compilación desde el código fuente)

**Branch**: `008-source-builds` | **Date**: 2026-11-26 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/008-source-builds/spec.md`

## Summary

Un manifiesto de nivel 2 declara el código fuente de upstream, la justificación y una carpeta
`debian/` estándar. Por cada suite y arquitectura, la herramienta genera el paquete fuente
(`3.0 (quilt)`, changelog propio, versión `+<sufijo>`) y lo compila con sbuild en modo `unshare`,
dentro de un contenedor desechable sin montajes ni credenciales, en tmpfs, dos veces para comprobar
que es reproducible; pasa lintian, piuparts y la prueba de instalación; y publica fuente y binarios
con aptly. El estado pasa a `schema: 2` sin tocar las entradas de nivel 1.

**Ajustes al stack propuesto**:
- **RAM**: la PC tiene 31 GB (constitución IX), no 64; el tmpfs y la concurrencia se dimensionan
  para eso (research D2).
- **Compilación cruzada**: fuera de esta fase, como dice la spec; impide que `apt-get source` en un
  sistema arm64 reproduzca el binario (D4). arm64 se compila con qemu-user.
- **sbuild `unshare` dentro de un contenedor rootless** es el mayor riesgo técnico: un spike lo
  comprueba primero, con una alternativa documentada (D1).

## Technical Context

**Language/Version**: Go 1.26 (`GOTOOLCHAIN=local`).

**Primary Dependencies**: sin dependencias Go nuevas. En imágenes de construcción, de cada suite:
`sbuild`, `mmdebstrap`, `devscripts`, `lintian`, `piuparts`; Docker rootless y qemu-user como hoy.

**Storage**: `repo/state.yaml` `schema: 2`; auditoría con `builds`; `.buildinfo` publicado.

**Testing**: unitarias (manifiesto, lockfile, estado v2, changelog, sufijos, orden de versiones);
integración `citest` (plan, publicación y ampliación de nivel 2 con builds simulados); E2E con
`tepache-hello` en bookworm y trixie × amd64/arm64 (instalación, `apt-get source`, recompilación).

**Target Platform**: la PC Debian (16 núcleos, 31 GB), runners `ci` y `pub`.

**Project Type**: CLI y pipeline existentes.

**Performance Goals**: `tepache-hello` en bookworm, trixie y noble × 2 arquitecturas × 2 compilaciones
en menos de 15 min en la E2E, arm64 emulado incluido; medir y anotar en research D4.

**Constraints**: aislamiento FR-005; reproducibilidad FR-008; nivel 1 intacto FR-014; tmpfs ≤ 18 GB
en total.

**Scale/Scope**: 1 paquete de prueba; 6 suites configuradas (las que el manifiesto no excluya).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principio | Cómo se cumple | Estado |
|---|---|---|
| I. Declarativo | Manifiesto + `debian/` estándar; changelog y versiones los genera la herramienta | ✅ |
| II. Dos niveles | Nivel 2 solo con `justification`; fuente publicado que corresponde exactamente al binario (compilado desde ese `.dsc`) | ✅ |
| III. Cadena de suministro | Fuente verificado por checksum antes de compilar; compilación aislada sin llave ni red; mirrors oficiales | ✅ |
| IV. Licencias | Gate sobre el fuente verificado, antes de compilar | ✅ |
| V. No duplicar | El paquete de prueba nunca se publica | ✅ |
| VI. Versionado | `<upstream>-0~tepache<N>+<level2_suffix>`; orden probado con `dpkg --compare-versions` | ✅ |
| VII. Reproducible | Doble compilación idéntica, `.buildinfo`, lintian, piuparts, instalación por suite; falla en una, no se publica en ninguna | ✅ |
| VIII. Sin humanos | Construcción, publicación y ampliación a suites nuevas automáticas | ✅ |
| IX. Simplicidad | Herramientas empaquetadas en Debian; misma PC; un comando (`debfactory build`); tmpfs dentro de los 31 GB | ✅ |
| X. IA | No aplica | ✅ |

Sin violaciones. Re-check tras la fase 1: el diseño no agrega servicios ni secretos; el riesgo de
compilar código de upstream en el runner `pub` es el mismo modelo que las pruebas de instalación de
nivel 1 (research D15). ✅

## Project Structure

### Documentation (this feature)

```text
specs/008-source-builds/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/build.md
├── checklists/requirements.md
└── tasks.md          # /speckit-tasks
```

### Source Code (repository root)

```text
schema/manifest.v1.schema.json     # level 2: justification, source, build; prohibidos de nivel 1
internal/manifest/                 # tipos y validación de nivel 2; carpeta debian/
internal/lock/                     # versions.<v>.source.sha256
internal/fetch/                    # descarga y verificación del tarball de fuente
internal/srcpkg/                   # nuevo: changelog, .orig, dpkg-source -b
internal/sbuild/                   # nuevo: imágenes de construcción, sbuild unshare, doble build, tmpfs, limitador
internal/build/                    # despacha nivel 1 / nivel 2; build.json por suite
internal/installtest/              # prueba el .deb de cada suite; piuparts
internal/repo/                     # estado schema 2, Inputs/Apply/Generate con .dsc y builds por suite
internal/sync/, internal/repo/verify.go   # un nombre, un contenido
internal/ci/                       # plan y ampliación de nivel 2 (extend por suite)
internal/site/                     # versión por suite y apt-get source
internal/repotest/                 # E2E de nivel 2 y recompilación
testdata/project/packages/tepache-hello{.yaml,.lock.yaml,/debian/}   # paquete de prueba
testdata/fixtures/tepache-hello-1.0.0/                               # su "upstream"
docs/operacion-ci.md               # nivel 2: imágenes, tmpfs, fallos
```

**Structure Decision**: dos paquetes nuevos (`srcpkg` y `sbuild`) para lo que no existe; el resto
son extensiones de los existentes, con el nivel 1 como camino por defecto sin cambios.

## Riesgos

1. **sbuild `unshare` en Docker rootless** (D1): spike primero; alternativa con
   `dpkg-buildpackage` y desconexión de red.
2. **Tiempo de arm64 emulado**: paquete de prueba mínimo; medir; un paquete real pesado puede
   necesitar un runner arm64 nativo (enmienda al principio IX, fuera de esta fase).
3. **Estado `schema: 2`**: migración en la lectura (`schema: 1` sigue siendo válido y se escribe
   como 2 solo cuando hay una versión de nivel 2); pruebas de ida y vuelta.
4. **Reproducibilidad de paquetes reales**: muchos no son reproducibles sin parches; la doble
   compilación lo detecta y bloquea antes de publicar.

## Complexity Tracking

Sin violaciones que justificar.
