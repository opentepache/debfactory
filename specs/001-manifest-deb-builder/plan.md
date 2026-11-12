# Implementation Plan: Constructor de .deb desde manifiestos declarativos

**Branch**: `001-manifest-deb-builder` | **Date**: 2026-11-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-manifest-deb-builder/spec.md`

## Summary

Un binario Go, `debfactory`, convierte un manifiesto YAML por herramienta
(`packages/<name>.yaml`, validado con JSON Schema) en un `.deb` de nivel 1. El proceso es:
descargar el asset oficial (plantillas `{{version}}`/`{{tag}}`/`{{arch}}`), verificarlo contra
un lockfile y contra los checksums upstream, validar la licencia de la versión exacta sobre su
texto ya verificado (antes de ensamblar, como fija el Flujo de la constitución), ensamblar el árbol con permisos y fechas fijados, empaquetar con nfpm como biblioteca y
pasar lintian en contenedor. Una segunda orden instala el `.deb` en contenedores limpios de las
5 suites × 2 arquitecturas (arm64 por qemu/binfmt) y verifica versión y purga limpia. La
investigación de los assets reales ([research.md](research.md)) dio forma al esquema: mapa de
arquitecturas por paquete, rutas internas con plantilla, globs,
completions incluidas o generadas en contenedor, `Depends` calculado desde ELF (bob requiere
glibc 2.39) y overrides de lintian justificados (binarios
estáticos: buf y aqua en ambas arquitecturas, fnm en arm64). pnpm se descartó: su binario da
`embedded-library` (libyaml), que la constitución VII no permite silenciar.

## Technical Context

**Language/Version**: Go 1.26 (`golang-1.26` 1.26.10 de trixie-backports), `GOTOOLCHAIN=local`

**Primary Dependencies**: `github.com/goreleaser/nfpm/v2` v2.48.x (deb),
`github.com/santhosh-tekuri/jsonschema/v6` v6.0.3, `github.com/google/licensecheck`,
`go.yaml.in/yaml/v3` (el fork mantenido; `gopkg.in/yaml.v3` está archivado), biblioteca estándar (`debug/elf`, `archive/zip`,
`archive/tar`, `compress/gzip`, `crypto/sha256`, `net/http`). Fuera de Go: Docker, lintian (en
contenedor) y `qemu-user-binfmt`.

**Storage**: archivos. Manifiestos y lockfiles en `packages/`, configuración en `config/`,
caché direccionada por contenido en `~/.cache/debfactory/sha256/`, salida en `dist/`, trabajo en
tmpfs.

**Testing**: `go test` (unitarias y de integración con `httptest` y fixtures en `testdata/`);
e2e con `just check` (build de todos, reproducibilidad, lintian y pruebas de instalación en
contenedores).

**Target Platform**: PC Debian trixie amd64 (64 GB RAM) como host de build. Paquetes para
`amd64` y `arm64`, probados en bookworm, trixie, sid, noble y resolute.

**Project Type**: CLI de un solo binario más datos declarativos (manifiestos y config).

**Performance Goals**: el build de un paquete con assets en caché tarda menos de 30 s, incluido
lintian; `just check` completo (8 `.deb` × 5 suites) en menos de 30 min con emulación arm64.
Son objetivos orientativos para una sola persona, no un SLA.

**Constraints**: sin código por paquete (I); falla cerrado ante hash, licencia o URL (III, IV);
`.deb` idéntico byte a byte entre builds (VII); ningún binario upstream se ejecuta en el host;
sin red en la orden de prueba.

**Scale/Scope**: 4 paquetes iniciales (fnm, bob, buf, aqua), pensado para decenas; 2 arquitecturas; 5 suites. Fuera
de alcance: publicar, CI, detección de versiones, nivel 2 y firmas upstream.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principio | Cómo lo cumple el diseño | Estado |
|---|---|---|---|
| I | Declarativo primero | Un YAML por paquete; los 4 se expresan con el esquema genérico (validados en [examples/](contracts/examples/)); sin hooks de código. `Depends` desde ELF y completions generadas son reglas genéricas. | ✅ |
| II | Dos niveles | Solo nivel 1 (binario oficial); cada manifiesto declara `level: 1` y el esquema v1 no admite otro valor. Nivel 2 fuera de alcance; el sufijo de suite ya está en la config para cuando llegue. No se publican paquetes fuente. | ✅ |
| III | Cadena de suministro | Lockfile con sha256 de cada artefacto, del texto de licencia y del archivo de checksums upstream cuando existe (buf, aqua), y verificación adicional contra esos checksums. Las URLs se restringen a `github.com/<upstream>/releases/download/<tag>/`. El texto de licencia viene del artefacto o de `raw.githubusercontent.com/<upstream>/<tag>/…` con URL derivada y hash en el lock (metadatos de verificación, permitido por III desde v1.4.0). Sin firma del repositorio en esta fase (no se publica). | ✅ |
| IV | Gate de licencias | Detección local sobre el texto exacto, tras verificar y antes de ensamblar (orden del Flujo desde v1.5.0); allowlist en `config/licenses.yaml`; no identificable, no permitida o distinta de la declarada = fallo. `copyright` DEP-5 con texto completo. | ✅ |
| V | No duplicar | `value` obligatorio en el manifiesto; `community_repos` en la config; los 4 verificados contra Debian, Ubuntu, repos del autor y griffo (research §1). | ✅ |
| VI | Versionado | `<upstream>-0~pav<N>` con `Release="0~pavN"` en nfpm; lintian **no** marca `0~pavN` como revisión cero (verificado); prueba con `dpkg --compare-versions` (FR-024). | ✅ |
| VII | Reproducible y verificable | `SOURCE_DATE_EPOCH` desde el lock; mtime, permisos y orden fijados; `gzip -n`; `--check-reproducible`. lintian `--fail-on error` y pruebas de instalación en cada suite no excluida × amd64 y arm64 (no se excluyen arquitecturas); un fallo hace fallar el paquete. Overrides de lintian declarados con motivo para `statically-linked-binary` y `unstripped-binary-or-object` (permitido por VII desde v1.4.0); `config.lintian.never_override_patterns` bloquea seguridad, licencia y copyright. | ✅ |
| VIII | Automatización | No aplica en la fase 1: todavía no existe el camino automático (detectar → construir → probar → publicar), así que no hay desviación que justificar. El diseño lo prepara: órdenes no interactivas, códigos de salida estables y `--json`. | ✅ (no aplica aún) |
| IX | Simplicidad operativa | Go de Debian backports, lintian, just y qemu-user-binfmt, empaquetados en Debian; Docker vale tanto `docker.io` de Debian como docker-ce de upstream (hoy 29.9 en la PC), porque solo se usan `info`, `build`, `image inspect`, `run`, `exec`, `network disconnect` y `rm`. Un binario y un `justfile`. Cada orden se explica en una línea ([cli.md](contracts/cli.md)). | ✅ |
| X | La IA propone | Ningún componente usa IA. | ✅ |

**Gate (pre-research)**: PASS. **Re-check (post-design)**: PASS contra la constitución v1.5.0;
queda una sola desviación de espíritu, justificada abajo.

## Project Structure

### Documentation (this feature)

```text
specs/001-manifest-deb-builder/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── manifest.v1.schema.json
│   ├── config.md
│   ├── cli.md
│   └── examples/        # fnm, bob, buf, aqua, example (mínimo), fnm.lock
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
cmd/debfactory/          # main: parseo de flags y despacho de órdenes
internal/
├── config/              # carga y validación de config/*.yaml
├── manifest/            # YAML → schema → reglas semánticas → plantillas → structs
├── lock/                # lectura y escritura del lockfile
├── fetch/               # descarga, caché por hash, checksums upstream
├── license/             # licensecheck + SPDX + allowlist
├── assemble/            # extracción, globs, man, completions, ELF → Depends
├── build/               # orquestación: lock, pipeline de build, build --all
├── container/           # envoltorio de docker: build, run, start, exec, disconnect, rm
├── pkg/                 # BuildPlan → nfpm → .deb atómico; copyright y changelog
├── lint/                # lintian en contenedor + overrides
├── installtest/         # pruebas por suite y arch, informe
└── report/              # errores tipados y salida humana o JSON
schema/
└── manifest.v1.schema.json   # embebido con go:embed (copia del contrato)
config/
├── debfactory.yaml
└── licenses.yaml
packages/
├── <name>.yaml
└── <name>.lock.yaml
testdata/
├── manifests/           # válidos e inválidos para validate
├── licenses/            # textos reales de los 4 paquetes (al tag exacto)
├── fixtures/ok/         # tar.gz con binario estático para tests positivos (httptest)
└── negative/            # manifiestos, licencias y locks de casos negativos
justfile
go.mod / go.sum
```

**Structure Decision**: un solo módulo Go con paquetes `internal/` por etapa del pipeline (cada
etapa se prueba por separado y se mapea 1:1 a los `stage` de los errores). Datos (`packages/`,
`config/`) separados del código, que es lo que exige la constitución I.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| **III (espíritu)**: se ejecuta un binario upstream para generar completions | fnm, bob y aqua solo ofrecen completions generadas | Se ejecuta en contenedor efímero, sin red y de solo lectura, nunca en el host; es opcional por manifiesto. |

**Mejora futura** (no es una desviación: III pide checksums **o** firmas): verificar las firmas
upstream (minisign de buf, sigstore de aqua).

## Riesgos que se atacan primero

Del [research §3](research.md#3-riesgos-abiertos-se-verifican-en-las-primeras-tareas):
reproducibilidad byte a byte de nfpm, completions bajo qemu y cobertura de licensecheck con los
4 textos reales. Son tareas de la fase 2 de `tasks.md`, antes de construir el resto del
pipeline. (El riesgo de pnpm desapareció al descartarlo.)
