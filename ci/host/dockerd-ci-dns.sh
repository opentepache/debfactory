#!/bin/sh
# dockerd de dfci con DNS públicos (specs/003-ci-pipeline, contracts/runner-host.md § Red).
#
# La regla de nftables cierra a dfci la red local, donde suele estar el
# resolvedor del host (el router, o 100.100.100.100 con Tailscale). Los
# contenedores usan Quad9 por daemon.json, pero el propio dockerd (descargas de
# imágenes, BuildKit) lee /etc/resolv.conf. dockerd-rootless.sh lo lanza dentro
# del espacio de nombres de montaje de rootlesskit, donde /etc es un tmpfs
# (--copy-up=/etc): sustituir resolv.conf aquí solo afecta a este dockerd, no
# al host. La unidad docker.service de dfci lo usa con DOCKERD=<este script>.
#
# Esto no es una barrera de seguridad (dfci puede cambiar su propio Docker);
# la barrera es la regla de nftables, que es de root.
set -eu
[ "$(stat -c %T -f /etc)" = tmpfs ] || {
    echo "dockerd-ci-dns: /etc no es el tmpfs de rootlesskit; no toco resolv.conf" >&2
    exit 1
}
rm -f /etc/resolv.conf
printf 'nameserver 9.9.9.9\nnameserver 149.112.112.112\n' > /etc/resolv.conf
exec dockerd "$@"
