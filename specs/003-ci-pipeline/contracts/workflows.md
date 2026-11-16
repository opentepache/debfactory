# Contrato: workflows de GitHub Actions

Hay dos archivos en `.github/workflows/`. Ninguno usa actions de terceros salvo
`actions/checkout`, fijada por sha de commit (research D16). Ninguno lee secretos de GitHub:
`secrets.*` no aparece en ningún workflow.

## `pr.yml`: pull requests (nivel `ci`)

```yaml
name: pr
on:
  pull_request:
    paths-ignore: ['docs/**', 'specs/**', '**.md', 'repo/state.yaml', 'repo/audit.jsonl']
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
      - run: go build -o bin/debfactory ./cmd/debfactory
      - run: bin/debfactory ci pr --base origin/${{ github.base_ref }}
```

| Requisito | Cómo |
|---|---|
| FR-008 | `ci pr` escribe el resumen; el check del PR refleja el código de salida |
| FR-009 y FR-017 | `permissions: contents: read`; el runner `ci` no tiene ningún secreto montado |
| FR-011 | `paths-ignore` |
| FR-013 | ajuste "Require approval for all external contributors" (research D12) |
| PR de estado | `paths-ignore` de `repo/state.yaml` y `repo/audit.jsonl` (research D8) |

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
      - run: go build -o bin/debfactory ./cmd/debfactory
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
| Ruleset `main` | objetivo: rama por defecto; require a pull request before merging; block force pushes; restrict deletions; **sin bypass actors** |
| Etiquetas | `pipeline-failure` |
| Runners | ninguno persistente. Los JIT aparecen y desaparecen con cada job (research D4) |
