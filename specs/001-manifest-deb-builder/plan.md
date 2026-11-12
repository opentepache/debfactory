# Implementation Plan: Constructor de .deb desde manifiestos declarativos

**Branch**: `001-manifest-deb-builder` | **Date**: 2026-11-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-manifest-deb-builder/spec.md`

## Summary

Un binario Go, `debfactory`, convierte un manifiesto YAML por herramienta
(`packages/<name>.yaml`, validado con JSON Schema) en un `.deb` de nivel 1. El proceso es:
descargar el asset oficial (plantillas `{{version}}`/`{{tag}}`/`{{arch}}`), verificarlo contra
un lockfile y contra los checksums upstream, validar la licencia de la versión exacta sobre su
texto, ensamblar el árbol con permisos y fechas fijados, empaquetar con nfpm como biblioteca y
pasar lintian en contenedor. Una segunda orden instala el `.deb` en contenedores limpios de las
5 suites × 2 arquitecturas (arm64 por qemu/binfmt) y verifica versión y purga limpia. La
investigación de los assets reales ([research.md](research.md)) dio forma al esquema: mapa de
arquitecturas por paquete, rutas internas con plantilla, globs, árboles más enlaces (pnpm),
completions incluidas o generadas en contenedor, `Depends` calculado desde ELF (bob requiere
glibc 2.39) y overrides de lintian justificados (Go estático).

## Technical Context

**Language/Version**: Go 1.26 (`golang-1.26` 1.26.10 de trixie-backports), `GOTOOLCHAIN=local`

**Primary Dependencies**: `github.com/goreleaser/nfpm/v2` v2.48.x (deb),
`github.com/santhosh-tekuri/jsonschema/v6` v6.0.3, `github.com/google/licensecheck`,
`gopkg.in/yaml.v3` (o `go.yaml.in/yaml/v3`), biblioteca estándar (`debug/elf`, `archive/zip`,
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
lintian; `just check` completo (10 `.deb` × 5 suites) en menos de 30 min con emulación arm64.
Son objetivos orientativos para una sola persona, no un SLA.

**Constraints**: sin código por paquete (I); falla cerrado ante hash, licencia o URL (III, IV);
`.deb` idéntico byte a byte entre builds (VII); ningún binario upstream se ejecuta en el host;
sin red en la orden de prueba.

**Scale/Scope**: 5 paquetes iniciales, pensado para decenas; 2 arquitecturas; 5 suites. Fuera
de alcance: publicar, CI, detección de versiones, nivel 2 y firmas upstream.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principio | Cómo lo cumple el diseño | Estado |
|---|---|---|---|
| I | Declarativo primero | Un YAML por paquete; los 5 se expresan con el esquema genérico (validados en [examples/](contracts/examples/)); sin hooks de código. `Depends` desde ELF y completions generadas son reglas genéricas. | ✅ |
| II | Dos niveles | Solo nivel 1 (binario oficial). Nivel 2 fuera de alcance; el sufijo de suite ya está en la config para cuando llegue. No se publican paquetes fuente. | ✅ |
| III | Cadena de suministro | Lockfile con sha256 siempre, más checksums upstream cuando existen (buf, aqua). Las URLs se restringen a `github.com/<upstream>/releases/download/<tag>/`. El texto de licencia viene del artefacto o de `raw.githubusercontent.com/<upstream>/<tag>/…`, con hash en el lock (ver Complexity). Sin firma del repositorio en esta fase (no se publica). | ✅ con justificación |
| IV | Gate de licencias | Detección local sobre el texto exacto; allowlist en `config/licenses.yaml`; no identificable, no permitida o distinta de la declarada = fallo. `copyright` DEP-5 con texto completo. | ✅ |
| V | No duplicar | `value` obligatorio en el manifiesto; `community_repos` en la config; los 5 verificados contra Debian, Ubuntu, repos del autor y griffo (research §1). | ✅ |
| VI | Versionado | `<upstream>-0~pav<N>` con `Release="0~pavN"` en nfpm; lintian **no** marca `0~pavN` como revisión cero (verificado); prueba con `dpkg --compare-versions` (FR-024). | ✅ |
| VII | Reproducible y verificable | `SOURCE_DATE_EPOCH` desde el lock; mtime, permisos y orden fijados; `gzip -n`; `--check-reproducible`. lintian `--fail-on error` y pruebas de instalación en todas las suites × arch no excluidas; un fallo hace fallar el paquete. Hay overrides de lintian declarados (ver Complexity). | ✅ con justificación |
| VIII | Automatización | Fuera de alcance en la fase 1. El diseño la prepara: órdenes no interactivas, códigos de salida estables y `--json`. | ✅ (diferido) |
| IX | Simplicidad operativa | Go de Debian backports, Docker, lintian, just y qemu-user-binfmt, todos empaquetados en Debian. Un binario y un `justfile`. Cada orden se explica en una línea ([cli.md](contracts/cli.md)). | ✅ |
| X | La IA propone | Ningún componente usa IA. | ✅ |

**Gate (pre-research)**: PASS. **Re-check (post-design)**: PASS; las tres desviaciones están
justificadas abajo.

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
│   └── examples/        # fnm, pnpm, bob, buf, aqua, minimal-static, fnm.lock
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
├── assemble/            # extracción, globs, árboles, enlaces, man, completions, ELF → Depends
├── container/           # envoltorio mínimo de `docker run` (completions, lintian, tests)
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
├── licenses/            # textos reales y de casos negativos
└── negative/            # fixtures servidos por httptest
justfile
go.mod / go.sum
```

**Structure Decision**: un solo módulo Go con paquetes `internal/` por etapa del pipeline (cada
etapa se prueba por separado y se mapea 1:1 a los `stage` de los errores). Datos (`packages/`,
`config/`) separados del código, que es lo que exige la constitución I.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| **III**: el texto de licencia se descarga de `raw.githubusercontent.com`, no de una URL de release | pnpm, fnm y bob no incluyen la licencia en el artefacto, y IV exige la licencia de la versión exacta | Usar el texto de la API de GitHub depende de un servicio que falla (aqua 404) y no fija la versión. Mitigación: la URL se deriva de `upstream` + `tag` (no es libre), y su sha256 queda en el lockfile. |
| **VII**: overrides de lintian (`statically-linked-binary` en buf y aqua) | Es un error inevitable para binarios Go estáticos de nivel 1 | Recompilar (nivel 2) contradice II, que lo reserva para cuando no hay binario utilizable. Mitigación: cada override se declara con motivo en el manifiesto y `config.lintian.never_override` impide silenciar etiquetas de seguridad o licencia. |
| **III (espíritu)**: se ejecuta un binario upstream para generar completions | fnm, pnpm, bob y aqua solo ofrecen completions generadas | Se ejecuta en contenedor efímero, sin red y de solo lectura, nunca en el host; es opcional por manifiesto. |
| **III**: firmas upstream (minisign, sigstore) no verificadas | La constitución exige checksums *o* firmas; verificar firmas añade dos herramientas | Se pospone; el lockfile más los checksums upstream cubren la integridad. Queda anotado como mejora. |

## Riesgos que se atacan primero

Del [research §3](research.md#3-riesgos-abiertos-se-verifican-en-las-primeras-tareas):
reproducibilidad byte a byte de nfpm, pnpm SEA a través del enlace simbólico, completions bajo
qemu y cobertura de licensecheck con los 5 textos reales. Deben ser las primeras tareas de
`tasks.md`, antes de construir el resto del pipeline.
