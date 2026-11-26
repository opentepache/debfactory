# Implementation Plan: Sitio web del repositorio

**Branch**: `006-repo-website` | **Date**: 2026-11-26 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/006-repo-website/spec.md`

## Summary

La herramienta gana un grupo de órdenes, `debfactory site`. `site data` reúne el estado
publicado, los manifiestos y la configuración en un `site.json` y lo verifica contra los índices
que generó aptly. `site build` lo renderiza con `html/template` en un árbol estático, en inglés en
la raíz y en español en `/es/`. Al final de cada `ci main`, `ci retire` y `ci refresh`, un paso
`site` genera el sitio desde la generación vigente y lo sube con rclone al mismo bucket que el
repositorio, junto a `dists/` y `pool/`, que no se mueven. Si el paso falla, se notifica y el
sitio anterior queda en línea; los paquetes ya están publicados.

| Pieza | Dónde | Qué hace |
|---|---|---|
| `site data` / `site build` / `site serve` / `site sync` | herramienta (`internal/site`, `cmd/debfactory`) | modelo → páginas; revisión en local; subida manual a un destino, p. ej. el de pruebas (research D1, D2, D5) |
| paso `site` | `internal/ci` | genera y sube el sitio al final de cada publicación, retiro o renovación (D5) |
| subida del sitio | `internal/sync` | `assets/` → páginas → borrado diferido, con registro por destino (D5, D6) |
| regla de reescritura | zona de Cloudflare (configuración, una vez) | `…/` → `…/index.html` (D4) |
| bloques `site` e `i18n.es` | configuración y manifiesto | activar el sitio, nombre, traducción de la descripción (D7, D13) |

**Cambios respecto al stack propuesto** (research D3): el repositorio **no** se mueve a `/apt`,
porque rompería el `.sources` de todo usuario existente. Los idiomas quedan como decidió el
clarify: inglés en la raíz y español en `/es/`, en lugar de `/en/` y `/es/`. Astro se descarta a
favor de `html/template` (D1).

## Technical Context

**Language/Version**: Go 1.26 (`GOTOOLCHAIN=local`), como 001–005.

**Primary Dependencies**:
- `html/template` y `embed` de la biblioteca estándar.
- `golang.org/x/net/html` para las pruebas de estructura (el módulo `golang.org/x/net` v0.60.0 ya
  está en `go.mod` como indirecto).
- rclone y aptly, ya en uso.
- Solo para pruebas, en contenedor con digest fijado: Caddy, que ya se usa, y `pa11y-ci`/axe-core
  para la auditoría de accesibilidad (D10).

**Storage**: archivos. `site.json` y el árbol en `<data>/site/<digest>/`; registro de subida en
`<data>/sync/<dest>.site.json`. La fuente es el estado de referencia de la ejecución (el que
describe la generación vigente, que puede venir de un PR de estado sin integrar), sin cambios de
formato.

**Testing**:
- `go test` sin etiquetas: modelo, render, escape, determinismo, estructura, presupuesto y rutas.
- `-tags e2e` (`just e2e`): comandos de la portada en las 5 suites × 2 arquitecturas y auditoría
  de accesibilidad.
- Pruebas de integración de `internal/ci` con `citest`: el paso `site` y sus fallos.

**Target Platform**: la PC Debian de siempre. El paso `site` corre en el runner `pub` dentro del
job de publicación. El sitio lo consume cualquier navegador, sin JavaScript.

**Project Type**: CLI (subcomandos de la herramienta existente) más sitio estático generado.

**Performance Goals**: versión nueva visible en menos de 10 min (SC-001); el diseño da 1–2 min.
La generación (segundos para decenas de paquetes) no es un objetivo aparte.

**Constraints**:
- Cada página pesa menos de 100 KB con su CSS.
- Cero recursos de terceros y cero JavaScript.
- WCAG 2.1 AA y 320 px de ancho.
- Salida determinista byte a byte.
- Ninguna ruta del sitio pisa `dists/`, `pool/` ni la raíz del repositorio.

**Scale/Scope**: 5 paquetes hoy y decenas a futuro; 2 idiomas; unas 2 + 2·N páginas.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principio | Cómo se cumple | Estado |
|---|---|---|
| I. Declarativo primero | Todo el contenido variable sale de manifiestos, configuración y estado; agregar un paquete es solo su manifiesto (FR-007, FR-008) | ✅ |
| II. Dos niveles | No aplica: el sitio no construye paquetes | ✅ |
| III. Cadena de suministro | Sin dependencias nuevas en el binario. Los textos de upstream se escapan siempre (`html/template`). Las herramientas de prueba van en contenedor con digest fijado. El paso `site` no lee la llave | ✅ |
| IV. Licencias | El sitio muestra la licencia del manifiesto, la que ya validó el gate; no la decide | ✅ |
| V. No duplicar | No aplica | ✅ |
| VI. Versionado Debian | Las versiones se muestran y ordenan con la comparación de Debian | ✅ |
| VII. Reproducible y verificable | Salida determinista (D12). Los comandos de la portada se prueban en las 10 combinaciones (D8). El sitio se verifica contra los índices de aptly (D2) | ✅ |
| VIII. Sin humanos en el camino feliz | El sitio se regenera y sube solo en cada publicación. Un fallo se notifica con etapa y motivo y se repara en el siguiente run (D5) | ✅ |
| IX. Simplicidad operativa | Mismo binario, mismo bucket, mismo job y solo operaciones S3 estándar. La regla de reescritura es configuración del proveedor, no código; otros hostings estáticos resuelven el índice solos (D4). Se explica en un párrafo y se opera con `just site` | ✅ |
| X. La IA propone | No aplica: no interviene ninguna IA | ✅ |
| Alcance: `Origin`/`Label` OpenTepache | El nombre del sitio sale de `site.name` y coincide con el del repositorio | ✅ |

Sin violaciones. **Re-check tras la fase 1**: el diseño (data-model, contratos) no agrega
servicios, dependencias ni secretos. ✅

## Project Structure

### Documentation (this feature)

```text
specs/006-repo-website/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md       # debfactory site, paso site, just, ci doctor
│   ├── pages.md     # contenido y estructura de cada página
│   └── config.md    # bloque site (config) e i18n.es (manifiesto)
├── checklists/requirements.md
└── tasks.md         # /speckit-tasks
```

### Source Code (repository root)

```text
cmd/debfactory/
└── site.go                     # órdenes site data | build | serve | sync

