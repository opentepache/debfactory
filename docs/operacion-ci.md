# Operación del pipeline

Cómo se instala, se opera y se recupera el pipeline de construcción y publicación
([specs/003-ci-pipeline](../specs/003-ci-pipeline/spec.md)), repartido en dos repositorios
([specs/004-split-repos](../specs/004-split-repos/spec.md)). Desde que está en marcha es el
**único escritor** del repositorio público: el mantenedor ya no publica, retira ni sincroniza
`r2` a mano. Las órdenes locales siguen sirviendo para probar con datos desechables y con
`r2test`.

## Dos repositorios

| Repositorio | Visibilidad | Contiene | En la PC |
|---|---|---|---|
| `opentepache/debfactory` | privado, sin ruleset ni runners | la herramienta, la imagen del runner, `ci/host`, pruebas, specs y esta guía | `~/Develop/debfactory` |
| `opentepache/packages` | público, ruleset en `main` | `packages/`, `config/`, `repo/`, `docs/usuario.md`, workflows y `debfactory.version` | `~/Develop/packages` |

El `justfile` de la herramienta trabaja sobre `../packages` (cámbialo con `DEBFACTORY_ROOT`).
Las pruebas de la herramienta usan siempre su copia congelada `testdata/project/`.

## Piezas

| Pieza | Dónde | Qué hace |
|---|---|---|
| `pr.yml` | `packages`, runner `debfactory-pr` (usuario `dfci`) | Check de cada PR: gates 1–6, sin secretos ni publicación |
| `main.yml` | `packages`, runner `debfactory-main` (usuario `dfpub`) | Push a `main`, ejecuciones manuales y refirmado |
| `debfactory.version` | `packages` | Commit de la herramienta que usan los jobs; cambia solo por PR |
| `/srv/debfactory/tools/<commit>/` | PC, de root | Cada versión instalada con `just tool-install`; montado de solo lectura en los dos runners |
| lanzador `debfactory` | imagen del runner | Ejecuta la versión que fija `debfactory.version`, o falla nombrando la que falta |
| `debfactory-runner@{ci,pub}` | PC, systemd | Un contenedor nuevo por job, con configuración JIT de un solo uso |
| `trusted-gate.sh` | imagen `pub` | Rechaza en el runner confiable todo lo que no sea `main.yml` en `main` de `packages` |
| `debfactory-refresh.timer` | PC, systemd | Lanza cada semana `refresh` (vuelve a firmar `Valid-Until`) |
| `watch.yml` | `packages`, runner `debfactory-pr` (usuario `dfci`) | Detector de versiones: propone cada versión estable nueva como PR (§ 6) |
| `debfactory-watch.timer` | PC, systemd | Lanza `watch.yml` cada hora; el ciclo completo corre cada `watch.interval` (24 h) |
| GitHub App `opentepache-updates` | organización `opentepache` | Identidad del detector: sus PR disparan `verify` sin aprobación y su merge dispara `main.yml` |
| `/srv/debfactory/data` | PC, dueño `dfpub` | Llavero de la subllave, almacén, generaciones y la copia del pipeline (`ci/`) |
| PR de estado | rama `debfactory/state` de `packages` | `repo/state.yaml` y `repo/audit.jsonl` tras cada publicación; lo integras tú |
| Issues `pipeline-failure` | `packages` (públicos) | Un issue por paquete, versión y etapa que falla; se cierran solos |

## 1. Instalación (una vez)

Requisitos en la PC: `sudo apt install systemd-container uidmap jq nftables git-filter-repo`
(Docker CE con `docker-ce-rootless-extras` ya está instalado).

La migración desde el repositorio único está en
[contracts/migracion.md](../specs/004-split-repos/contracts/migracion.md) (`ci/migrate/split.sh`).
Para una instalación nueva:

