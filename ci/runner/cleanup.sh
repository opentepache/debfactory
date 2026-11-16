#!/bin/sh
# ExecStopPost=+ de debfactory-runner@<nivel>.service (como root): borra el
# trabajo del job y la configuración JIT. En el nivel ci borra además las
# imágenes, volúmenes y la caché de dfci, para que nada de un PR sobreviva al
# job (research D15). Nunca toca /srv/debfactory/data.
set -u

level=${1:?uso: cleanup.sh ci|pub}
case "$level" in ci | pub) ;; *) exit 2 ;; esac
work="/srv/debfactory/$level/work"
user=df$level

# El contenedor puede seguir vivo si la unidad se mató a mitad.
uid=$(id -u "$user")
runuser -u "$user" -- env XDG_RUNTIME_DIR="/run/user/$uid" DOCKER_HOST="unix:///run/user/$uid/docker.sock" \
    docker rm -f "debfactory-runner-$level" >/dev/null 2>&1 || true

if [ -d "$work" ]; then
    find "$work" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
fi
rm -f "${RUNTIME_DIRECTORY:-/run/debfactory-runner-$level}/config"

if [ "$level" = ci ]; then
    # Todo lo que dejó el job, salvo la imagen del runner (por ID).
    keep=$(sed -n 's/^DEBFACTORY_RUNNER_ID_CI=//p' /etc/default/debfactory-runner)
    d() { runuser -u "$user" -- env XDG_RUNTIME_DIR="/run/user/$uid" DOCKER_HOST="unix:///run/user/$uid/docker.sock" docker "$@"; }
    d container prune -f >/dev/null 2>&1 || true
    for img in $(d image ls -aq --no-trunc 2>/dev/null | sort -u); do
        [ "$img" = "$keep" ] || d rmi -f "$img" >/dev/null 2>&1 || true
    done
    d volume prune -af >/dev/null 2>&1 || true
    d network prune -f >/dev/null 2>&1 || true
    d builder prune -af >/dev/null 2>&1 || true
    home=$(getent passwd "$user" | cut -d: -f6)
    [ -n "$home" ] && rm -rf "$home/.cache/debfactory"
fi
exit 0
