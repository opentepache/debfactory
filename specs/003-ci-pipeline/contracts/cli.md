# Contrato: órdenes `debfactory ci`

> **Fase 004** ([cambios-003.md](../../004-split-repos/contracts/cambios-003.md)): las órdenes `ci` comprueban en CI que el binario es la versión que fija `debfactory.version` (etapa `tool`); `debfactory version` imprime el commit; `ci doctor` lleva embebida la versión del runner y comprueba `/srv/debfactory/tools`.

Amplía la CLI de las fases 001 y 002 ([002 cli.md](../../002-signed-apt-repo/contracts/cli.md)).
Las opciones globales (`--data`, `--cache`, `--root`, `--json`), el formato de errores y los
códigos de salida son los mismos. Las opciones globales van **después** de la orden.

Los workflows ([workflows.md](workflows.md)) solo llaman a estas órdenes. La lógica está en Go,
no en YAML (research D16).

## Entorno

`ci` lee el contexto del job desde variables de GitHub:

- `GITHUB_EVENT_NAME`, `GITHUB_REF`, `GITHUB_SHA`, `GITHUB_REPOSITORY`, `GITHUB_RUN_ID` y
  `GITHUB_SERVER_URL`;
- `GITHUB_TOKEN`, solo para la API y el push de la rama de estado;
- `GITHUB_STEP_SUMMARY`;
- `RUNNER_LABELS`, que define la imagen del runner (`ci` o `pub`).

Fuera de GitHub (para pruebas locales) todas se pueden pasar con `--env-file`. `GITHUB_TOKEN`
nunca aparece en la salida, ni siquiera en los errores HTTP.

Si una orden que escribe se ejecuta con `RUNNER_LABELS` distinto de `pub`, o fuera de `main`,
sale con código 3 antes de tocar nada. Es una segunda barrera después del hook (research D3).

## Órdenes