internal/site/
├── model.go                    # Model (site.json), Load desde config + manifiestos + estado
├── verify.go                   # coherencia estado ↔ índices Packages de la generación
├── commands.go                 # scripts de agregar/verificar/quitar desde la config
├── render.go                   # html/template, rutas, CSS con hash, árbol temporal → rename
├── i18n.go                     # carga y validación de traducciones
├── templates/                  # base, portada y paquete (embed)
├── assets/site.css             # (embed)
├── i18n/en.yaml, i18n/es.yaml  # (embed)
└── *_test.go                   # modelo, escape, determinismo, estructura, presupuesto, rutas

internal/sync/
├── site.go                     # subida del sitio: assets → páginas → borrado diferido; registro
└── site_test.go

internal/ci/
├── site.go                     # paso site al final de main, retire y refresh; notificación
└── site_test.go

internal/config/, internal/manifest/   # bloques site e i18n.es; nombres reservados; esquema
internal/report/                        # etapa site
internal/ci/doctor.go                   # comprobación GET <public_url>/
internal/repotest/
├── site_test.go                # e2e: comandos de la portada en 5 suites × 2 arquitecturas
└── a11y_test.go                # e2e: pa11y-ci/axe en contenedor, 320 px y 1280 px

justfile                         # site, site-serve
docs/operacion-ci.md             # regla de reescritura, activar el sitio, fallos del paso site
```

En `opentepache/packages`, en el PR de activación: el bloque `site` en `config/debfactory.yaml`,
la herramienta nueva en `debfactory.version` y, opcionalmente, `i18n.es` en los manifiestos.
`docs/usuario.md` pasa a enlazar la portada como referencia.

**Structure Decision**: un paquete nuevo, `internal/site`, para el modelo y el render, sin
dependencias de `ci`. La subida va en `internal/sync`, junto a la del repositorio, porque comparte
rclone, los destinos y el borrado diferido. La orquestación y la notificación van en
`internal/ci`.

## Riesgos

1. **La regla de reescritura de Cloudflare.** Sin ella, `https://apt.opentepache.org/` sigue en
   404 aunque el sitio esté subido. Mitigación: `ci doctor` la comprueba, y quickstart §3 la
   configura antes de activar `site.enabled`.
2. **Estado e índices desalineados** (p. ej. un estado importado a mano): `site data` falla en vez
   de publicar algo falso. El fallo se notifica y no afecta a los paquetes.
3. **Auditoría a11y en contenedor**: es una imagen de terceros solo para pruebas, con digest
   fijado y sin acceso al host. Si deja de mantenerse, las comprobaciones estructurales en Go
   siguen cubriendo lo básico.

## Complexity Tracking

Sin violaciones de un MUST. Una excepción justificada a una preferencia:

| Excepción | Por qué hace falta | Alternativa más simple descartada porque |
|-----------|------------|-------------------------------------|
| `pa11y-ci`/axe-core (no empaquetado en Debian) para la auditoría WCAG de SC-006, contra IX "preferir herramientas empaquetadas en Debian" | No hay un auditor WCAG empaquetado en Debian. Corre solo en pruebas (`just e2e`), en contenedor fijado por digest, sin credenciales y fuera de la operación | Las comprobaciones estructurales en Go (T033) no cubren contraste calculado sobre el render, roles ARIA ni el desplazamiento a 320 px; quedan como respaldo si la imagen deja de mantenerse |
