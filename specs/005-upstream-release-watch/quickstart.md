# Quickstart: validar el detector de versiones

**Feature**: [spec.md](spec.md) | Contratos: [cli](contracts/cli.md),
[workflows](contracts/workflows.md), [PR y notificaciones](contracts/pr-y-notificaciones.md)

## Requisitos

- Fase 004 terminada: `ci doctor` en verde, herramienta fijada en `packages`.
- La GitHub App `opentepache-updates` creada e instalada solo en `packages` (contracts/workflows.md).
- Environment `watch` con `DEBFACTORY_APP_ID` y `DEBFACTORY_APP_KEY`, limitado a `main`.
- "Allow auto-merge" activado en `packages`.
- La herramienta con `ci watch` instalada (`just tool-install`) y fijada en `packages`.

## 1. Pruebas de la herramienta (sin red, sin GitHub real)

```bash
just check            # incluye las pruebas de internal/watch e internal/ci con el GitHub falso
just e2e              # recorrido: release simulado de bob → PR → auto-merge → publicación local
```

Esperado (SC-001 a SC-007 contra el upstream simulado):

| Escenario | Resultado |
|---|---|
| bob 4.4.0 estable publicado hace 49 h | 1 PR `bob: 4.3.0 → 4.4.0`, revisión 1, lockfile nuevo; con auto-merge, publicado en el repositorio local |
| bob 4.4.0 publicado hace 10 h | sin PR; resumen `en-espera (elegible …)` |
| bob 4.4.0 con `prerelease: true`, o `v4.4.0-rc1` sin la marca | 0 PRs, 0 issues |
| bob 4.4.0 con licencia fuera de la allowlist, 3 ciclos | 1 issue `watch-license`, 0 PRs, nada publicado |
| bob 5.0.0 con auto-merge | PR sin auto-merge, "cambio de versión mayor" |
| 4.4.0 y luego 4.5.0 | el mismo PR pasa a 4.5.0 (nunca 2 PRs) |
| ciclo sin novedades | todas las respuestas `304`; sin cambios en el repositorio |
| límite simulado casi agotado | ciclo detenido en verde, pendientes listados y revisados primero en el siguiente |

## 2. Barrera de confianza

```bash
bin/debfactory ci watch            # fuera de CI → código 3
bin/debfactory ci watch --dry-run  # con GH_TOKEN de lectura: imprime decisiones, no escribe nada
```

## 3. Primer ciclo real (sin auto-merge en ningún paquete)

En `packages`: Actions → **watch** → Run workflow (`force: true`). Esperado: una línea por
paquete; con todo al día, `sin-cambios` y ningún PR. Repetir sin `force`: "ciclo no vencido".

`sudo systemctl enable --now debfactory-watch.timer` y, una hora después, comprobar en Actions
que hubo una ejecución de `watch` y que terminó en "ciclo no vencido" o en un ciclo completo.

## 4. Límites (SC-006)

En el resumen del segundo ciclo completo: `peticiones N (N × 304)`, con N = número de paquetes.

## 5. Primera actualización real (US1, US3, SC-001, SC-002)

Cuando upstream publique una versión estable de un paquete:

1. 48 h después, como mucho en el siguiente ciclo, aparece `<pkg>: X → Y` abierto por la App, y
   `verify` corre **sin pedir aprobación**.
2. Sin auto-merge: integrar a mano; `main` publica (como en 003).
3. Activar `watch.auto_merge: true` en ese paquete (PR normal). En su siguiente versión menor el
   PR se integra solo tras `verify`, `main.yml` corre por el push de la App y la versión aparece
   en apt (`apt-5-suites.sh`) sin ninguna acción humana.

## 6. `ci doctor`

```bash
GH_TOKEN=$(gh auth token) just ci-doctor
```

Las comprobaciones nuevas (App, environment, auto-merge, timer) en verde.
