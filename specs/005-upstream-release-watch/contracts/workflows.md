# Contrato: `watch.yml`, la GitHub App y el timer

## `watch.yml` (nuevo, en `packages`)

```yaml
name: watch

on:
  workflow_dispatch:
    inputs:
      package:
        description: Solo este paquete (vacío = todos)
        type: string
        required: false
      force:
        description: Ignorar watch.interval (nunca la espera de 48 h)
        type: boolean
        default: false

permissions:
  contents: read
  issues: write            # notificaciones del detector (etapas watch-*)

concurrency:
  group: watch
  cancel-in-progress: false

jobs:
  watch:
    runs-on: [self-hosted, debfactory-pr]
    environment: watch
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: main
          fetch-depth: 0
          persist-credentials: false
      - id: app
        uses: actions/create-github-app-token@64ab729a6294000f5aa4a4bc0b73d1f32bc45510 # v3.3.0
        with:
          client-id: ${{ secrets.DEBFACTORY_APP_CLIENT_ID }}
          private-key: ${{ secrets.DEBFACTORY_APP_KEY }}
          permission-contents: write
          permission-pull-requests: write
      - uses: actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: ${{ runner.temp }}/debfactory-watch
          key: watch-${{ github.run_id }}
          restore-keys: watch-
      - name: debfactory ci watch
        env:
          GITHUB_TOKEN: ${{ github.token }}
          DEBFACTORY_APP_TOKEN: ${{ steps.app.outputs.token }}
          DEBFACTORY_APP_SLUG: ${{ steps.app.outputs.app-slug }}
          INPUT_PACKAGE: ${{ inputs.package }}
          INPUT_FORCE: ${{ inputs.force }}
        # Las entradas solo pasan por env: nunca se interpolan en el script.
        run: debfactory ci watch
```

| Requisito | Cómo |
|---|---|
| FR-001 | `ci watch` lee el inicio del último ciclo completo de la caché (`$RUNNER_TEMP/debfactory-watch/state.json`) y no hace nada hasta que vence `watch.interval` |
| FR-005 | `concurrency: watch` + comprobación de intervalo al empezar |
| FR-011 | los PR los abre la App: `pr.yml` corre sin aprobación y `verify` es obligatorio |
| FR-012 | runner `ci` (sin `<data>`); `ci watch` se niega en el runner `pub` |
| FR-021 | ETag, cuerpos y pendientes en la caché de `main` (`actions/cache` solo la guarda si el job termina bien); el token de la App tiene su propio límite |
| FR-022 | sin novedades no hay push, PR, comentario ni issue |

**Cambio respecto de 003**: `watch.yml` es el único workflow que lee `secrets.*`, y solo desde
el environment `watch` (rama `main`). `pr.yml` y `main.yml` siguen sin secretos.

`pr.yml` y `main.yml` no cambian: un PR de la App es un PR normal, y su merge (por la App) es un
`push` normal a `main`. `trusted-gate.sh` tampoco cambia: `watch.yml` nunca llega al runner
`pub`.

## GitHub App `opentepache-updates`

| Ajuste | Valor |
|---|---|
| Propietario | organización `opentepache` |
| Instalación | solo `opentepache/packages` |
| Permisos de repositorio | Contents: Read and write; Pull requests: Read and write; Metadata: Read (obligatorio). Ninguno más |
| Webhooks | desactivados |
| Llave privada | en el environment `watch` (`DEBFACTORY_APP_KEY`); el Client ID en `DEBFACTORY_APP_CLIENT_ID` (la v3 de la acción usa `client-id`; `app-id` está obsoleta) |
| Ruleset de `main` | sin bypass: la App no puede empujar a `main` ni integrar sin `verify` |

## Repositorio `packages`

- Settings → General → **Allow auto-merge**: activado.
- Settings → Environments → `watch`: *Deployment branches* = `main`; secretos de la App.

## Host: `debfactory-watch.timer`

```ini
[Timer]
OnCalendar=hourly
Persistent=true
RandomizedDelaySec=10m
```

`debfactory-watch.service` igual que `debfactory-refresh.service`, con
`ExecStart=/usr/local/lib/debfactory/dispatch.sh watch.yml '{}'`. `dispatch-refresh.sh` pasa a
ser `dispatch.sh <workflow> <inputs-json>` (prueba: `ci/runner/dispatch_test.sh`) y el refirmado
lo llama con `main.yml '{"action":"refresh"}'`. `setup.sh` instala las dos unidades pero no
habilita el timer: se habilita a mano cuando `watch.yml` ya está en `main` (antes, cada disparo
daría HTTP 404).
