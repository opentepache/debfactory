# Contrato: workflows de GitHub Actions

> **Fase 004** ([cambios-003.md](../../004-split-repos/contracts/cambios-003.md)): los dos workflows viven en `opentepache/packages`, no compilan nada y llaman a `debfactory`, el lanzador de la imagen que ejecuta la versión fijada en `debfactory.version`.

Hay dos archivos en `.github/workflows/`. Ninguno usa actions de terceros salvo
`actions/checkout`, fijada por sha de commit (research D16). Ninguno lee secretos de GitHub:
`secrets.*` no aparece en ningún workflow.

## `pr.yml`: pull requests (nivel `ci`)

```yaml
name: pr
on:
  pull_request:
    paths-ignore: ['repo/state.yaml', 'repo/audit.jsonl']   # solo el estado: verify es obligatorio
permissions:
  contents: read
concurrency:
  group: pr-${{ github.event.pull_request.number }}
  cancel-in-progress: true          # un push nuevo al PR reemplaza la ejecución anterior
jobs:
  verify:
    runs-on: [self-hosted, debfactory-pr]
    timeout-minutes: 90
    steps:
      - uses: actions/checkout@<sha>     # fetch-depth: 0 (diff contra la base); persist-credentials: false
      - env: {BASE_SHA: ${{ github.event.pull_request.base.sha }}}
        run: debfactory ci pr --base "$BASE_SHA"   # el lanzador (fase 004)
```

La base es el commit fijo del evento, no `origin/<rama>`: si `main` avanza
antes de que corra el check (p. ej. el PR se integra con el check en cola), el
diff contra `origin/main` sale vacío y el check pasaría sin probar nada
(hallazgo de T026, fase 004).

`paths-ignore` solo lista el estado del repositorio: el ruleset exige `verify`
y un workflow que no corre deja el check pendiente para siempre. Un PR de solo
documentación sí corre y `ci pr` decide en segundos que no hay nada que hacer
(`plan: nada que hacer`). El PR de estado solo cambia esos dos archivos: sin el
`paths-ignore`, GitHub crea una ejecución a nombre de `github-actions[bot]` que
espera aprobación sin jobs y termina en `failure` al integrar el PR (visto en el
PR de estado de k9s, fase 004). Un PR humano que solo toque el estado se queda
sin `verify` y no se puede integrar, que es lo deseado.

| Requisito | Cómo |
|---|---|
| FR-008 | `ci pr` escribe el resumen; el check del PR refleja el código de salida |
| FR-009 y FR-017 | `permissions: contents: read`; el runner `ci` no tiene ningún secreto montado |
| FR-011 | `ci pr` no construye si el diff no toca paquetes ni rutas de código (`plan: nada que hacer`) |
| FR-013 | ajuste "Require approval for all external contributors" (research D12) |
| PR de estado | `paths-ignore` del estado: `pr.yml` no corre; `ci main` publica `verify=success` sobre la punta de la rama de estado al abrirlo o actualizarlo, y lo repone al conciliar (research D8) |

## `main.yml`: rama principal, ejecuciones manuales y refirmado (nivel `pub`)

```yaml
name: main
on:
  push:
    branches: [main]
    paths-ignore: ['docs/**', 'specs/**', '**.md']
  workflow_dispatch:
    inputs:
      action:  {type: choice, options: [rebuild, retire, refresh, restore-state-pr], required: true}
      package: {type: string, required: false}
      version: {type: string, required: false}
      reason:  {type: string, required: false}
      last:    {type: boolean, default: false}     # confirmar el retiro de la única versión
permissions:
  contents: write          # solo la rama de estado; el ruleset de main lo bloquea allí
  pull-requests: write
  issues: write
  statuses: write          # verify sobre la punta de la rama de estado (ruleset de main)
concurrency:
  group: publish
  cancel-in-progress: false
  queue: max               # hasta 100 en cola, sin cancelar ninguna (research D7)
jobs:
  run:
    runs-on: [self-hosted, debfactory-main]
    timeout-minutes: 120
    steps:
      - uses: actions/checkout@<sha>     # ref: main, fetch-depth: 0
      - env:
          INPUT_ACTION: ${{ inputs.action }}
          INPUT_PACKAGE: ${{ inputs.package }}
          INPUT_VERSION: ${{ inputs.version }}
          INPUT_REASON: ${{ inputs.reason }}
          INPUT_LAST: ${{ inputs.last }}
        run: bin/debfactory ci main   # con workflow_dispatch, ci main lee INPUT_ACTION y despacha
      - if: failure()
        run: bin/debfactory ci notify --package pipeline --version - --stage-file "$RUNNER_TEMP/debfactory-stage" --reason "la ejecución terminó antes de su informe"
```

Reglas:

- **Inputs del dispatch**: llegan a `ci` como variables de entorno (`INPUT_PACKAGE`, etc.), y
  `ci` los valida con las mismas reglas que la CLI (nombre de paquete, versión Debian). Nunca se
  interpolan en un `run:` como `${{ inputs.x }}`, para evitar inyección de shell.
- **Ruta del workflow**: el hook del runner `pub` solo acepta `GITHUB_WORKFLOW_REF` igual a
  `<repo>/.github/workflows/main.yml@refs/heads/main`. Renombrar este archivo exige actualizar
  la imagen del runner.
- **Merge del PR de estado**: dispara `push` en `main`. `ci main` concilia, no encuentra nada
  que publicar y termina en verde (FR-007).
- **Refirmado semanal**: no hay `on: schedule`, porque se desactiva tras 60 días sin actividad.
  Lo lanza `debfactory-refresh.timer` desde la PC con `workflow_dispatch` y `action=refresh`
  (research D11).

| Requisito | Cómo |
|---|---|
| FR-001 | `on.push.branches: [main]` |
| FR-014 | solo este workflow corre en `debfactory-main`, y el hook lo garantiza (research D3) |
| FR-017a | ruleset en `main`; `contents: write` solo sirve para `debfactory/state` |
| FR-018 | `concurrency` con `queue: max`, un solo runner `pub` y el `flock` de debfactory |
| FR-026 a FR-028 | `workflow_dispatch`, que solo puede lanzar quien tiene escritura (el mantenedor) |
| FR-031 | `action=refresh`, lanzado por el temporizador |

## Configuración de GitHub (la comprueba `ci doctor`)

| Ajuste | Valor |
|---|---|
| Repositorio | público, en la cuenta personal del mantenedor |
| Actions → Fork pull request workflows | Require approval for all external contributors |
| Actions → Workflow permissions | Read repository contents (por defecto); Allow GitHub Actions to create and approve pull requests: **sí** |
| Ruleset `main` | objetivo: rama por defecto; require a pull request before merging; **require status checks to pass: `verify`** (fuente GitHub Actions); block force pushes; restrict deletions; **sin bypass actors** |
| Etiquetas | `pipeline-failure` |
| Runners | ninguno persistente. Los JIT aparecen y desaparecen con cada job (research D4) |
