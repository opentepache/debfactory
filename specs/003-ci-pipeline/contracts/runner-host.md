# Contrato: configuración de la PC para los runners

> **Fase 004** ([cambios-003.md](../../004-split-repos/contracts/cambios-003.md)): `DEBFACTORY_REPOSITORY=opentepache/packages`; el PAT solo tiene acceso a ese repositorio; `/srv/debfactory/tools` y el lanzador (abajo, § Imagen).

Lo instala `sudo ci/host/setup.sh` (idempotente) y lo comprueba `debfactory ci doctor`. La
operación diaria se documenta en `docs/operacion-ci.md`. Diseño en research D2, D4, D5, D11, D13
y D14.

## Usuarios y directorios

| Ruta | Dueño | Permisos | Contenido |
|---|---|---|---|
| `/var/lib/dfci`, `/var/lib/dfpub` | el usuario | `700` | home de cada usuario: Docker rootless (`~/.local/share/docker`) y su sesión de systemd |
| `/srv/debfactory/ci/work` | `dfci` | `700` | trabajo del runner `ci`; se borra tras cada job |
| `/srv/debfactory/pub/work` | `dfpub` | `700` | trabajo del runner `pub`; se borra tras cada job |
| `/srv/debfactory/data` | `dfpub` | `700` | el antiguo `<data>`: `gnupg/`, `store/`, `public/`, `sync/`, `lock` y `ci/` |
| `/srv/debfactory/rclone/rclone.conf` | `dfpub` | `600` | remote `r2`, con `no_check_bucket` y `no_head` |
| `/srv/debfactory/cache-pub` | `dfpub` | `700` | caché de descargas del nivel `pub` (`--cache`) |
| `/etc/credstore/debfactory-gh-runner` | `root` | `600` | fine-grained PAT, solo de este repositorio, con `Administration: write` y `Actions: write` |

Requisitos de los usuarios:

- `dfci` y `dfpub` son usuarios de sistema, sin contraseña, con shell `nologin` y home en
  `/var/lib/df<nivel>` (`adduser --system --group --home /var/lib/df<nivel>`).
- No están en los grupos `docker` ni `sudo`.
- Tienen rangos propios en `/etc/subuid` y `/etc/subgid` y `loginctl enable-linger`.
- Cada uno ejecuta `dockerd-rootless.sh` como servicio de usuario (`docker.service` de
  `systemctl --user`). Se instala con `machinectl shell df<nivel>@
  /usr/bin/dockerd-rootless-setuptool.sh install` y se comprueba con
  `systemctl --user -M df<nivel>@ is-active docker`.
- `/home/pav0n` tiene permisos `700`, y `ci doctor` lo comprueba.

## Unidades systemd

`/etc/systemd/system/debfactory-runner@.service`, instanciada como `@ci` y `@pub`:

```ini
[Unit]
Description=Runner efímero de debfactory (%i)
After=network-online.target user@%U.service

[Service]
User=df%i                      # %i=ci → dfci, %i=pub → dfpub
LoadCredential=gh:/etc/credstore/debfactory-gh-runner
RuntimeDirectory=debfactory-runner-%i
RuntimeDirectoryMode=0700          # dfci no puede ni listar el directorio JIT de pub
ExecStartPre=+/usr/local/lib/debfactory/jitconfig.sh %i     # como root: pide la config JIT y la deja en el RuntimeDirectory, legible solo por el usuario
ExecStart=/usr/local/lib/debfactory/run-runner.sh %i        # docker run --rm (rootless) debfactory-runner:<versión> --jitconfig …
ExecStopPost=+/usr/local/lib/debfactory/cleanup.sh %i       # borra work/; en ci, además imágenes y caché
Restart=always
RestartSec=5
CPUQuota=1400%
MemoryMax=24G
TasksMax=4096
```

Contenedor del runner (`run-runner.sh`):

- `--mount type=bind,src=/srv/debfactory/<nivel>/work,dst=/srv/debfactory/<nivel>/work`, en la
  misma ruta dentro y fuera, con `TMPDIR` dentro de ella (research D5);
- el socket del Docker rootless del usuario en `/var/run/docker.sock`;
- solo en `pub`: `/srv/debfactory/data`, `/srv/debfactory/rclone` y `/srv/debfactory/cache-pub`
  en la misma ruta, más `DEBFACTORY_DATA`, `DEBFACTORY_CACHE` y `RCLONE_CONFIG`;
- `--read-only` con `tmpfs` para `/tmp` y `HOME`. Nada persiste fuera de los montajes.
- `--security-opt no-new-privileges` y `--cap-drop ALL`.

## Hook del runner `pub` (`ci/runner/hooks/trusted-gate.sh`)

Va dentro de la imagen `pub` y se activa con `ACTIONS_RUNNER_HOOK_JOB_STARTED`. Termina con
código 1 y el mensaje `trusted-gate: rechazado (<variable>=<valor>)` salvo que se cumpla todo
esto:

```text
GITHUB_REPOSITORY   == <ci.repository>
GITHUB_EVENT_NAME   ∈ {push, workflow_dispatch}
GITHUB_REF          == refs/heads/main
GITHUB_WORKFLOW_REF == <ci.repository>/.github/workflows/main.yml@refs/heads/main
```

No lee `GITHUB_EVENT_PATH`. La prueba de aceptación es un workflow de `pull_request` que pide
`debfactory-main` y falla en "Set up runner" sin ejecutar ningún paso.

## Temporizador de refirmado

`debfactory-refresh.timer` (`OnCalendar=weekly`, `Persistent=true`, `RandomizedDelaySec=1h`)
lanza `debfactory-refresh.service`. Ese servicio corre como root, con `DynamicUser=yes` y
`LoadCredential=`, y hace un `POST /repos/{owner}/{repo}/actions/workflows/main.yml/dispatches`
con `{"ref":"main","inputs":{"action":"refresh"}}`. El token solo se envía a `api.github.com`.

