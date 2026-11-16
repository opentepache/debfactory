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
     before merging*, *Block force pushes*, *Restrict deletions*, **sin bypass**.
   - Notifications (tu cuenta) → Actions: correo para ejecuciones fallidas. Cubre los jobs que
     GitHub cancela tras 24 h en cola si la PC está apagada.
6. **Comprobar.** Con un token tuyo de lectura de la administración del repositorio:

   ```bash
   GH_TOKEN=$(gh auth token) just ci-doctor          # informe
   GH_TOKEN=$(gh auth token) just ci-doctor --fix    # además crea la etiqueta pipeline-failure
   ```

7. **Datos.** Con todo en verde, mueve los datos del repositorio a `dfpub`:

   ```bash
   sudo ci/host/migrate-data.sh --root ~/Develop/packages
   sudo systemctl enable --now debfactory-runner@ci debfactory-runner@pub
   ```

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
