# Contrato: `debfactory ci watch`

Orden nueva del grupo `ci` (specs/003-ci-pipeline/contracts/cli.md). La ejecuta `watch.yml`.

```text
debfactory ci watch [--package PKG] [--force] [--dry-run]
```

| Opción | Efecto |
|---|---|
| `--package PKG` | revisa solo ese paquete |
| `--force` | ignora `watch.interval` (no la espera de 48 h: FR-004a) |
| `--dry-run` | consulta upstream y decide, pero no crea ramas, PRs, comentarios ni issues; imprime lo que haría. Funciona fuera de CI con un token de lectura (`GH_TOKEN`) |

## Entorno

| Variable | Origen | Uso |
|---|---|---|
| `GITHUB_TOKEN` | job (`issues: write`, `actions: read`) | notificaciones; ejecuciones de `watch.yml` (D3) |
| `DEBFACTORY_APP_TOKEN` | `actions/create-github-app-token` | releases upstream, ramas, PRs, comentarios, auto-merge |
| `GITHUB_*`, `RUNNER_LABELS` | runner | barrera de confianza |

## Barrera de confianza (sin `--dry-run`)

Sale con código 3 si no se cumple todo: `GITHUB_ACTIONS=true`, evento `workflow_dispatch`, ref
`refs/heads/main`, repositorio = `ci.repository`, `RUNNER_LABELS` contiene `debfactory-pr` y
**no** `debfactory-main` (FR-012: nunca en el runner con `<data>`), y existe
`DEBFACTORY_APP_TOKEN`.

## Pasos por ciclo

1. Barrera; leer `config/` y manifiestos de `main`.
2. Si no `--force`: último ciclo con éxito + `watch.interval` > ahora → "ciclo no vencido", 0.
3. Por paquete (orden: pendientes del ciclo anterior primero), mientras
   `X-RateLimit-Remaining > watch.rate_reserve`:
   releases (con ETag) → candidata (research D5, D6) → issue abierto para la misma clave → sin
   reintento → preparar (D7) → licencia (D8) → PR (D11) → auto-merge (D10) → cerrar issues
   resueltos (D12).
4. Resumen (data-model §7).

## Salida y códigos

| Código | Cuándo |
|---|---|
| 0 | ciclo completo o detenido por límite; las notificaciones no son error del ciclo |
| 1 | fallo del propio ciclo (API caída, git) que impidió revisar paquetes; se notifica como `pipeline` |
| 2 | uso incorrecto |
| 3 | barrera de confianza o falta el token de la App |

Una línea por paquete en la salida estándar, con el mismo formato que `ci plan`:

```text
watch: bob 4.3.0 → 4.4.0: pr-abierto #12 (auto-merge)
watch: fnm 1.39.0: sin-cambios
watch: aqua 2.66.1 → 2.67.0: en-espera (elegible 2026-11-21 10:00 UTC)
watch: k9s 0.51.0 → 0.52.0: notificado watch-assets #13
watch: peticiones 6 (5 × 304); restantes 4987
```

## `ci doctor` (comprobaciones nuevas)

- Environment `watch` limitado a `main`, con los secretos `DEBFACTORY_APP_ID` y
  `DEBFACTORY_APP_KEY` (por nombre). La instalación y los permisos de la App no se pueden leer
  con el token del mantenedor: se comprueban en el spike (tasks T003) y los documenta
  `docs/operacion-ci.md`.
- "Allow auto-merge" activado si algún manifiesto tiene `watch.auto_merge: true`.
- `debfactory-watch.timer` habilitado.