## Red (`/etc/nftables.d/debfactory-ci.nft`)

```nft
table inet debfactory_ci {
  chain out {
    type filter hook output priority 0; policy accept;
    meta skuid "dfci" ip daddr { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4, 255.255.255.255 } reject
    meta skuid "dfci" meta nfproto ipv6 reject
  }
}
```

El resolvedor del host suele estar en una de esas redes (el router, o `100.100.100.100` con
Tailscale), así que `setup.sh` configura el Docker rootless de `dfci` con DNS públicos (Quad9,
`9.9.9.9` y `149.112.112.112`) en `~dfci/.config/docker/daemon.json` (contenedores) y lanza su `dockerd` con
`ci/host/dockerd-ci-dns.sh` (variable `DOCKERD` de `dockerd-rootless.sh`), que escribe ese
`resolv.conf` en el `/etc` temporal del espacio de nombres de rootlesskit: así las descargas de
imágenes del propio daemon tampoco dependen del resolvedor del host. Lo encontró el spike T019.
No es una barrera de seguridad (`dfci` controla su propio Docker); la barrera es esta regla. IPv6 se cierra entero: con IPv6 global (el caso de la PC), los equipos de la red local tienen
direcciones públicas que ningún rango privado cubre, y GitHub no tiene IPv6. Loopback, multicast y
broadcast se cierran para que `dfci` no alcance servicios de la PC en `localhost` ni el
descubrimiento de la red local. La prueba de aceptación consiste en que, desde un job `ci`,
`curl http://192.168.1.1`, `curl http://127.0.0.1` y cualquier destino IPv6 fallen y `curl
https://github.com` funcione.

## Imagen (`ci/runner/Dockerfile`)

- **Base**: `debian:trixie`, fijada por digest.
- **Paquetes de trixie**: `git`, `just`, `aptly`, `gnupg`,
  `gpgv`, `rclone`, `curl`, `jq`, `ca-certificates`, `docker-cli` y `docker-buildx`, más las
  bibliotecas que pide el runner (`libicu76`, `libkrb5-3`, `zlib1g`, `liblttng-ust1t64`).
- **Runner**: `actions-runner-linux-x64-<versión>.tar.gz` de `github.com/actions/runner`, con
  versión y sha256 fijados en `ci/runner/runner.env` y verificados al construir. Como el
  contenedor es `--read-only`, `entrypoint.sh` copia el runner a `/tmp` (tmpfs) y lo arranca con
  `run.sh --jitconfig`; nunca ejecuta `config.sh`. El runner no debe actualizarse solo; si la
  configuración JIT lo impide se verifica en el spike T021. Actualizarlo es un cambio revisado en
  git, antes de 30 días desde un release nuevo (`ci doctor` avisa a los 14).
- **Usuario**: root del contenedor, con `RUNNER_ALLOW_RUNASROOT=1` y sin `sudo`. Con Docker
  rootless, ese root es `dfci` o `dfpub` en el host, así que los archivos de los montajes quedan
  a nombre del usuario del nivel (research D5).
- **Hook**: el de arriba, solo en la variante `pub` (`--target pub`).
- **Lanzador** (fase 004): `/usr/local/bin/debfactory` ejecuta
  `/opt/debfactory/tools/<commit>/debfactory` según `debfactory.version` del checkout. El
  directorio es `/srv/debfactory/tools` del host (root, `755`), montado `readonly` en los dos
  niveles. Sin Go en la imagen: los jobs no compilan.

`just runner-image` construye las dos variantes en el Docker del mantenedor y las etiqueta con la
versión del runner y el commit. `setup.sh --runner-tag <etiqueta>` las carga en el Docker
rootless de `dfci` y `dfpub` y guarda su **ID de contenido** en `/etc/default/debfactory-runner`
(`DEBFACTORY_RUNNER_ID_CI` y `DEBFACTORY_RUNNER_ID_PUB`). `run-runner.sh` arranca la imagen por
ese ID, así que un job no puede sustituirla retagueando otra, y `cleanup.sh` del nivel `ci` borra
todas las imágenes de `dfci` salvo esa.

## Diferencias de la implementación

- **binfmt**: dentro del contenedor `/proc/sys/fs/binfmt_misc` no está montado. `run-runner.sh`
  monta el del host en `/run/binfmt_misc` (solo lectura) y fija `DEBFACTORY_BINFMT_DIR`, que usa
  la comprobación previa de Docker y qemu.
- **Refirmado**: el temporizador ejecuta `/usr/local/lib/debfactory/dispatch-refresh.sh`
  (`curl` con el token por la entrada estándar).
- **Unidad**: además de lo de arriba, `NoNewPrivileges=yes`, `ProtectSystem=strict`,
  `ProtectHome=read-only` y `PrivateTmp=yes` para los procesos del host de la unidad.
- **Contenedor**: `--init`, `--read-only`, `--tmpfs /tmp` y `/root`,
  `--security-opt no-new-privileges` y `--cap-drop ALL`: el runner no necesita ninguna
  capacidad (spike T019 repetido con `CapEff: 0`, 2026-11-16). Por eso `entrypoint.sh` copia el
  runner sin conservar el dueño (no hay `CAP_CHOWN`). Los contenedores que lanza debfactory
  (build, lintian, pruebas) no heredan estas restricciones: los crea el daemon rootless.
- **Serialización**: además del `flock` de `<data>/lock` (publish, retire, sync), `debfactory ci`
  toma `<data>/ci/lock` durante toda la ejecución que escribe.
