# Operación del pipeline

Cómo se instala, se opera y se recupera el pipeline de construcción y publicación
([specs/003-ci-pipeline](../specs/003-ci-pipeline/spec.md)). Desde que está en marcha es el
**único escritor** del repositorio público: el mantenedor ya no publica, retira ni sincroniza
`r2` a mano. Las órdenes locales siguen sirviendo para probar con datos desechables y con
`r2test`.

## Piezas

| Pieza | Dónde | Qué hace |
|---|---|---|
| `pr.yml` | GitHub, runner `debfactory-pr` (usuario `dfci`) | Check de cada PR: gates 1–6, sin secretos ni publicación |
| `main.yml` | GitHub, runner `debfactory-main` (usuario `dfpub`) | Push a `main`, ejecuciones manuales y refirmado |
| `debfactory-runner@{ci,pub}` | PC, systemd | Un contenedor nuevo por job, con configuración JIT de un solo uso |
| `trusted-gate.sh` | imagen `pub` | Rechaza en el runner confiable todo lo que no sea `main.yml` en `main` |
| `debfactory-refresh.timer` | PC, systemd | Lanza cada semana `refresh` (vuelve a firmar `Valid-Until`) |
| `/srv/debfactory/data` | PC, dueño `dfpub` | Llavero de la subllave, almacén, generaciones y la copia del pipeline (`ci/`) |
| PR de estado | rama `debfactory/state` | `repo/state.yaml` y `repo/audit.jsonl` tras cada publicación; lo integras tú |
| Issues `pipeline-failure` | GitHub | Un issue por paquete, versión y etapa que falla; se cierran solos |

## 1. Instalación (una vez)

Requisitos en la PC: `sudo apt install systemd-container uidmap jq nftables` (Docker CE con
`docker-ce-rootless-extras` ya está instalado).

1. **Repositorio en GitHub.** Es privado, en la organización `opentepache`, y la organización
   necesita **GitHub Team**: en Free los rulesets solo existen en repositorios públicos (research
   D1). Sube `main` y pon su `owner/nombre` en `ci.repository` de `config/debfactory.yaml` en un
   commit.
2. **PAT.** En GitHub → Settings → Developer settings → Fine-grained tokens, crea uno solo para
   este repositorio con `Administration: write` (runners JIT) y `Actions: write` (refresh).
   No lo pegues en ningún chat ni archivo del repositorio.
3. **PC.**

   ```bash
   chmod 700 ~                          # setup.sh lo exige
   just runner-image                    # imprime la etiqueta, p. ej. 2.338.2-aef378b
   sudo ci/host/setup.sh --repository OWNER/debfactory --runner-tag 2.338.2-aef378b --token-stdin
   # pega el PAT cuando lo pida (no se muestra)
   ```

   `setup.sh` es idempotente: crea `dfci` y `dfpub`, su Docker rootless, `/srv/debfactory`, las
   unidades, el temporizador, la regla de nftables y el credstore del PAT, y carga las imágenes
   del runner en el Docker de cada usuario (fijadas por ID de contenido).
4. **GitHub (Settings).**
   - Actions → General → Fork pull request workflows in private repositories: **desmarcado**
     (*Run workflows from fork pull requests*). El código de un fork nunca se ejecuta en la PC.
     Si algún día el repositorio pasa a público, la opción equivalente es **Require approval for
     all external contributors**; `ci doctor` comprueba la que corresponda.
   - Actions → General → Workflow permissions: **Read repository contents** y marca **Allow
     GitHub Actions to create and approve pull requests**.
   - Rules → Rulesets → New branch ruleset sobre la rama por defecto: *Require a pull request
     before merging*, *Block force pushes*, *Restrict deletions*, **sin bypass**.
   - Notifications (tu cuenta) → Actions: correo para ejecuciones fallidas. Cubre los jobs que
     GitHub cancela tras 24 h en cola si la PC está apagada.
5. **Comprobar.** Con un token tuyo de lectura de la administración del repositorio (por ejemplo
   `GH_TOKEN=$(gh auth token)` si usas `gh`, o un fine-grained de solo lectura):

   ```bash
   GH_TOKEN=$(gh auth token) just ci-doctor          # informe
   GH_TOKEN=$(gh auth token) just ci-doctor --fix    # además crea la etiqueta pipeline-failure
   ```