| Orden | Nivel | Efecto | Escribe |
|---|---|---|---|
| `ci plan [--mode main\|pr\|rebuild] [--base REF] [--package P --version V]` | ambos | Calcula el [Plan](../data-model.md#2-plan-plan) y lo imprime (`--json`: un objeto). No construye nada. | nada |
| `ci pr --base REF [--jobs N]` | `ci` | Si cambió `ci.code_paths` (desde la fase 004, `config/` y `debfactory.version`), valida todos los manifiestos y añade al resumen el enlace a las diferencias de la herramienta. Luego aplica los gates 1–6 a `plan.test[]` y reconstruye y compara `plan.compare[]` (FR-010). Escribe el resumen por paquete, suite y arquitectura en `GITHUB_STEP_SUMMARY`. | `dist/` del espacio de trabajo |
| `ci main [--jobs N]` | `pub` | Ejecución de `main`, en este orden: (1) comprueba si está superada (D6); (2) concilia el PR de estado (D8) y completa lo pendiente de una ejecución interrumpida: si el destino tiene un `serial` sincronizado menor que el local, `sync` + `verify`; si la rama va por detrás, push (FR-029); (3) plan; (3b) si `<last_main_sha>..HEAD` toca `ci.code_paths`, validación de los manifiestos y comparación de FR-010 (etapa `compare`; la etapa `code` desapareció en la fase 004): si fallan, no publica (FR-005); (4) gates 1–6 por paquete, en paralelo; (5) `publish` de los que pasaron, en una generación, y justo después del swap escribe la auditoría y la copia en `<data>/ci/`; (6) `repo sync` al destino `ci.destination` y `repo verify --dest`; (7) push del PR de estado; (8) notificaciones. | `<data>`, destino público, rama de estado, issues |
| `ci rebuild --package P --version V [--jobs N]` | `pub` | Igual que `ci main`, pero solo para `P V`, con el manifiesto y el lockfile del commit que la declaró (`restore.FindCommit`). Si `V` ya está publicada, compara los sha256 y no publica (FR-026). | ídem |
| `ci retire --package P --version V --reason TEXTO [--last]` | `pub` | Concilia el estado y ejecuta `retire`, `sync` y `verify`. Después añade la auditoría y actualiza el PR de estado (FR-027). | ídem |
| `ci refresh` | `pub` | Concilia el estado. Después ejecuta `repo rebuild` (vuelve a firmar con `Valid-Until` nuevo), `sync` y `verify`. No toca `state.yaml` ni la auditoría (FR-031). Comprueba también la antigüedad del PR de estado (FR-007c). | `<data>`, destino |
| `ci restore-state-pr` | `pub` | Vuelve a crear la rama y el PR de estado desde `<data>/ci/` (D8). Antes comprueba que `<data>/ci/state.yaml` corresponde a la generación `current`. | rama de estado |
| `ci notify --package P --version V (--stage S \| --stage-file FILE) --reason TEXTO` | `pub` | Abre o comenta el issue de fallo. La usan los pasos `if: failure()` cuando la ejecución muere antes de su propio informe. Con `--stage-file` lee la etapa en curso que `ci` escribe en `$RUNNER_TEMP/debfactory-stage`; si no existe, la etapa es `pipeline`. | issues |
| `ci audit check [--against REF]` | ambos | Comprueba los invariantes de la auditoría: solo añadir respecto de `REF` y cobertura de `state.yaml` (FR-024, FR-025). | nada |
| `ci doctor` | ninguno (lo ejecuta el mantenedor con un token suyo) | Comprueba con la API la configuración de GitHub (research §2; en un repositorio privado, workflows de forks desactivados en lugar de la aprobación de forks, D1) y en local la del host ([runner-host.md](runner-host.md)). Lista lo que falta. | nada |

Con `GITHUB_EVENT_NAME=workflow_dispatch`, `ci main` lee `INPUT_ACTION` (y `INPUT_PACKAGE`,
`INPUT_VERSION`, `INPUT_REASON`, `INPUT_LAST`), los valida y despacha a `rebuild`, `retire`,
`refresh` o `restore-state-pr`; así `main.yml` siempre ejecuta `bin/debfactory ci main`.

Cambios en órdenes de la fase 002:

- `repo verify` exige `Valid-Until` en cada `Release`.
  - Falla (código 1) si la generación ya caducó.
  - Avisa si quedan menos de `repo.validity.warn_days`.
  - Con `--json`, el aviso es el evento `{"event":"expiry","valid_until":…,"days":…}`, que `ci`
    convierte en un issue de etapa `expiry` (FR-032).
- La generación firma con debfactory (`-skip-signing` en aptly, research D11). Antes de firmar,
  añade `Valid-Until: <fecha de firma + repo.validity.days>` a cada `Release`.

## Salida

```text
$ debfactory ci main
ci: ejecución 1234 (push a3f9e1c) en debfactory-main
estado: referencia main (serial 1, sha256 5be1…); sin PR de estado
plan: publicar fnm 1.40.0-0~tepache1 (new-version); sin cambios: aqua bob buf
fnm 1.40.0-0~tepache1: build ok · lint ok · test 10/10 ok
repo: serial 1 → 2; firmado con 84983CE038A7C736…; válido hasta 2026-12-13
sync r2: 6 archivos nuevos, 25 índices; verify --dest r2 ok
auditoría: +1 entrada (seq 9)
estado: PR #14 abierto (debfactory/state)
issues: cerrado #11 (fnm 1.40.0-0~tepache1 test)
```

Con `--json` se emiten los eventos de las fases 001 y 002 más `plan`, `state`, `audit`, `pr`,
`issue` y `superseded`.

## Códigos de salida

| Código | Significado |
|---|---|
| `0` | Éxito. También cuando la ejecución está superada o no había nada que publicar. |
| `1` | Falló al menos un paquete o una etapa. Los demás paquetes se publicaron si pasaron (FR-004). |
| `2` | Uso incorrecto. |
| `3` | Entorno incompleto o nivel de confianza equivocado. |

## `justfile`

| Receta | Ejecuta |
|---|---|
| `just runner-image` | construye `debfactory-runner:<versión>` para los dos niveles con `ci/runner/Dockerfile` |
| `just ci-doctor` | `debfactory ci doctor --root {{root}}` |
| `just tool-install` | instala la versión de la herramienta para los runners (fase 004, [herramienta.md](../../004-split-repos/contracts/herramienta.md)) |
| `just ci-local-pr` | `debfactory ci pr --base origin/main` en local, para probar antes de abrir un PR |
