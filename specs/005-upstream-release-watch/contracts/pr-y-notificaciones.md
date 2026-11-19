# Contrato: PR del detector y notificaciones

## PR de actualización

| Elemento | Valor |
|---|---|
| Rama | `debfactory/update/<pkg>` desde `main` |
| Commit | uno, autor la App, mensaje = título |
| Archivos | `packages/<pkg>.yaml` (`version`, `revision: 1` y, solo con cambio de licencia permitido, `license.spdx`) y `packages/<pkg>.lock.yaml` |
| Título | `<pkg>: <anterior> → <nueva>` |
| Auto-merge | activado solo si `watch.auto_merge ∧ ¬mayor ∧ licencia igual` |

Cuerpo:

```markdown
<!-- debfactory-watch pkg=bob version=4.4.0 -->
Versión nueva de **bob** propuesta por el detector.

| | |
|---|---|
| Anterior | 4.3.0 (`4.3.0-0~tepache1`) |
| Nueva | 4.4.0 → `4.4.0-0~tepache1` |
| Notas | https://github.com/MordechaiHadad/bob/releases/tag/v4.4.0 |
| Publicada upstream | 2026-11-19 08:00 UTC (espera de 48 h cumplida) |
| Licencia | MIT, sin cambios |
| Cambio mayor | no |
| Auto-merge | sí: se integra solo cuando `verify` pase |
```

Si no hay auto-merge, la fila dice el motivo: `no (desactivado en el manifiesto)`,
`no (cambio de versión mayor)`, `no (cambio de licencia MIT → Apache-2.0)`.

Comentario cuando hay commits ajenos (FR-009):

```markdown
<!-- debfactory-watch-newer version=4.5.0 -->
Hay una versión más nueva de upstream: **4.5.0**. Esta rama tiene commits que no son del
detector, así que no la actualizo. Ciérrala para que el próximo ciclo proponga 4.5.0 desde cero.
```

Un comentario por versión (el marcador evita repetirlo).

## Notificaciones

Mismo formato y ciclo de vida que specs/003-ci-pipeline/contracts/notificaciones.md: un issue
por `paquete/versión/etapa` con la etiqueta de fallos, que se actualiza si se repite y se cierra
solo. Etapas nuevas (data-model §6):

| Etapa | Título | Cierre automático |
|---|---|---|
| `watch-license` | `fallo: bob 4.4.0 en watch-license` | PR válido para `bob` o 4.4.0 deja de ser candidata |
| `watch-assets` | `fallo: bob 4.4.0 en watch-assets` | igual |
| `watch-checksums` | `fallo: bob 4.4.0 en watch-checksums` | igual |
| `watch-tampered` | `fallo: bob 4.4.0 en watch-tampered` | solo el mantenedor (posible manipulación) |
| `watch-ratelimit` | `fallo: watch - en watch-ratelimit` | un ciclo completo sin detenerse |

Mientras el issue de una clave está abierto, el detector no vuelve a preparar esa versión
(FR-018), salvo que el manifiesto del paquete haya cambiado en `main` desde que se abrió el
issue (se compara el commit que lo tocó por última vez con la fecha del issue).
