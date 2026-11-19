# Research: Detección de versiones upstream y actualización automática

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-11-19

Cada decisión sigue el formato Decisión / Motivo / Alternativas. Las referencias a la
documentación de GitHub se consultaron el 2026-11-19 (docs.github.com, "GITHUB_TOKEN" y
"Events that trigger workflows").

## D1. Identidad del detector: una GitHub App propia

**Decisión**: el detector abre, actualiza y programa el auto-merge de sus PR con un token de
instalación de una GitHub App de la organización (`opentepache-updates`, gratuita), con permisos
de repositorio **Contents: write** y **Pull requests: write** sobre `opentepache/packages`
únicamente. El token se emite en cada ejecución (dura 1 hora) con
`actions/create-github-app-token` fijada por sha, y se reduce a esos dos permisos.

**Motivo**: lo vimos en la fase 004 (PR de estado #8) y lo confirma la documentación:

- Un PR que abre o actualiza el `GITHUB_TOKEN` crea sus ejecuciones de `pull_request` **en
  estado de aprobación requerida**. `verify` nunca correría sin que el mantenedor pulse
  "Approve workflows to run": eso rompe el camino feliz (FR-011, SC-002).
- Un merge hecho con el `GITHUB_TOKEN` (incluido el auto-merge que él activó) **no crea**
  ejecuciones de `push`: `main.yml` no correría y nada se publicaría.
- Con un token de GitHub App, ambos eventos crean ejecuciones normales, sin aprobación.

El ruleset de `main` (sin bypass) sigue aplicando a la App: no puede empujar a `main`, solo a sus
ramas, y no puede integrar sin `verify` en verde.

**Alternativas**:

- *`GITHUB_TOKEN` + aprobación manual*: deja un clic humano por actualización; contradice el
  principio VIII.
- *`GITHUB_TOKEN` + `workflow_dispatch` de `pr.yml` sobre la rama*: el check `verify` saldría,
  pero el merge automático seguiría sin disparar `main.yml`; habría que encadenar otro dispatch y
  dejar una ejecución "pendiente de aprobación" que falla al integrar (ruido visto en #8).
- *Token personal (PAT) del mantenedor como secreto*: actúa como el mantenedor en todo el
  repositorio, caduca y está ligado a una persona. La App tiene permisos mínimos, no caduca su
  llave y sus acciones se distinguen en el historial.

## D2. Dónde corre: el runner `ci`, nunca el confiable

**Decisión**: el ciclo es un workflow nuevo `watch.yml` en `packages`, con un único job sobre
`[self-hosted, debfactory-pr]` (usuario `dfci`). Su orden es `debfactory ci watch`.

**Motivo**: FR-012 exige que el detector no tenga acceso a la llave ni a las credenciales de
publicación. En el runner `pub` el proceso podría leer `<data>` (llavero GPG, credenciales de
R2). El runner `ci` no tiene `<data>` montado, es efímero (JIT) y está aislado por nftables. El
detector descarga artefactos para calcular hashes, pero no ejecuta nada de upstream.

**Consecuencia sobre la fase 003**: su contrato decía que ningún workflow lee `secrets.*`. Ahora
uno lo hace: `watch.yml`, a través de un **environment** de GitHub (`watch`) limitado a la rama
`main`. Un job de `pull_request` (de un fork o no) no puede leer secretos de ese environment, y
cada job JIT tiene su propio contenedor. Se registra en Complexity Tracking del plan.

**Alternativas**: el runner `pub` (rechazada por FR-012); un servicio en el host (rechazada:
añade código de debfactory con red y credenciales fuera de los contenedores, contra IX).

## D3. Disparo: timer horario en la PC, intervalo decidido por la configuración

**Decisión**: `debfactory-watch.timer` (cada hora, `Persistent=true`) lanza `workflow_dispatch`
de `watch.yml` con el mismo script y la misma credencial que el refirmado
(`dispatch-refresh.sh` se generaliza a `dispatch.sh <workflow> <inputs>`). `ci watch` consulta
la hora de inicio del último ciclo completado con éxito (API de ejecuciones de `watch.yml`) y, si
no han pasado `watch.interval` (por defecto 24 h, en `config/debfactory.yaml`), termina con "ciclo
no vencido" sin consultar upstream. El mantenedor lanza un ciclo con `watch.yml` → "Run workflow"
(entradas `package` y `force`).

**Motivo**: `on: schedule` se desactiva tras 60 días sin actividad en repos públicos (research
D11 de 003). El intervalo vive en el repositorio de paquetes (FR-001) y no en el host. Con la PC
apagada, el siguiente disparo horario recupera el ciclo una sola vez (edge case "PC apagada").

**Alternativas**: timer diario en el host (el intervalo quedaría en el host, no en la
configuración); `on: schedule` (se desactiva solo).

## D4. Un solo ciclo a la vez

**Decisión**: `concurrency: {group: watch, cancel-in-progress: false}` en `watch.yml`, con un
único job. Un ciclo que llega mientras otro corre queda en cola; al empezar, la comprobación de
intervalo de D3 lo convierte en "ciclo no vencido" (FR-005).

**Alternativas**: un candado en disco (el runner `ci` es efímero; no hay disco compartido).

## D5. Qué es una versión estable (FR-003, FR-003a)

**Decisión**, en este orden, para cada release de upstream:

1. Descartar `draft`.
2. Descartar si `prerelease: true`, salvo que el manifiesto declare
   `watch.prerelease_flag: ignore`.
3. Obtener la versión invirtiendo la plantilla `tag` del manifiesto (`v{{version}}` por
   defecto, `release-{{version}}`, etc.). Si la etiqueta no encaja, el release no es de este
   paquete y se descarta sin notificar.
4. Descartar si la versión contiene, sin distinguir mayúsculas, alguno de `alpha`, `beta`, `rc`,
   `pre`, `preview`, `nightly`, `tip`, `canary`, `dev`, `snapshot` como componente (separado por
   `-`, `.`, `_`, `+` o pegado a un número: `1.2.0rc1`). Este filtro **no** se puede desactivar.
5. Exigir que la versión empiece por un dígito.

El "patrón propio de etiquetas" de FR-003a es la plantilla `tag` que ya existe; no se añade otro
campo.

**Alternativas**: un `tag_regex` libre en el manifiesto (más poder del necesario y una regex
mal escrita podría dejar pasar una RC).

## D6. Comparar versiones y elegir la candidata (FR-004, FR-004a)

**Decisión**: candidata = la versión estable más alta, según `dpkg --compare-versions` (ya se usa
en `internal/installtest`), que sea mayor que la del manifiesto en `main` y cuyo release lleve
publicado al menos `watch.cooldown` (48 h por defecto) según `published_at`. La espera se aplica
también a ciclos manuales y con `force`.

**FR-020**: con la espera de 48 h ningún release se evalúa antes de dos ciclos diarios; si tras
la espera faltan los assets, es un cambio de formato real y se notifica en ese mismo ciclo. No
hace falta estado propio para contar ciclos.

## D7. Preparar la propuesta sin estado propio

**Decisión**: para la candidata, `ci watch` hace en un worktree de `main`:

1. Cambia `version`, pone `revision: 1`.
2. Ejecuta la orden de lockfile existente (`debfactory lock`): descarga artefactos, verifica
   checksums upstream (III), obtiene el texto de licencia y escribe los hashes.
3. Aplica el gate de licencias existente (`internal/license.Gate`) sobre el texto nuevo.
4. Compara con el PR abierto (si existe) para la misma versión: si los hashes del lockfile
   difieren, es un asset resubido con la misma etiqueta (FR-016 d).

No hay base de datos del detector. Lo que debe recordarse entre ciclos ya está en GitHub:

| Hecho | Dónde vive |
|---|---|
| propuesta en curso | el PR abierto de la rama `debfactory/update/<pkg>` |
| problema ya notificado | el issue de notificación abierto (marcador de 003) |
| último ciclo | las ejecuciones de `watch.yml` |
| respuestas no modificadas | caché de Actions (D9) |

## D8. Cambio de licencia (FR-015, FR-016 a)

**Decisión**: se compara la licencia **detectada** en el texto nuevo con la `license.spdx`
declarada:

- Compatible con la declarada → no hay cambio de licencia (aunque el texto cambie, p. ej. el año).
- Incompatible, y lo detectado es una sola licencia de la allowlist → el PR cambia también
  `license.spdx`, se marca como "cambio de licencia" y nunca se integra solo.
- Incompatible y fuera de la allowlist, o no identificable (cobertura < 90 %) → notificación
  `watch-license`, sin PR.

## D9. Límites de la API (FR-021, SC-006, SC-007)

**Decisión**:

- Una petición por paquete: `GET /repos/{upstream}/releases?per_page=30` con `If-None-Match`.
  Las respuestas `304` no cuentan contra el límite. Los ETag y cuerpos se guardan con
  `actions/cache` en la caché de `main` (los PR no pueden escribir en ella).
- Todas las peticiones usan el token de la App (5 000/h por instalación), no el `GITHUB_TOKEN`
  (1 000/h por repositorio), para no competir con el pipeline. Que ese token, instalado solo en
  `packages`, pueda leer releases de repositorios públicos ajenos y con qué límite se comprueba
  en el spike (tasks T003); si no puede, las lecturas de upstream usan el `GITHUB_TOKEN` y la
  reserva se recalcula sobre 1 000/h.
- Antes de cada paquete se mira `X-RateLimit-Remaining`; por debajo de `watch.rate_reserve`
  (por defecto 500) el ciclo se detiene, lista los paquetes pendientes en el resumen y termina en
  verde. El orden de revisión empieza por los que no se revisaron en el ciclo anterior.
- Ante `403/429` con `Retry-After` o `X-RateLimit-Reset`, se detiene igual (no reintenta).

Con 100 paquetes, un ciclo sin novedades cuesta ≤ 100 peticiones (casi todas `304`).

## D10. Auto-merge (FR-013–FR-015a)

**Decisión**: con `watch.auto_merge: true` en el manifiesto y ninguna de las excepciones (cambio
mayor, de licencia u otros campos), `ci watch` activa el auto-merge nativo del PR
(`enablePullRequestAutoMerge`, método `MERGE`, GraphQL) con el token de la App. GitHub integra
cuando `verify` pasa; el push a `main` lo hace la App y dispara `main.yml`, que publica. Si
`verify` falla, el PR queda abierto y la notificación de fallo es la del pipeline (003).

Requisito del repositorio: "Allow auto-merge" activado (`ci doctor` lo comprueba).

## D11. Ramas y PRs

**Decisión**: rama `debfactory/update/<pkg>` desde `main`, un solo commit de la App, título
`<pkg>: <anterior> → <nueva>`. Al actualizar se reescribe con `--force-with-lease` solo si todos
los commits de la rama son de la App; si hay commits de otra persona, se comenta (FR-009). Si la
candidata desaparece (release retirado) y ya no hay versión nueva, el PR se cierra con un
comentario y se borra la rama. El cuerpo lleva un marcador HTML con el paquete y la versión
para reconocerlo.

## D12. Notificaciones

**Decisión**: el notificador de la fase 003 (issues con la etiqueta de fallos y un marcador por
paquete, versión y etapa) con etapas nuevas: `watch-license`, `watch-assets`,
`watch-checksums`, `watch-tampered`, `watch-ratelimit` (solo si tres ciclos seguidos se detienen
por límite). Un issue abierto para la misma clave suprime reintentos (FR-018); se cierra solo
cuando hay PR válido para el paquete o la versión deja de ser candidata (FR-019). Los issues se
crean con el `GITHUB_TOKEN` del job (`issues: write`).

## D13. Pruebas

**Decisión**: el GitHub falso de `internal/ci/citest` añade releases con ETag/304, cabeceras de
límite, refs y la mutación GraphQL de auto-merge. Un servidor httptest sirve los assets y
licencias de un upstream simulado de `bob`. Las pruebas de integración cubren SC-001 a SC-007;
el e2e local (`just e2e`) recorre release simulado → PR → auto-merge → publicación en el
repositorio local. La prueba en producción es la primera actualización real (quickstart §5).

## D14. Versiones de las acciones

- `actions/create-github-app-token` **v3.3.0** (`64ab729a6294000f5aa4a4bc0b73d1f32bc45510`,
  2026-10-12). La v3.4.0 tiene un día; se revisará en la siguiente actualización.
- `actions/cache` **v6.1.0** (`55cc8345863c7cc4c66a329aec7e433d2d1c52a9`, 2026-06-26).
- `actions/checkout` sigue en la v7.0.1 fijada en 003.
