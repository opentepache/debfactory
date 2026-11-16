# Contrato: cambios sobre los contratos de la fase 003

Lo que cambia en [specs/003-ci-pipeline/contracts](../../003-ci-pipeline/contracts). Al
implementar, se editan esos archivos para que reflejen la estructura nueva, con una nota que
remite aquí.

## `cli.md`

| Orden | Cambio |
|---|---|
| `debfactory version` | nueva (contracts/herramienta.md) |
| `ci pr`, `ci main` | sin la etapa `code`; `ci.code_paths` es `[config/, debfactory.version]`; autocomprobación de la herramienta en CI (etapa `tool`); `ci pr` añade el enlace de diferencias si cambia `debfactory.version` |
| `ci main` paso 3b | se dispara con `ci.code_paths` nuevos; solo compara, ya no ejecuta comprobaciones de código |
| `ci doctor` | la versión del runner viene embebida; nuevas comprobaciones del host sobre `/srv/debfactory/tools` (contracts/herramienta.md) |
| recetas `just` | todas aceptan `root` (por defecto `../packages`); nuevas `tool-install`; `check` y `e2e` usan `testdata/project` |

## `workflows.md`

- Los dos workflows viven en `opentepache/packages`.
- Desaparece el paso "compilar debfactory"; los pasos llaman a `debfactory` (el lanzador).
- `paths-ignore` deja de listar `specs/**` (no existe en `packages`).

## `runner-host.md`

- `/srv/debfactory/tools`: de root, `755`; nuevo montaje `readonly` en los dos runners.
- `/etc/default/debfactory-runner`: `DEBFACTORY_REPOSITORY=opentepache/packages`.
- Imagen: sin Go; con el lanzador `/usr/local/bin/debfactory`.
- El PAT solo tiene acceso a `opentepache/packages`.

## `notificaciones.md`

- Los issues y el PR de estado viven en `opentepache/packages`, que es público. Su contenido ya
  era solo paquete, versión, etapa, motivo y enlace al log; no cambia.
- Etapa nueva `tool` en la lista de etapas.

## Auditoría (`data-model.md` §4 de 003)

Campo `tool` (data-model.md §3 de esta fase).
