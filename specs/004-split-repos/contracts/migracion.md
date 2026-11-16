# Contrato: migración a dos repositorios

Cubre FR-016 a FR-018 y research D8. Se ejecuta una vez. Ningún paso cambia el estado publicado
ni la llave (FR-017). La única generación nueva es la del paso 7b: volver a firmar con
`Valid-Until` es una obligación de la fase 003 (FR-030, FR-031) que la fase 002 no cumplía, no
una consecuencia de la migración; los paquetes y el `serial` no cambian.

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
| 3 | `packages` | commit con `debfactory.version` = C, `README.md`, `LICENSE`, workflows sin `go build` y `ci.repository`/`ci.code_paths`/`ci.tool_repository` nuevos | `debfactory validate --root .` pasa |
| 4 | `packages` | `debfactory ci plan --root .` con el estado actual | no hay nada que publicar (SC-002) |
| 5 | GitHub | crear `opentepache/packages` público, subir `main`, ruleset, aprobación de forks, permisos de Actions | `GH_TOKEN=… debfactory ci doctor --root ../packages` en verde (SC-007) |
| 6 | PC | PAT nuevo solo para `packages`; `sudo ci/host/setup.sh --repository opentepache/packages --token-stdin` | `/etc/default/debfactory-runner` con el repositorio nuevo |
| 7 | `debfactory` | commit que borra `packages/`, `config/`, `repo/`, `docs/usuario.md` y `.github/`, y deja `testdata/project/` | las pruebas pasan en un clon limpio (SC-008) |
| 7b | PC | La generación publicada en la fase 002 (`gen-1`) no tiene `Valid-Until`, que exige la fase 003 (FR-030): `debfactory repo rebuild --root ~/Develop/packages` la vuelve a firmar en local con los mismos paquetes y el mismo `serial` (lo mismo que hace `refresh`). Después, `sudo ci/host/migrate-data.sh --root ~/Develop/packages` | `repo verify --root ~/Develop/packages` pasa antes de migrar |
| 8 | PC | arrancar los runners y lanzar primero `refresh` (Actions → main → Run workflow), que sincroniza a R2 la generación con `Valid-Until` | `refresh` en verde; la siguiente ejecución de `main` en `packages` no publica nada (SC-002) |
| 9 | PC | `debfactory repo verify --root ../packages --dest r2` y `apt update` en las 5 suites | mismo serial que antes (SC-001) |

## Si algo falla

Hasta el paso 7 el repositorio actual sigue intacto: basta con borrar `opentepache/packages` y
volver a empezar. Después del paso 7 la historia anterior sigue en `debfactory`, y el contenido
borrado se puede recuperar con `git checkout <commit>^ -- <ruta>`.
