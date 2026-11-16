# Research: Pipeline de construcción y publicación

Decisiones de diseño de la fase 003. Cada una indica qué se eligió, por qué y qué se descartó.
Los hechos sobre GitHub se verificaron el 2026-11-13 contra docs.github.com y el repositorio
`actions/runner` (fuentes en cada decisión).

## 1. Decisiones

### D1. Forja: GitHub, repositorio privado en la organización `opentepache` con GitHub Team

> **Enmienda 2026-11-15.** La decisión original era un repositorio público en la cuenta personal.
> El repositorio se creó en la organización `opentepache` y el mantenedor decidió mantenerlo
> **privado** y contratar **GitHub Team**, porque en GitHub Free los rulesets solo existen en
> repositorios públicos (la API responde 403 a un privado). Consecuencias:
>
> - **Forks**: un repositorio privado no tiene "Require approval for all external contributors"
>   (la API responde 422). En su lugar, *Fork pull request workflows in private repositories*
>   queda **desactivado**: el código de un fork nunca se ejecuta en la PC, lo que cumple FR-013
>   con más margen (no hay aprobación que conceder por error). Solo pueden hacer fork quienes
>   tengan acceso de lectura al repositorio.
> - `ci doctor` detecta la visibilidad: en un privado exige que los workflows de forks estén
>   desactivados (sin tokens de escritura ni secretos); en un público, la aprobación de forks.
> - El ruleset de `main` se mantiene igual (Team lo ofrece en privados).
> - Los usuarios finales no ven las recetas ni `audit.jsonl`; la trazabilidad (FR-025, SC-006)
>   se conserva para el mantenedor.

- **Decision**: GitHub, con GitHub Actions y runners propios (*self-hosted*) en la PC.
- **Rationale**:
  - Los 4 upstreams publican en GitHub, y debfactory ya habla con su API (`GITHUB_TOKEN`).
  - Ofrece todo lo que pide la spec sin servicios propios:
    - aprobación de PR de forks;
    - `workflow_dispatch` con parámetros, que solo puede lanzar quien tiene escritura;
    - rulesets para proteger `main` (en repos públicos con GitHub Free; en privados de una
      organización, con GitHub Team);
    - issues;
    - configuración JIT de runners de un solo uso.
  - El principio X ya habla de "pull requests e issues".
- **Alternatives considered**:
  - **GitLab.com**: las merge requests de forks corren en el proyecto del fork salvo
    configuración adicional, y habría que reescribir el principio X.
  - **Codeberg (Forgejo)**: no tiene aprobación por ejecución ni configuración JIT, así que
    habría que construir más piezas propias.
- **Source**: decisión del mantenedor (2026-11-13).

### D2. Dos niveles de confianza: dos usuarios, dos runners, Docker rootless

- **Decision**: se crean dos usuarios de sistema sin privilegios en la PC, cada uno con su
  propio Docker rootless (`dockerd-rootless.sh`, del paquete `docker-ce-rootless-extras` ya
  instalado):

  | Usuario | Runner | Etiqueta | Qué ejecuta | Secretos |
  |---|---|---|---|---|
  | `dfci` | no confiable | `debfactory-pr` | todos los pull requests (forks y ramas propias) | ninguno |
  | `dfpub` | confiable | `debfactory-main` | push a `main`, ejecuciones manuales y refirmado | subllave, frase de paso, credenciales de R2 y datos del repositorio APT |

  Ninguno de los dos usuarios está en el grupo `docker` del sistema, y ninguno puede leer
  `/home/pav0n` (que tiene permisos `700`).
- **Rationale**:
  - **El acceso al Docker del sistema equivale a ser root.** Con Docker rootless, el job solo
    puede hacer lo que podría hacer su usuario.
  - **Separar usuarios aísla los dos niveles de confianza.** Un PR, aunque se apruebe, solo
    puede tocar la caché y las imágenes de `dfci`. Nunca llega a los secretos, a la caché ni
    a las imágenes que usa la publicación.
  - **Todos los builds de `main` corren en el runner confiable** y repiten los gates. Ningún
    artefacto producido por un PR llega a publicarse.
  - **qemu ya está registrado con el flag `F`** (`flags: POF` en
    `/proc/sys/fs/binfmt_misc/qemu-aarch64`). Por eso los contenedores arm64 funcionan también
    con Docker rootless, sin copiar el intérprete.