1. **Herramienta.** En `~/Develop/debfactory`, con el árbol limpio: `just tool-install`. Ejecuta
   todas las pruebas, compila con el commit embebido e instala en
   `/srv/debfactory/tools/<commit>/`. Imprime el commit.
2. **Repositorio `packages` en GitHub**, público, con `debfactory.version` = ese commit y
   `ci.repository: opentepache/packages` en `config/debfactory.yaml`.
3. **PAT.** En GitHub → Settings → Developer settings → Fine-grained tokens, crea uno solo para
   `opentepache/packages` con `Administration: write` (runners JIT) y `Actions: write`
   (refresh). No lo pegues en ningún chat ni archivo del repositorio.
4. **PC.**

   ```bash
   chmod 700 ~                          # setup.sh lo exige
   just runner-image                    # imprime la etiqueta, p. ej. 2.338.2-aef378b
   sudo ci/host/setup.sh --repository opentepache/packages --runner-tag 2.338.2-aef378b --token-stdin
   # pega el PAT cuando lo pida (no se muestra)
   ```

   `setup.sh` es idempotente: crea `dfci` y `dfpub`, su Docker rootless, `/srv/debfactory`
   (con `tools/`), las unidades, el temporizador, la regla de nftables y el credstore del PAT, y
   carga las imágenes del runner en el Docker de cada usuario (fijadas por ID de contenido).
5. **GitHub (Settings de `packages`).**
   - Actions → General → Fork pull request workflows from outside collaborators: **Require
     approval for all external contributors**.
   - Actions → General → Workflow permissions: **Read repository contents** y marca **Allow
     GitHub Actions to create and approve pull requests**.
   - Rules → Rulesets → New branch ruleset sobre la rama por defecto: *Require a pull request
     before merging*, *Require status checks to pass* con el check **`verify`** (fuente GitHub
     Actions), *Block force pushes*, *Restrict deletions*, **sin bypass**. Sin el check
     obligatorio, un PR integrado mientras su check está en cola llega a `main` sin probar; los
     PR de estado lo reciben de la propia ejecución de `main`.
   - Notifications (tu cuenta) → Actions: correo para ejecuciones fallidas. Cubre los jobs que
     GitHub cancela tras 24 h en cola si la PC está apagada.
6. **Comprobar.** Con un token tuyo de lectura de la administración del repositorio:

   ```bash
   GH_TOKEN=$(gh auth token) just ci-doctor          # informe
   GH_TOKEN=$(gh auth token) just ci-doctor --fix    # además crea la etiqueta pipeline-failure
   ```

7. **Datos.** Con todo en verde, mueve los datos del repositorio a `dfpub`. Si la generación
   local es anterior a la fase 003 (sin `Valid-Until`; `repo verify` lo dice), vuelve a firmarla
   antes:

   ```bash
   bin/debfactory repo rebuild --root ~/Develop/packages     # solo si repo verify pide Valid-Until
   sudo ci/host/migrate-data.sh --root ~/Develop/packages
   sudo systemctl enable --now debfactory-runner@ci debfactory-runner@pub
   ```

   Después lanza `refresh` (Actions → main → Run workflow) para que R2 sirva la generación con
   `Valid-Until`.

## 2. Operación diaria

- **Subir un paquete.** En `~/Develop/packages`, en una rama: edita `packages/<p>.yaml`; desde
  `~/Develop/debfactory`, `just lock <p>`; commit, PR. Espera el check verde, integra. La
  ejecución de `main` construye, prueba, publica, sincroniza, verifica y abre o actualiza el PR
  de estado. Los usuarios ya tienen la versión antes de que lo integres.
- **PR de un contribuyente externo.** El check espera tu aprobación ("Approve workflows to run")
  en cada push; revisa antes el diff, sobre todo `.github/`. Si cambia un manifiesto, su lockfile
  no será coherente (no tiene la herramienta): ejecuta `just lock <p>` en su rama y súbelo
  (GitHub lo permite con *Allow edits by maintainers*). Aun aprobado, su código corre como
  `dfci`, sin secretos ni red local, y el hook impide que llegue al runner confiable.