6. **Datos.** Con todo en verde, mueve los datos del repositorio a `dfpub`:

   ```bash
   sudo ci/host/migrate-data.sh
   sudo systemctl enable --now debfactory-runner@ci debfactory-runner@pub
   ```

## 2. Operación diaria

- **Subir un paquete.** En una rama: `$EDITOR packages/<p>.yaml && just lock <p>`, commit, PR.
  Espera el check verde, integra. La ejecución de `main` construye, prueba, publica, sincroniza,
  verifica y abre o actualiza el PR de estado. Los usuarios ya tienen la versión antes de que lo
  integres.
- **PR de estado.** Intégralo sin editarlo cuando quieras. Su merge no publica nada. Si lleva más
  de 7 días abierto, aparece un issue `state-pr-age`.
- **Issues de fallo.** Título `fallo: <paquete> <versión> en <etapa>`, con el motivo y el enlace
  al log. Un fallo repetido añade un comentario; la ejecución que lo resuelve lo cierra.
- **Ejecuciones manuales.** Actions → main → Run workflow:

  | Acción | Entradas | Efecto |
  |---|---|---|
  | `rebuild` | paquete, versión | Construye y prueba desde el commit que la declaró; si está publicada compara los sha256 y no publica |
  | `retire` | paquete, versión, motivo, `last` | Retira, sincroniza, verifica y deja el retiro en la auditoría |
  | `refresh` | — | Vuelve a firmar con `Valid-Until` nuevo, sincroniza y verifica |
  | `restore-state-pr` | — | Vuelve a crear el PR de estado desde la copia del pipeline |

- **PRs de forks.** No ejecutan workflows. Para probar uno, revisa el diff (sobre todo
  `.github/` y `ci/`) y súbelo a una rama tuya; su check corre como `dfci`, sin secretos ni red
  local, y el hook impide que llegue al runner confiable.

## 3. Recuperación

| Situación | Qué hacer |
|---|---|
| Una ejecución murió a mitad (corte, `systemctl kill`) | Relanza (un push vacío o `rebuild` de esa versión). La siguiente completa sync, verify y el PR de estado sin reconstruir (FR-029). |
| Issue `fallo: pipeline - en state` | El PR de estado se cerró sin integrar, se editó o `main` cambió el estado. Si fue un cierre por error: `restore-state-pr`. Si alguien editó el estado, revisa el diff antes. |
| Issue `fallo: pipeline - en expiry` | La generación servida caduca en menos de 7 días: lanza `refresh`. Si la PC estuvo apagada, el temporizador (`Persistent=true`) lo hace al encender. |
| La PC estuvo apagada más de 30 días | `apt update` dice `is expired` a los usuarios hasta que corra `refresh`. Enciende la PC y lanza `refresh`. |
| Se perdió la PC con el PR de estado abierto | Ese PR tiene el `state.yaml` que corresponde a lo que sirve R2. En la PC nueva, tras restaurar la llave (`docs/operacion-llave.md`): `git checkout debfactory/state`, `debfactory repo rebuild --data /srv/debfactory/data` (reconstruye los `.deb` desde git), y vuelve a `main`. |
| Un runner no arranca | `journalctl -u debfactory-runner@pub -e`. Lo habitual: el PAT caducó (`jitconfig: HTTP 401`) o la imagen no existe (`setup.sh --runner-tag`). |

## 4. Mantenimiento

- **Actualizar el runner.** Cuando `ci doctor` avise (más de 14 días por detrás; GitHub deja de
  aceptarlo a los 30), en un PR: cambia `RUNNER_VERSION` y `RUNNER_SHA256` en
  `ci/runner/runner.env` con los del release de
  [actions/runner](https://github.com/actions/runner/releases). Tras integrarlo:
  `just runner-image` y `sudo ci/host/setup.sh --runner-tag <nueva>`.
- **Rotar el PAT.** Crea uno nuevo con los mismos permisos, `sudo ci/host/setup.sh --token-stdin`,
  y revoca el anterior en GitHub.
- **Actualizar `actions/checkout`.** Cambia el sha de commit en `pr.yml` y `main.yml` en un PR
  revisado.
- **Renombrar `main.yml`** exige cambiar `trusted-gate.sh` y reconstruir la imagen.