- **Alternatives considered**:
  - **Una VM efímera por job (KVM)**: es la que más aísla, pero exige mantener imágenes de VM y
    un orquestador, lo que va contra el principio IX. Queda como mejora si algún día se abre la
    aprobación de forks.
  - **Podman rootless con `podman-docker`**: el código está probado contra la CLI de Docker
    (`build --pull --no-cache`, `network create`, `--mount`, `image inspect`), y cambiar de
    motor añade riesgo sin beneficio.
  - **Un solo runner para todo**: un PR aprobado podría dejar algo plantado que después
    ejecutaría la publicación.
- **Source**: https://docs.github.com/en/actions/concepts/runners/runner-groups

### D3. Filtro del runner confiable: hook `job started`

- **Decision**: la imagen del runner confiable fija `ACTIONS_RUNNER_HOOK_JOB_STARTED`, que
  apunta a `ci/runner/hooks/trusted-gate.sh`. El script corre antes del primer paso de cada
  job y lo rechaza (código ≠ 0) salvo que se cumpla todo esto:
  - `GITHUB_REPOSITORY` es el repositorio configurado;
  - `GITHUB_EVENT_NAME` es `push` o `workflow_dispatch` (no hay `schedule`; D11);
  - `GITHUB_REF` es `refs/heads/main`;
  - `GITHUB_WORKFLOW_REF` es `<repo>/.github/workflows/main.yml@refs/heads/main`.

  El script nunca lee el payload del evento (`GITHUB_EVENT_PATH`), porque contiene texto del
  autor del PR.
- **Rationale**: en una cuenta personal no hay runner groups, así que cualquier workflow,
  incluido uno editado por un fork, puede pedir `runs-on: [self-hosted, debfactory-main]`. El
  hook se ejecuta antes de los pasos, un fallo marca el job como fallido sin ejecutar nada, y
  GitHub no permite sobrescribir las variables `GITHUB_*` desde el workflow. Un `pull_request`
  siempre trae `GITHUB_REF=refs/pull/N/merge`, y debfactory no define workflows
  `pull_request_target`.
- **Alternatives considered**:
  - **Una organización con runner groups restringidos a un workflow**: restringir por workflow
    exige el plan Team, de pago.
  - **Un servicio aparte de firma que valide tokens OIDC**: añade un servicio y un protocolo
    propio. El hook, junto con los runners efímeros (D4), da la misma garantía con menos piezas.
- **Sources**:
  - https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/run-scripts
  - https://docs.github.com/en/actions/reference/workflows-and-actions/variables

### D4. Runners efímeros: configuración JIT y un contenedor desechable por job

- **Decision**: hay una unidad systemd del sistema por nivel, `debfactory-runner@ci.service` y
  `debfactory-runner@pub.service`, con `Restart=always`. Cada ciclo hace lo siguiente:
  1. `ExecStartPre=+ci/runner/jitconfig.sh`, que corre como root, lee el token con
     `LoadCredential=` y pide a la API una configuración JIT de un solo uso
     (`POST /repos/{owner}/{repo}/actions/runners/generate-jitconfig`, con la etiqueta del nivel).
     La escribe en el `RuntimeDirectory` de la unidad.
  2. `ExecStart` corre como `dfci` o `dfpub` y lanza `docker run --rm` con la imagen del runner,
     usando el Docker rootless del usuario y `--jitconfig`. El runner atiende un job y termina.
  3. `ExecStopPost` borra el directorio de trabajo. En el nivel `ci` también borra las imágenes y
     la caché de descargas de `dfci`.
- **Credenciales**: el token es un fine-grained PAT con dos permisos sobre este repositorio,
  `Administration: write` (runners) y `Actions: write` (D11). Vive en
  `/etc/credstore/debfactory-gh-runner`, propiedad de root con permisos `600`. Nunca está al
  alcance de `dfci` ni de `dfpub`. Hay que tenerlo aislado, porque cualquiera que pueda usar el
  Docker de un usuario puede leer todo lo que lee ese usuario.
- **Rationale**: cumple FR-012, porque cada job empieza en un contenedor nuevo y nada de un job
  sobrevive al siguiente. Tampoco quedan runners registrados de forma permanente, y el token
  administrativo nunca entra en un contenedor.