- **PR de estado.** Intégralo sin editarlo cuando quieras. Su merge no publica nada. Si lleva más
  de 7 días abierto, aparece un issue `state-pr-age`.
- **Issues de fallo.** Título `fallo: <paquete> <versión> en <etapa>`, con el motivo y el enlace
  al log. Un fallo repetido añade un comentario; la ejecución que lo resuelve lo cierra. Son
  públicos.
- **Ejecuciones manuales.** En `packages`, Actions → main → Run workflow:

  | Acción | Entradas | Efecto |
  |---|---|---|
  | `rebuild` | paquete, versión | Construye y prueba desde el commit que la declaró, con la herramienta que fija hoy `main`; si está publicada compara los sha256 y no publica |
  | `retire` | paquete, versión, motivo, `last` | Retira, sincroniza, verifica y deja el retiro en la auditoría |
  | `refresh` | — | Vuelve a firmar con `Valid-Until` nuevo, sincroniza y verifica |
  | `restore-state-pr` | — | Vuelve a crear el PR de estado desde la copia del pipeline |

## 3. Actualizar la herramienta

1. En `~/Develop/debfactory`, integra los cambios en `main` y ejecuta `just tool-install`. Se
   niega si hay cambios sin commit o alguna prueba falla. La versión anterior sigue instalada.
2. En `~/Develop/packages`, en una rama: `echo <commit> > debfactory.version`, commit, PR.
3. El check reconstruye todos los paquetes publicados con la versión nueva y compara los sha256;
   su resumen empieza con el enlace a las diferencias entre las dos versiones. Si un `.deb`
   publicado cambia, falla nombrando el paquete: sube su `revision` en el mismo PR o corrige la
   herramienta.
4. Integra. Hasta ese momento, `main` sigue usando la versión anterior sin fallar.
5. **Podar versiones viejas.** `ci doctor` avisa de las instaladas que `main` no fija. Antes de
   borrar una, comprueba que ningún PR abierto la fija (el aviso no lo consulta):
   `sudo rm -r /srv/debfactory/tools/<commit>`.

## 4. Recuperación

| Situación | Qué hacer |
|---|---|
| Una ejecución murió a mitad (corte, `systemctl kill`) | Relanza (un push vacío o `rebuild` de esa versión). La siguiente completa sync, verify y el PR de estado sin reconstruir (FR-029). |
| Un job falla en el lanzador (`la versión … no está instalada`) o en la etapa `tool` | `ci notify` tampoco puede correr porque usa el mismo lanzador: solo llega el correo de GitHub por la ejecución fallida. Instala la versión fijada (`git checkout <commit> && just tool-install` en `debfactory`) y relanza. |
| Issue `fallo: pipeline - en state` | El PR de estado se cerró sin integrar, se editó o `main` cambió el estado. Si fue un cierre por error: `restore-state-pr`. Si alguien editó el estado, revisa el diff antes. |
| Issue `fallo: pipeline - en expiry` | La generación servida caduca en menos de 7 días: lanza `refresh`. Si la PC estuvo apagada, el temporizador (`Persistent=true`) lo hace al encender. |
| La PC estuvo apagada más de 30 días | `apt update` dice `is expired` a los usuarios hasta que corra `refresh`. Enciende la PC y lanza `refresh`. |
| Se perdió la PC con el PR de estado abierto | Ese PR tiene el `state.yaml` que corresponde a lo que sirve R2. En la PC nueva, tras restaurar la llave (`docs/operacion-llave.md`) y `just tool-install`: en `packages`, `git checkout debfactory/state`, `debfactory repo rebuild --root . --data /srv/debfactory/data` (reconstruye los `.deb` desde git), y vuelve a `main`. |
| Un runner no arranca | `journalctl -u debfactory-runner@pub -e`. Lo habitual: el PAT caducó (`jitconfig: HTTP 401`) o la imagen no existe (`setup.sh --runner-tag`). |

