# Data Model: Separar la herramienta y los paquetes en dos repositorios

Solo lo nuevo o lo que cambia respecto a
[specs/003-ci-pipeline/data-model.md](../003-ci-pipeline/data-model.md).

## 1. Versión fijada (`packages/debfactory.version`)

| Campo | Formato | Regla |
|---|---|---|
| contenido | una línea, `^[0-9a-f]{40}\n?$` | commit de `opentepache/debfactory` |

- Lo leen el lanzador del runner (antes de ejecutar nada) y el binario (autocomprobación en CI).
- Un PR que lo cambia dispara la comparación de todo lo publicado (`ci.code_paths`).
- Un valor con otro formato es error de entrada (código 2), no "versión no instalada".

## 2. Versión instalada (`/srv/debfactory/tools/<commit>/debfactory`)

| Campo | Regla |
|---|---|
| `<commit>` | 40 hex; igual a `debfactory version` del binario |
| dueño y permisos | `root:root`; directorios `755`, binario `755` |
| origen | `just tool-install` desde un árbol limpio con todas las pruebas en verde |
| visibilidad en los jobs | `/opt/debfactory/tools/<commit>/debfactory`, montaje `readonly` en los dos runners |

**Ciclo de vida**: instalada → fijada por un PR abierto → fijada por `main` → sin fijar (ni
`main` ni un PR abierto) → borrada a mano. `ci doctor` informa de las que están sin fijar.

## 3. Entrada de auditoría (cambio)

Igual que en 003 §4, con un campo nuevo justo después de `commit`:

| Campo | Tipo | Regla |
|---|---|---|
| `tool` | string, 40 hex | commit de la herramienta que produjo la entrada. Obligatorio en toda entrada que escribe `ci` desde esta fase; ausente en las anteriores, que no se modifican |

`CheckAudit` añade una regla: si `tool` está, MUST ser un hash válido.

## 4. Configuración (`packages/config/debfactory.yaml`, cambios)

| Clave | Antes | Ahora |
|---|---|---|
| `ci.repository` | `opentepache/debfactory` | `opentepache/packages` |
| `ci.code_paths` | `cmd/`, `internal/`, `config/`, `schema/`, `ci/`, `go.mod`, `go.sum` | `config/`, `debfactory.version` |
| `ci.tool_repository` | — | `opentepache/debfactory` (nuevo; solo para el enlace de diferencias) |

`ci.tool_repository` valida igual que `ci.repository` (`owner/nombre`) y puede estar vacío (sin
enlace).

## 5. Etapas (cambio)

| Etapa | Cambio |
|---|---|
| `code` | desaparece (no hay código Go en `packages`) |
| `tool` | nueva: la herramienta en ejecución no es la que fija `debfactory.version`, o el formato del archivo es inválido |

## 6. Datos de prueba de la herramienta (`debfactory/testdata/project/`)

Copia congelada de `config/`, `packages/`, `repo/state.yaml` y `docs/usuario.md` de `packages`
en el momento de la migración. No se sincroniza después: si un cambio de la herramienta necesita
otro manifiesto de prueba, se añade aquí a propósito.