- **Alternatives considered**:
  - **Un runner persistente en el host con el directorio de trabajo limpiado a mano**: deja
    residuos (`~/.cache`, `~/.config`, procesos) entre un job y el siguiente.
  - **Un token de registro clásico guardado en el runner**: cualquier job podría reutilizarlo
    para registrar otros runners.
- **Sources**:
  - https://docs.github.com/en/rest/actions/self-hosted-runners
  - https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens

### D5. Imagen del runner propia, sobre `debian:trixie`

- **Decision**: la imagen se define en `ci/runner/Dockerfile`:
  - base `debian:trixie`;
  - paquetes de Debian `golang-1.26-go` (de trixie-backports, igual que en la PC), `git`, `just`,
    `aptly`, `gnupg`, `rclone`, `curl` y `docker-cli`;
  - el tarball de `actions/runner` en una versión fijada y verificada por sha256;
  - el hook de D3 en el nivel `pub`.

  La construye el mantenedor con `just runner-image` a partir de `main`. Ningún PR puede
  cambiar la imagen que está en uso. No incluye `sudo`. El runner corre como root del
  contenedor (`RUNNER_ALLOW_RUNASROOT=1`), y con Docker rootless ese root es el usuario sin
  privilegios `dfci` o `dfpub` del host. Así los archivos de los montajes quedan a nombre de ese
  usuario y no de un subuid ajeno.
- **Montajes**: Docker corre fuera del contenedor del runner (*Docker-out-of-Docker*). Por eso
  el directorio de trabajo (`/srv/debfactory/<nivel>/work`) se monta en la misma ruta dentro y
  fuera, y `TMPDIR` apunta dentro de él. Así siguen funcionando los `--mount` que debfactory
  pasa a lintian, a las completions y a las pruebas de instalación. En el nivel `pub` también se
  monta en la misma ruta `/srv/debfactory/data` (lo que antes era `<data>`) y el `rclone.conf`.
- **Rationale**:
  - La imagen oficial `ghcr.io/actions/actions-runner` es Ubuntu 24.04, con `sudo` sin
    contraseña y sin Go 1.26.
  - Con imagen propia se usan las mismas herramientas de Debian que en la PC (principio IX).
  - El binario upstream del runner nunca corre en el host, solo dentro del contenedor, como
    exige la regla del proyecto.
- **Alternatives considered**:
  - **La imagen oficial más `actions/setup-go`**: descarga Go de upstream en cada job y trae
    `sudo`.
  - **El runner instalado en el host**: va contra la regla "ningún binario upstream en el host"
    y no deja el entorno limpio.
- **Source**: https://github.com/actions/runner/blob/main/images/Dockerfile

### D6. Qué paquetes procesar: conciliar contra el estado, no comparar commits

- **Decision**:
  - **En `main` y en ejecuciones manuales**: `debfactory ci plan` compara los manifiestos de
    `HEAD` con el **estado de referencia**, que es el del PR de estado si hay uno abierto y si
    no el de `main` (D8). Un paquete está afectado si la versión completa de su manifiesto
    (`<upstream>-0~tepache<revision>`) no está publicada. Si esa versión está retirada, es un
    error de la etapa `state`.
  - **En PRs**: los paquetes afectados son los que tienen manifiesto o lockfile cambiado
    respecto de la base (`git diff --name-only base...head`).
    - Si cambia `cmd/`, `internal/`, `config/`, `schema/`, `go.mod`, `go.sum` o `ci/`, se
      construyen además todos los paquetes publicados y se comparan con el sha256 de
      `state.yaml` (FR-010).
    - Un cambio solo en `docs/`, `*.md` o `specs/` no dispara el workflow (`paths-ignore`,
      FR-011).
- **Rationale**:
  - Conciliar contra el estado es idempotente: una ejecución perdida, cancelada o interrumpida
    se recupera sola en la siguiente, sea de `main`, manual o semanal (FR-029). Las ejecuciones
    no dependen del orden en que lleguen.
  - El merge del PR de estado no publica nada (FR-007) sin ninguna regla especial: cuando
    corre su ejecución, todo ya está publicado.
- **Superada**: si al empezar `GITHUB_SHA` ya no es la punta de `origin/main`, la ejecución
  termina en verde marcada como superada, sin construir. La ejecución del commit más nuevo
  concilia todo.
