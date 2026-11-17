#!/bin/sh
# ExecStart de debfactory-runner@<nivel>.service: corre como df<nivel> y lanza
# el runner en un contenedor desechable con el Docker rootless del usuario
# (specs/003-ci-pipeline, contracts/runner-host.md § Contenedor).
#
# Los directorios se montan en la misma ruta dentro y fuera (Docker-out-of-
# Docker): así los --mount que pasa debfactory a sus contenedores apuntan a
# rutas que existen en el host.
set -eu

level=${1:?uso: run-runner.sh ci|pub}
. /etc/default/debfactory-runner
: "${DEBFACTORY_REPOSITORY:?falta en /etc/default/debfactory-runner}"
: "${DEBFACTORY_RUNNER_ID_CI:?falta en /etc/default/debfactory-runner (setup.sh --runner-tag)}"
: "${DEBFACTORY_RUNNER_ID_PUB:?falta en /etc/default/debfactory-runner (setup.sh --runner-tag)}"
: "${RUNTIME_DIRECTORY:?ejecutar desde la unidad}"

uid=$(id -u)
export XDG_RUNTIME_DIR="/run/user/$uid"
export DOCKER_HOST="unix://$XDG_RUNTIME_DIR/docker.sock"
work="/srv/debfactory/$level/work"
mkdir -p "$work/_work" "$work/tmp"

set -- \
    --rm --name "debfactory-runner-$level" \
    --init \
    --read-only \
    --tmpfs /tmp:rw,exec,size=4g \
    --tmpfs /root:rw,size=1g \
    --security-opt no-new-privileges \
    --cap-drop ALL \
    --mount "type=bind,src=$work,dst=$work" \
    --mount "type=bind,src=$XDG_RUNTIME_DIR/docker.sock,dst=/var/run/docker.sock" \
    --mount "type=bind,src=/proc/sys/fs/binfmt_misc,dst=/run/binfmt_misc,readonly" \
    --mount "type=bind,src=/srv/debfactory/tools,dst=/opt/debfactory/tools,readonly" \
    -e DEBFACTORY_BINFMT_DIR=/run/binfmt_misc \
    -e "DEBFACTORY_REPOSITORY=$DEBFACTORY_REPOSITORY" \
    -e "TMPDIR=$work/tmp" \
    -e "HOME=/root" \
    -e "DEBFACTORY_CACHE=$work/cache"

if [ "$level" = pub ]; then
    if [ "${DEBFACTORY_SPIKE:-0}" = 1 ]; then
        # Spike T021: sin secretos ni datos hasta comprobar el hook.
        set -- "$@" -e TRUSTED_GATE_MODE=log
    else
        set -- "$@" \
            --mount type=bind,src=/srv/debfactory/data,dst=/srv/debfactory/data \
            --mount type=bind,src=/srv/debfactory/rclone,dst=/srv/debfactory/rclone \
            --mount type=bind,src=/srv/debfactory/cache-pub,dst=/srv/debfactory/cache-pub \
            -e DEBFACTORY_DATA=/srv/debfactory/data \
            -e DEBFACTORY_CACHE=/srv/debfactory/cache-pub \
            -e RCLONE_CONFIG=/srv/debfactory/rclone/rclone.conf
    fi
fi

# Por ID de contenido, no por etiqueta: un job no puede sustituir la imagen.
if [ "$level" = pub ]; then image=$DEBFACTORY_RUNNER_ID_PUB; else image=$DEBFACTORY_RUNNER_ID_CI; fi
# La configuración JIT entra por la entrada estándar, no por un montaje: el
# dockerd rootless vive en el espacio de montaje de rootlesskit, cuyo /run es
# una copia (--copy-up=/run) hecha al arrancar el daemon, y no ve el
# RuntimeDirectory que systemd crea después. Tampoco va en una variable de
# entorno, que quedaría en `docker inspect`.
exec docker run -i "$@" "$image" < "$RUNTIME_DIRECTORY/config"
