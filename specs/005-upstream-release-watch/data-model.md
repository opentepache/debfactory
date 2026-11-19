# Data Model: Detección de versiones upstream

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md)

El detector no tiene almacenamiento propio (research D7). Sus datos son campos nuevos en
archivos que ya existen y objetos de GitHub que ya usa el pipeline.

## 1. Manifiesto: bloque `watch` (nuevo, opcional)

`packages/<pkg>.yaml`, esquema `schema/manifest.v1.schema.json` (campo opcional, compatible).

| Campo | Tipo | Defecto | Regla |
|---|---|---|---|
| `watch.enabled` | bool | `true` | `false` excluye el paquete del detector (se lista como omitido en el resumen) |
| `watch.auto_merge` | bool | `false` | FR-013; nunca cubre cambios mayores, de licencia ni de otros campos (FR-015) |
| `watch.prerelease_flag` | `trust` \| `ignore` | `trust` | FR-003a; `ignore` solo anula la marca `prerelease` de upstream, nunca el filtro de marcadores en la versión |

La plantilla `tag` (existente, `v{{version}}` por defecto) es el patrón de etiquetas estables
(research D5).

## 2. Configuración: bloque `watch` (nuevo)

`config/debfactory.yaml` en `packages`.

| Campo | Tipo | Defecto | Regla |
|---|---|---|---|
| `watch.interval` | duración | `24h` | FR-001; mínimo `1h` |
| `watch.cooldown` | duración | `48h` | FR-004a; mínimo `0h`, global |
| `watch.rate_reserve` | entero | `500` | FR-021; peticiones que el ciclo deja sin usar |
| `watch.branch_prefix` | texto | `debfactory/update/` | ramas del detector; termina en `/`; no puede chocar con `main` ni con `ci.state_branch` |

El slug de la App no se configura: lo entrega `actions/create-github-app-token` (salida
`app-slug`) en `DEBFACTORY_APP_SLUG` (contracts/cli.md).

## 3. Release upstream (leído, no guardado)

De `GET /repos/{upstream}/releases`: `tag_name`, `draft`, `prerelease`, `published_at`,
`html_url`, `assets[].name`. Derivados: `version` (plantilla `tag` invertida), `estable`
(research D5), `elegible` (`published_at + cooldown ≤ ahora`).

## 4. Candidata

La versión estable y elegible más alta mayor que `manifest.version` en `main` (dpkg).

| Atributo | Origen |
|---|---|
| `paquete`, `anterior`, `nueva` | manifiesto y release |
| `mayor` | FR-015a: cambia el 1.er componente numérico; si es `0`, también el 2.º; esquema no numérico → `true` |
| `licencia` | `igual` \| `cambiada` (a otra de la allowlist) \| `bloqueada` (research D8) |
| `lock` | entrada de lockfile generada (hashes de artefactos, licencia, `source_date_epoch`) |
| `auto_merge` | `manifest.watch.auto_merge ∧ ¬mayor ∧ licencia = igual` |

## 5. PR de actualización

Identidad: rama `<branch_prefix><pkg>`; como máximo uno abierto por paquete (FR-008).

Cuerpo: marcador `<!-- debfactory-watch pkg=<pkg> version=<nueva> -->` + tabla (anterior,
nueva, notas del release, licencia, mayor, auto-merge).

```text
                       candidata nueva / más nueva
   (sin PR) ──────────────────────────────────────────▶ abierto
      ▲                                                │  │  │
      │ merge → main publica                           │  │  └─ commits ajenos ─▶ comentado (no se toca)
      │                                                │  └──── candidata retirada sin sustituta ─▶ cerrado
      └──────────────── integrado ◀── verify ✅ + (auto-merge o mantenedor)
```

## 6. Notificación del detector

Issue del notificador de 003 (etiqueta de fallos, marcador `paquete/versión/etapa`).

| Etapa | Cuándo (FR-016) | Datos (FR-017) |
|---|---|---|
| `watch-license` | licencia nueva fuera de la allowlist o no identificable | declarada, detectada, cobertura |
| `watch-assets` | la plantilla del manifiesto no encuentra los assets tras la espera | esperados y encontrados |
| `watch-checksums` | los artefactos no coinciden con los checksums de upstream | archivo, esperado, obtenido |
| `watch-tampered` | hashes distintos para la misma etiqueta que el PR abierto | hashes antes y después |
| `watch-ratelimit` | 3 ciclos seguidos detenidos por límite | paquetes pendientes |

Ciclo de vida: abierta → (mismo motivo en ciclos siguientes: sin cambios, sin reintento) →
cerrada sola al abrirse un PR válido o dejar de ser candidata la versión (FR-019). Un cambio del
manifiesto del paquete en `main` reabre la evaluación (FR-018).

## 7. Ciclo

Una ejecución de `watch.yml`. Su resumen (`$GITHUB_STEP_SUMMARY` y salida) tiene una fila por
paquete:

| Campo | Valores |
|---|---|
| decisión | `sin-cambios`, `en-espera (elegible <fecha>)`, `pr-abierto #n [(auto-merge)]`, `pr-actualizado #n`, `pr-al-día #n`, `pr-cerrado #n`, `comentado #n`, `notificado <etapa> #n`, `ya-notificado #n (<etapa>)`, `omitido (watch.enabled: false)`, `pendiente-límite`, `error …` (fallo del ciclo, código 1) |
| publicada / upstream | versión en `main` y última estable de upstream |
Más una línea final con las consultas de releases (y cuántas fueron `304`) y
`X-RateLimit-Remaining`.

## 8. Estado entre ciclos (caché de Actions)

`$RUNNER_TEMP/debfactory-watch/state.json`, que restaura y guarda `actions/cache` en `watch.yml`:

| Campo | Uso |
|---|---|
| `last_cycle` | inicio del último ciclo completo (FR-001) |
| `pending` | paquetes que el límite dejó sin revisar; van primero en el ciclo siguiente |
| `stopped` | ciclos completos seguidos detenidos por el límite (3 → `watch-ratelimit`) |
| `releases` | por upstream, el ETag y el cuerpo de la última respuesta (FR-021) |

Perderlo no rompe nada: el ciclo siguiente es completo y sin ETag.