- **Alternatives considered**: comparar solo con el commit anterior (`before..after` del
  push). Una ejecución perdida dejaría la versión sin publicar hasta el siguiente cambio de
  ese paquete.

### D7. Serialización

- **Decision**: tres capas.
  1. Hay un solo runner `debfactory-main`, que atiende un job a la vez.
  2. El workflow `main.yml` declara `concurrency: {group: publish, cancel-in-progress: false,
     queue: max}`, que mantiene hasta 100 ejecuciones en cola sin cancelar ninguna.
  3. `debfactory` toma el `flock` de `<data>/lock` de la fase 002 en toda operación que
     escribe.
- **Rationale**: con la opción por defecto (`queue: single`), cada ejecución nueva en cola
  cancela la que esperaba, y eso contradice FR-018. Aun así, GitHub cancela los jobs que llevan
  más de 24 h en cola (por ejemplo, con la PC apagada), y por eso D6 es la garantía de fondo:
  la siguiente ejecución concilia lo que se perdió.
- **Sources**:
  - https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax
  - https://docs.github.com/en/actions/reference/limits

### D8. PR de estado

- **Decision**:
  - **Rama y PR**: la rama `debfactory/state` siempre es `origin/main` más un único commit que
    escribe `repo/state.yaml` y `repo/audit.jsonl`. El pipeline la regenera y la empuja con
    `--force-with-lease` a la versión que espera encontrar. Abre el PR si no hay uno abierto y,
    si lo hay, lo actualiza.
  - **Copia de referencia**: el pipeline guarda en `/srv/debfactory/data/ci/` la última copia
    de los dos archivos que empujó, con su sha256.
  - **Al empezar una ejecución que escribe**, compara tres cosas: esa copia, la rama remota y
    `main`.

    | Situación | Acción |
    |---|---|
    | La rama remota coincide con la copia | continuar |
    | `main` ya contiene la copia (PR integrado) | continuar y borrar la rama |
    | La rama falta o va por detrás de la copia (el último push falló) | volver a empujarla y continuar (FR-029) |
    | Cualquier otro caso (PR cerrado sin integrar, editado a mano, `main` con otro estado) | etapa `state`: no publicar y notificar (FR-007b) |

  - **Recuperación**: la acción manual `restore-state-pr` vuelve a crear el PR desde la copia.
  - **Recordatorio**: si el PR lleva más de 7 días abierto, se notifica (FR-007c). Se comprueba
    en cada ejecución confiable, incluida la semanal.
  - **Permisos**: el job usa `GITHUB_TOKEN` con `contents: write`, `pull-requests: write` e
    `issues: write`. Un ruleset sobre `main` sin actores con bypass exige PR para cualquier
    cambio, así que ese token puede empujar `debfactory/state` pero no `main` (FR-017a).
  - **Configuración de GitHub**: hace falta activar "Allow GitHub Actions to create and approve
    pull requests", que viene desactivado.
  - **Workflow de PRs**: un PR abierto con `GITHUB_TOKEN` crea ejecuciones de `pull_request`
    que esperan aprobación. Por eso `pr.yml` ignora `repo/state.yaml` y `repo/audit.jsonl`
    (`paths-ignore`), y el PR de estado no necesita aprobación. Si un PR ajeno tocara solo esos
    archivos, la siguiente ejecución confiable lo detectaría como discrepancia.
- **Rationale**: regenerar la rama en lugar de rebasarla evita conflictos, y la copia local
  distingue "voy por delante" (recuperable) de "alguien cambió el estado" (hay que parar).
- **Alternatives considered**:
  - **Push directo a `main`**: lo descartó el mantenedor en la clarificación.
  - **Un PR por publicación**: se acumularían PR que entran en conflicto entre sí.
- **Sources**:
  - https://docs.github.com/en/actions/concepts/security/github_token
  - https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets

### D9. Registro de auditoría: `repo/audit.jsonl`

- **Decision**: una línea JSON por operación, solo añadir, con claves en orden fijo
  ([contracts/audit.md](contracts/audit.md)). El pipeline comprueba que el registro de `main`
  sea un prefijo exacto del nuevo (FR-024) y que todo `.deb` de `state.yaml` tenga una entrada
  `publish` con el mismo sha256 (FR-025). La primera ejecución añade una entrada `import` por
  cada versión publicada antes de esta fase, marcada como hecha a mano por el mantenedor y con
  el commit `9306cdd`.
