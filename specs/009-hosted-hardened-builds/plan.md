# Implementation Plan: Nivel 2 con toolchains de upstream, compilación hospedada y cadena de suministro endurecida

**Branch**: `009-hosted-hardened-builds` | **Date**: 2026-11-27 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/009-hosted-hardened-builds/spec.md`

## Summary

El nivel 2 de la fase 008 se amplía en tres frentes:
1. **Toolchains y dependencias de upstream verificados**: un manifiesto declara un toolchain oficial
   (Zig 0.17.0, sha256 y firma minisign fijados) y la herramienta resuelve, una por una, las
   dependencias de Zig con el hash que declara upstream; viajan dentro del paquete fuente como
   componente `.orig-zigdeps`, así `apt-get source` sigue bastando para recompilar.
2. **GitHub compila y prueba; la PC verifica, firma y publica** (constitución 1.11.0): las
   compilaciones de nivel 2 (suite × arquitectura × variante) y la matriz de pruebas de todos los
   niveles corren en paralelo en runners estándar gratuitos, sin secretos, con atestación; las PR
   corren enteras en GitHub y el runner `ci` se retira. La PC verifica de forma independiente
   (atestaciones, doble compilación, lintian, reconstrucción del nivel 1, prueba de humo amd64),
   firma y publica; si GitHub falla, lo hace todo en local. Se agrega `exclude.arches` y la
   ampliación por arquitectura.
3. **Cadena de suministro**: tarball de release contra tag de git (excepciones por ruta exacta con
   sha256), llaves de firma de upstream fijadas (obligatorias si upstream firma; también nivel 1),
   procedencia de upstream verificada si existe; Scorecard y resumen con IA (GitHub Models) como
   avisos en la PR.

4. **Transparencia**: herramienta pública, página de procedencia por versión, `debfactory verify`
   para terceros, commits firmados en `packages`, y cada paquete autocontenido en `packages/<p>/`.

Primer paquete real: ghostty 1.4.0 en trixie y forky, amd64 y arm64.

## Technical Context

**Language/Version**: Go 1.26 (`GOTOOLCHAIN=local`); sin dependencias Go nuevas.

**Primary Dependencies**: `cosign`, `minisign`, `gpgv` (empaquetados en Debian) en la PC y en las
imágenes de construcción; Zig 0.17.0 oficial solo dentro de contenedores; GitHub Actions
(`actions/upload-artifact`, `download-artifact`, `attest-build-provenance`) en los workflows de
`packages`; GitHub Models y la API pública de OpenSSF Scorecard en jobs de PR sin secretos.

**Storage**: manifiesto (`exclude.arches`, `signing`, `toolchain`, `deps`, `source.extra_files`),
lockfile (toolchain, dependencias, tag, extras, `zigdeps_sha256`), estado (`arches`, componentes del
`orig`), auditoría (`arches`, `toolchain`).

**Testing**: unitarias (ZON, cierre transitivo con un servidor falso, lockfile, firmas con llaves de
prueba, tarball contra tag, `exclude.arches`, estado y auditoría); integración (`accept` con
atestaciones de prueba firmadas por una CA local de cosign o con bundles grabados; `ci` con
artefactos simulados; ampliación por arquitectura); E2E local de ghostty en amd64 con contenedores
(red solo en la preparación); validación real en una PR de `packages` con runners hospedados.

**Target Platform**: PC Debian (solo el runner `pub`) + runners estándar de GitHub (`ubuntu-24.04`,
`ubuntu-24.04-arm`).

**Project Type**: CLI y pipeline existentes.

**Performance Goals**: ghostty, 8 compilaciones en paralelo en < 30 min de reloj (SC-002);
preparación del fuente < 10 min.

**Constraints**: ningún job hospedado con secretos ni escritura del repositorio (SC-005); nivel 1
byte a byte igual (SC-006); repositorio de paquetes público (FR-012); memoria del runner 16 GB.

**Scale/Scope**: 1 paquete real nuevo (ghostty), 4 manifiestos de nivel 1 con firma o procedencia
fijada (buf y aqua; fnm y bob sin firma de upstream), tepache-hello sigue igual.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principio | Cómo se cumple | Estado |
|---|---|---|
| I. Declarativo | Toolchain, dependencias, firmas, excepciones y exclusiones en manifiesto y lockfile | ✅ |
| II. Dos niveles | ghostty justificado (sin binarios de Linux); fuente publicado con sus dependencias | ✅ |
| III. Cadena de suministro | Toolchain y dependencias del lenguaje verificadas, llaves fijadas, procedencia, tarball contra tag, commits firmados (1.11.0) | ✅ |
| IV. Licencias | Gate sobre el fuente verificado, igual que 008 (las dependencias de Zig: se registran sus licencias en el informe; ver Riesgos) | ✅ |
| V. No duplicar | ghostty: no está en Debian/Ubuntu; griffo-free con 1.3.1, solo amd64 y dos suites (no al día) | ✅ |
| VI. Versionado | `1.4.0-0~tepache1+deb13`/`+deb14`; ampliación por arquitectura sin reescribir | ✅ |
| VII. Reproducible | Doble compilación en jobs distintos e idéntica; pruebas atestadas por suite y arquitectura no excluida (1.9.0); transparencia: herramienta pública, procedencia por versión, `verify` (1.11.0) | ✅ |
| VIII. Sin humanos | Todo automático; las llaves nuevas de upstream sí requieren revisión humana, a propósito | ✅ |
| IX. Simplicidad | Runners estándar gratuitos sin secretos ni escritura; verificación independiente y respaldo local en la PC; un solo runner propio; `cosign`/`minisign` de Debian (1.11.0) | ✅ |
| X. IA | Resumen con GitHub Models en job sin secretos ni escritura; lo publica un job sin IA; no bloquea | ✅ |

**Gate**: pasa. Las enmiendas necesarias (III, VII, IX) están en la constitución 1.11.0, y la 1.11.1
aclara que los runners de GitHub pueden construir paquetes de cualquier nivel (análisis C1).
Re-check tras el diseño: sin violaciones.

## Project Structure

### Documentation (this feature)

```text
specs/009-hosted-hardened-builds/
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
schema/manifest.v1.schema.json     # exclude.arches, signing, toolchain, deps, source.extra_files
internal/manifest/                 # validación; origen de source.url ampliado con firma fijada
internal/lock/                     # toolchain, deps, tag, extras, zigdeps
internal/zon/                      # nuevo: lector de build.zig.zon (url/hash/lazy/path)
internal/supply/                   # nuevo: firmas (minisign/gpgv/cosign), procedencia, tarball contra tag, Scorecard, informe
internal/srcpkg/, internal/srcbuild/  # componente .orig-zigdeps, toolchain en el contenedor, modo job hospedado
internal/build/                    # level2 source/build; nivel 1 con firmas y procedencia
internal/ci/                       # accept (verificación independiente), plan y matriz para GitHub, ampliación por arquitectura, review, doctor (commits firmados, sin runner ci)
internal/repo/                     # arches por versión, componentes del orig, ampliación por arquitectura
internal/site/                     # arquitecturas y motivo; páginas de procedencia (provenance.json)
internal/verify/                   # nuevo: debfactory verify para terceros
cmd/debfactory/                    # órdenes level2, supply
ci/workflows/ (plantillas)         # main.yml y pr.yml de packages con los jobs hospedados
ci/runner/                         # retiro del runner ci (setup.sh --remove ci)
testdata/fixtures/level2/          # casos alterados para SC-003
docs/operacion-ci.md, docs/usuario.md (plantilla)  # cadena de suministro y qué garantiza OpenTepache
```

**Structure Decision**: dos paquetes nuevos (`zon`, `supply`) para lo que no existe; el resto amplía
lo de 008. Los workflows los empuja el mantenedor (el token del asistente no puede).

## Riesgos

1. **Herramienta pública** (research D5): el mantenedor cambia la visibilidad de
   `opentepache/debfactory` antes de activar los workflows; la revisión del historial no encontró
   secretos ni datos de sesiones.
2. **Licencias de las dependencias de Zig**: el gate de 008 mira la licencia del fuente; ghostty
   incluye ~40 dependencias con licencias propias. Se registran en el informe y en
   `debian/copyright`; si alguna no está en la allowlist, falla (principio IV).
3. **Memoria del runner** (16 GB, ghostty 11,8 GB): si falla, se excluye arm64 o amd64 con motivo.
4. **Cambios en GitHub** (cuotas, etiquetas, precios): FR-012 y la vuelta a la PC están previstos.
5. **Tarball con muchos archivos generados**: se mide primero (quickstart §1).

## Complexity Tracking

| Desviación | Por qué hace falta | Alternativa más simple descartada |
|---|---|---|
| Descargas fuera del release (toolchain, dependencias de Zig) | ghostty no compila sin ellas | Esperar a que Debian empaquete Zig y las dependencias: sin plazo |
| Jobs fuera de la PC | arm64 emulado tarda 1 h por compilación y la matriz de pruebas crece con cada paquete | Todo en la PC (2–3 h por versión con arm64 emulado y más infraestructura propia) |
