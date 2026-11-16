# Contrato: migración a dos repositorios

Cubre FR-016 a FR-018 y research D8. Se ejecuta una vez. Ningún paso cambia el estado publicado,
la llave ni lo que sirve R2 (FR-017).

## Precondiciones

- `main` del repositorio actual con la fase 004 implementada y todas las pruebas en verde.
- Ningún PR de estado abierto. Si lo hay, se integra antes.
- `git-filter-repo` instalado (`sudo apt install git-filter-repo`).
- La licencia de `packages`: MIT (research D8).

## Pasos

| # | Dónde | Qué | Comprobación |
|---|---|---|---|
| 1 | `debfactory` | `just tool-install` desde `main` | imprime el commit C |
| 2 | PC | clon nuevo del repositorio actual en `~/Develop/packages` y `git filter-repo --path packages/ --path config/ --path repo/ --path docs/usuario.md --path .github/` | `git log --stat` solo muestra esas rutas; la autoría no cambia |
| 3 | `packages` | commit con `debfactory.version` = C, `README.md`, `LICENSE`, workflows sin `go build` y `ci.repository`/`ci.code_paths`/`ci.tool_repository` nuevos | `debfactory --root . validate` pasa |
| 4 | `packages` | `debfactory --root . ci plan` con el estado actual | no hay nada que publicar (SC-002) |
| 5 | GitHub | crear `opentepache/packages` público, subir `main`, ruleset, aprobación de forks, permisos de Actions | `GH_TOKEN=… debfactory --root ../packages ci doctor` en verde (SC-007) |
| 6 | PC | PAT nuevo solo para `packages`; `sudo ci/host/setup.sh --repository opentepache/packages --token-stdin` | `/etc/default/debfactory-runner` con el repositorio nuevo |
| 7 | `debfactory` | commit que borra `packages/`, `config/`, `repo/`, `docs/usuario.md` y `.github/`, y deja `testdata/project/` | las pruebas pasan en un clon limpio (SC-008) |
| 8 | PC | arrancar los runners (o completar las tareas pendientes de 003 que lo hacen) | la primera ejecución de `main` en `packages` no publica nada (SC-002) |
| 9 | PC | `debfactory --root ../packages repo verify --dest r2` y `apt update` en las 5 suites | mismo serial que antes (SC-001) |

## Si algo falla

Hasta el paso 7 el repositorio actual sigue intacto: basta con borrar `opentepache/packages` y
volver a empezar. Después del paso 7 la historia anterior sigue en `debfactory`, y el contenido
borrado se puede recuperar con `git checkout <commit>^ -- <ruta>`.