- **Rationale**: con una línea por entrada, el diff en git es legible y comprobar "solo
  añadir" es una comparación de prefijo. JSON evita las ambigüedades de YAML al concatenar.
- **Alternatives considered**:
  - **Una sección en `state.yaml`**: mezclaría lo que está publicado hoy con la historia, y
    `state.yaml` poda versiones por la retención.
  - **Releases de GitHub**: dejarían el registro fuera de git y atado a la forja.

### D10. Notificaciones: issues con marcador

- **Decision**: debfactory llama a la API REST de GitHub con `net/http` y el `GITHUB_TOKEN`
  del job, solo hacia `api.github.com` y con `User-Agent: debfactory`.
  - **Al fallar**: busca un issue abierto con la etiqueta `pipeline-failure` y el marcador
    `<!-- debfactory:failure package=fnm version=1.40.0-0~tepache1 stage=test -->`. Si lo
    encuentra, lo comenta; si no, lo crea ([contracts/notificaciones.md](contracts/notificaciones.md)).
  - **Al publicar una versión**: cierra los issues abiertos de esa versión con un comentario
    que enlaza la ejecución.
  - **Fallos del pipeline** (la ejecución muere antes del informe): un paso
    `if: failure()` llama a `debfactory ci notify --package pipeline --stage <paso>`.
  - **En PRs** no se crean issues: el resultado va al resumen del job
    (`$GITHUB_STEP_SUMMARY`) y al estado del check (FR-022).
- **Rationale**: no hace falta ningún servicio ni credencial nueva (principio IX), y GitHub ya
  avisa por correo a quien vigila el repositorio.
- **Alternatives considered**:
  - **La CLI `gh`**: es otra dependencia dentro de la imagen y no está en trixie.
  - **Correo SMTP o ntfy**: descartados en la clarificación.

### D11. `Valid-Until`: firma propia y refirmado semanal desde la PC

- **Decision**:
  - **Firma**: aptly 1.6.1 no tiene una opción para `Valid-Until`. La generación (fase 002) pasa
    a publicar con `-skip-signing`. Después debfactory añade `Valid-Until` a cada `Release`
    (fecha de firma + `repo.validity.days`, por defecto 30) y firma con gpg usando la subllave y el
    archivo de frase de paso. Escribe `InRelease` (`--clearsign`) y `Release.gpg`
    (`--detach-sign`). `Release` no se incluye a sí mismo, así que los hashes de los índices y
    el `by-hash` no cambian.
  - **Verificación**: `repo verify` exige `Valid-Until` y avisa cuando quedan menos de
    `repo.validity.warn_days` (por defecto 7; no confundir con `repo.signing.warn_days`, que avisa
    de la caducidad de la subllave), con lo que cumple FR-032.
  - **Refirmado semanal** (`ci refresh`): `repo rebuild` (la misma `serial` y una generación
    nueva) más `sync` y `verify`. `state.yaml` no cambia (FR-031).
  - **Disparo del refirmado**: un temporizador systemd de la PC (`debfactory-refresh.timer`,
    semanal, `Persistent=true`) lanza `workflow_dispatch` con `action=refresh` usando el mismo
    PAT de D4 (`Actions: write`).
- **Rationale**: en repos públicos, GitHub desactiva los workflows con `schedule` tras 60 días
  sin actividad en el repositorio. Con pocos cambios de versión, eso dejaría caducar los
  índices sin aviso. El temporizador con `Persistent=true` se pone al día si la PC estuvo
  apagada.
- **Alternatives considered**:
  - **`on: schedule`**: se desactiva solo, como se explicó arriba.
  - **Un temporizador que ejecute `ci refresh` directamente como `dfpub`**: no dejaría log ni
    notificación en la forja.
  - **Dejar firmar a aptly y añadir el campo después**: rompería la firma.
- **Source**: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows

### D12. Pull requests de forks