## 5. Mantenimiento

- **Actualizar el runner.** Cuando `ci doctor` avise (más de 14 días por detrás; GitHub deja de
  aceptarlo a los 30): en `debfactory`, cambia `RUNNER_VERSION` y `RUNNER_SHA256` en
  `ci/runner/runner.env` con los del release de
  [actions/runner](https://github.com/actions/runner/releases), commit, `just runner-image` y
  `sudo ci/host/setup.sh --runner-tag <nueva>`. La versión que comprueba `ci doctor` va
  embebida en el binario: instala también esa versión de la herramienta.
- **Rotar el PAT.** Crea uno nuevo con los mismos permisos, `sudo ci/host/setup.sh --token-stdin`,
  y revoca el anterior en GitHub.
- **Actualizar `actions/checkout`.** Cambia el sha de commit en `pr.yml` y `main.yml` de
  `packages` en un PR revisado.
- **Renombrar `main.yml`** exige cambiar `trusted-gate.sh` y reconstruir la imagen.
- **Rotar la llave de la App.** En la App: *Generate a private key*, cambia el secreto
  `DEBFACTORY_APP_KEY` del environment `watch` y borra la llave anterior en la App.

## 6. Detector de versiones (fase 005)

Revisa una vez al día los releases de cada paquete y propone las versiones estables nuevas como
PR, que valida el check `verify` como cualquier otro (specs/005-upstream-release-watch). No firma
ni publica: corre en el runner `ci`, sin `<data>` ni la llave.

### Instalación (una vez)

1. **Crear la App.** opentepache → Settings → Developer settings → GitHub Apps → *New GitHub
   App*: nombre `opentepache-updates`, sin webhook, permisos de repositorio **Contents: Read and
   write** y **Pull requests: Read and write** (Metadata queda en lectura, obligatorio), y
   *Only on this account*. Después: *Install App* → solo `opentepache/packages`. En la página de
   la App, anota el **Client ID** y genera una **private key** (`.pem`). La llave no va al chat
   ni a ningún repositorio.
2. **Environment y auto-merge en `packages`.** Settings → Environments → *New environment*
   `watch`: *Deployment branches and tags* → *Selected branches and tags* → solo `main`; *Add
   environment secret* `DEBFACTORY_APP_CLIENT_ID` (el Client ID) y `DEBFACTORY_APP_KEY` (el
   contenido del `.pem`). Settings → General → **Allow auto-merge**. Guarda el `.pem` con el
   respaldo de la llave GPG (fuera de la PC) y bórralo de la PC.
3. **Herramienta y workflow.** `just tool-install` con la versión que trae `ci watch` y un PR en
   `packages` que fija esa versión e incluye `.github/workflows/watch.yml` y el bloque `watch:`
   de `config/debfactory.yaml` (los tres juntos: una versión anterior rechaza el bloque `watch`).
4. **Primer ciclo.** Actions → **watch** → *Run workflow* con `force`. Con todo al día, una
   línea `sin-cambios` por paquete y ningún PR.
5. **Timer.** `sudo ci/host/setup.sh` instala las unidades; actívalo cuando `watch.yml` ya esté
   en `main`: `sudo systemctl enable --now debfactory-watch.timer`.
6. **Comprobar.** `GH_TOKEN=$(gh auth token) just ci-doctor`: environment `watch` limitado a
   `main` con sus dos secretos, "Allow auto-merge" si algún paquete lo usa y el timer.

### Qué hace cada ciclo

- Considera solo versiones **estables**: descarta lo marcado como pre-release por upstream y lo
  que tenga en la versión `alpha`, `beta`, `rc`, `pre`, `preview`, `nightly`, `tip`, `canary`,
  `dev` o `snapshot`. `watch.prerelease_flag: ignore` en un manifiesto ignora la marca de
  upstream (si la pone por error), nunca esos marcadores.
- Espera **48 h** desde la publicación upstream (`watch.cooldown`, global) antes de proponer nada.
- Propone la versión más alta: **un PR por paquete** en `debfactory/update/<pkg>`, que cambia
  solo `version`, `revision: 1` y el lockfile (y `license.spdx` si upstream cambió a otra licencia
  permitida). Si sale otra más nueva, actualiza el mismo PR; si `main` cambió, lo regenera.
- **Auto-merge** solo con `watch.auto_merge: true` en el manifiesto y nunca con un cambio de
  versión mayor (4.x → 5.0; en `0.x`, 0.46 → 0.47) ni de licencia: esos esperan a que integres tú.

Ejecutar un ciclo a mano: Actions → **watch** → *Run workflow* (`package` para uno solo; `force`
para no esperar a `watch.interval`). Probarlo en local sin escribir nada:
`GH_TOKEN=$(gh auth token) bin/debfactory ci watch --dry-run --root ../packages`.

### Decisiones del resumen

| Decisión | Qué significa |
|---|---|
| `sin-cambios` | Upstream no tiene ninguna versión estable más nueva |
| `en-espera (elegible …)` | Hay una nueva, pero aún no cumple las 48 h |
| `pr-abierto #n`, `pr-actualizado #n` | PR nuevo o actualizado (con `(auto-merge)` si se integrará solo) |
| `pr-al-día #n` | El PR ya propone la candidata sobre el `main` actual: nada que hacer |
| `pr-cerrado #n` | La versión propuesta dejó de ser candidata (retirada upstream, o `main` ya la tiene) |
| `comentado #n` | Hay una versión más nueva, pero la rama tiene commits tuyos: no la toca |
| `notificado <etapa> #n`, `ya-notificado #n` | Problema notificado (abajo); no se reintenta mientras el issue siga abierto |
| `omitido` | `watch.enabled: false` en el manifiesto |
| `pendiente-límite` | El ciclo se detuvo para no gastar las peticiones de la API; va primero en el siguiente |

### Notificaciones

| Issue | Qué pasó | Qué hacer |
|---|---|---|
| `fallo: <pkg> <v> en watch-license` | La versión nueva tiene una licencia fuera de la allowlist, no identificable, o no se encuentra el texto | Revisa la licencia upstream. Si el paquete sigue siendo empaquetable, corrige el manifiesto (`license`) en un PR: el ciclo siguiente vuelve a intentarlo. Si no, deja el issue abierto o cierra el paquete. |
| `… en watch-assets` | El release no tiene los assets que espera el manifiesto (cambió el nombre o el formato) | Ajusta `artifact.url`/`arch_map` en un PR; el ciclo siguiente propone la versión y cierra el issue. |
| `… en watch-checksums` | Los artefactos no coinciden con los checksums que publica upstream | No integres nada; revisa el release upstream. Se cierra solo cuando aparece una versión que sí cuadra. |
| `… en watch-tampered` | Upstream resubió un asset con la misma etiqueta (otros sha256) | Posible manipulación: el PR no se toca. Investiga antes de nada; ciérralo a mano cuando decidas (cerrar el PR del detector hace que proponga desde cero). |
| `fallo: pipeline - en watch-ratelimit` | Tres ciclos seguidos detenidos por el límite de la API | Sube `watch.rate_reserve` solo si sobra margen, o espera: se cierra solo con un ciclo completo. |
| `fallo: pipeline - en watch` | El propio ciclo falló (API, git, descarga cortada) | Mira el log de la ejecución; el ciclo siguiente reintenta. |

Para que el detector proponga una versión desde cero, cierra su PR (y borra la rama
`debfactory/update/<pkg>` si tiene commits tuyos).