- **Decision**: en Settings → Actions se elige "Require approval for all external
  contributors". Cada push a un PR de fork es un evento nuevo de su autor, así que la ejecución
  queda pendiente de aprobación otra vez (FR-013). No hay validación previa a la aprobación
  (FR-013 la permite, pero no la exige): habría que usar `pull_request_target`, con un checkout
  del código del PR dentro de un workflow con permisos de la rama base, que es el error clásico
  de seguridad.
  - **Antes de aprobar**: el mantenedor mira el diff de `.github/` y de `ci/`. Aunque se le
    escape algo, D2 y D3 impiden que el código llegue a secretos o al runner confiable.
  - **Permisos de `pr.yml`**: declara `permissions: contents: read` (FR-017).
- **Source**: https://docs.github.com/en/actions/how-tos/manage-workflow-runs/approve-runs-from-forks

### D13. Secretos y logs

- **Decision**:
  - **Montajes**: los secretos viven en `/srv/debfactory/data/gnupg` (`700`) y en
    `/srv/debfactory/rclone/rclone.conf` (`600`), propiedad de `dfpub`. Solo se montan en el
    contenedor del runner `pub`.
  - **Pasos del workflow**: ningún paso imprime, exporta ni pasa secretos por variables de
    entorno. gpg recibe el archivo de frase de paso y rclone, su configuración.
  - **Filtro de salida**: la salida de procesos externos pasa por `internal/redact` (fase 002).
    Además, `ci` filtra `GITHUB_TOKEN` en los mensajes de error HTTP.
  - **La forja nunca guarda secretos de publicación** (FR-015). El único token de la forja es
    el `GITHUB_TOKEN` efímero de cada job.
- **Rationale**: los logs de un repositorio público son públicos.

### D14. Red del runner no confiable

- **Decision**: una regla de nftables para el usuario `dfci` (`meta skuid dfci`) bloquea el
  tráfico hacia las redes privadas (RFC 1918, link-local y la ULA de IPv6). Con Docker rootless,
  el tráfico de los contenedores sale con el uid del usuario. Internet sigue disponible para
  descargar artefactos y módulos de Go.
- **Rationale**: un PR aprobado no puede atacar el router ni otras máquinas de la red local.
- **Alternatives considered**: sin red. No es viable: la descarga de artefactos (gate 2) la
  necesita.

### D15. Cachés

- **Decision**:
  - **Nivel `ci`**: no hay cachés entre jobs. Después de cada job se borran las imágenes de
    Docker y `~/.cache/debfactory` de `dfci`, y cada PR reconstruye las imágenes de prueba.
  - **Nivel `pub`**: conserva su caché de descargas (direccionada por sha256 y escrita solo
    tras verificar el hash, como en la fase 001) y sus imágenes, que solo construye código de
    `main`.
- **Rationale**: un PR podría envenenar la caché o las etiquetas de imagen de su usuario. Al
  borrarlas, ese envenenamiento no sobrevive al job, y además nunca alcanzó al nivel `pub`.
  Reconstruir las imágenes cuesta unos minutos por PR, algo aceptable con este volumen.

### D16. Código: `internal/ci` e `internal/forge`, sin dependencias nuevas

- **Decision**:
  - **`internal/ci`**: plan, ejecución por nivel, PR de estado, auditoría y refresh.
  - **`internal/forge`**: un cliente REST mínimo de GitHub (issues, PRs y refs) con `net/http`.
  - **Workflows**: se limitan a hacer checkout, compilar y llamar a `debfactory ci <acción>`.
- **Rationale**: la lógica queda en Go, con pruebas unitarias y un servidor `httptest` que
  simula la API, en lugar de estar repartida en YAML. Tampoco se añaden actions de terceros,
  que serían otra cadena de suministro.
- **Actions permitidas**: solo `actions/checkout`, fijada por sha de commit. Todo lo demás son
  órdenes propias.

### D17. Migración de los datos del repositorio a `dfpub`

- **Decision**: `ci/host/migrate-data.sh` copia `~/.local/share/debfactory` (llavero, `store`,
  `public`, `sync`) a `/srv/debfactory/data` con dueño `dfpub` y verifica que `repo verify`
  pase allí. También mueve el `rclone.conf` de R2.
- **Después de migrar**: el mantenedor deja de publicar a mano en `r2`. Las órdenes locales
  siguen sirviendo con datos desechables y con `r2test`.
- **Rationale**: un único escritor (Assumptions de la spec) y un directorio de datos que nunca
  vive bajo el home del mantenedor.

## 2. Configuración inicial de la PC y de GitHub

- **En la PC**, una sola orden: `sudo ci/host/setup.sh`, idempotente
  ([contracts/runner-host.md](contracts/runner-host.md)). Hace esto:
  - crea los usuarios `dfci` y `dfpub`, con `subuid` y `subgid` y `loginctl enable-linger`;
  - instala Docker rootless para cada uno;
  - crea `/srv/debfactory`;
  - instala las unidades systemd y el temporizador;
  - aplica la regla de nftables;
  - deja el token en el credstore, leído desde la entrada estándar y nunca como argumento.
- **En GitHub**, la configuración la comprueba `debfactory ci doctor` con la API:
  - aprobación de forks;
  - permisos por defecto del token en solo lectura;
  - Actions puede crear PRs;
  - un ruleset en `main` sin bypass;
  - la etiqueta `pipeline-failure`.

## 3. Riesgos que se atacan primero

1. **Docker-out-of-Docker con Docker rootless**: hay que comprobar los montajes en la misma
   ruta, `--network none` y qemu arm64 desde el contenedor del runner. Es un spike con
   `debfactory test fnm` dentro de la imagen.
2. **Variables que ve el hook**: un workflow de prueba lanzado con `pull_request` (desde una
   rama propia), `push` y `workflow_dispatch` contra un runner con el hook en modo registro,
   antes de activarlo en modo rechazo.
3. **`Valid-Until` con firma propia**: apt en las 5 suites tiene que aceptarlo sin
   advertencias (con `just e2e`), y rechazar una generación caducada.
4. **Tiempo de SC-001**: una publicación completa de fnm desde la ejecución de `main` tiene que
   tardar menos de 30 min con las cachés de `dfpub` en frío.

### Resultados (2026-11-14, `/speckit-implement`)

- **Riesgo 3, cerrado.** aptly publica con `-skip-signing`; debfactory añade `Valid-Until` a cada
  `Release` y firma `InRelease` y `Release.gpg`. `just e2e` (TestE2ELifecycle) pasa en las 5
  suites: apt acepta la generación sin advertencias y rechaza con `is expired` una generación
  firmada 31 días antes (fecha de firma inyectada; el reloj del host no se toca).
- **Riesgo 1, cerrado (2026-11-16, T019).** Como `dfci`, con su Docker rootless (29.9.3,
  `rootless` + `seccomp`), la imagen `ci` cargada por `setup.sh` y usada por ID, y los montajes y
  restricciones de `run-runner.sh` (`--read-only`, `no-new-privileges`, socket rootless,
  `binfmt_misc` del host): `go build` + `debfactory build fnm` + `debfactory test fnm` pasan en
  las 5 suites y en arm64 en 350 s con cachés vacías (descarga de módulos de Go e imágenes
  base). Los `.deb` son **idénticos byte a byte** a los publicados (sha256 de
  `repo/state.yaml`: amd64 `7c7ff9f6…`, arm64 `ad5a7b73…`). Antes, con el Docker del
  mantenedor (no rootless), ya había pasado en 1 min 52 s con cachés llenas. Ajustes que
  salieron del spike:
  - **DNS**: la regla de nftables cierra a `dfci` la red local, donde está el resolvedor del
    host (aquí el de Tailscale, `100.100.100.100`). Los contenedores usan Quad9 por
    `daemon.json`, y el propio `dockerd` por `ci/host/dockerd-ci-dns.sh` (runner-host.md § Red).
  - **Regla de red endurecida**: con IPv6 global en la red local, los equipos de casa tienen
    direcciones públicas que `fc00::/7` no cubre; se cierra todo IPv6 a `dfci`, además de
    loopback, multicast y broadcast. Prueba desde un contenedor de `dfci`: router (80 y 53), la
    propia PC, el DNS de Tailscale e IPv6 cerrados; `github.com` y `proxy.golang.org` abiertos.
  - **Sin capacidades (T041)**: repetido con `--cap-drop ALL` (`CapEff: 0`) pasa igual, con
    los mismos sha256, en 78 s con cachés llenas. `entrypoint.sh` copia el runner con
    `--no-preserve=ownership`.
- **Riesgos 2 y 4**: pendientes de T021 y T033 (necesitan el repositorio en GitHub; con la
  separación en dos repositorios, el repositorio público de paquetes).
