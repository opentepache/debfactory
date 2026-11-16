#!/bin/sh
# Mueve los datos del repositorio APT del mantenedor a dfpub
# (specs/003-ci-pipeline, research D17). Se ejecuta como root desde el
# checkout, con el runner pub parado:
#
#   sudo systemctl stop debfactory-runner@pub
#   sudo ci/host/migrate-data.sh [--from DIR] [--rclone ARCHIVO]
#
#   --from    datos actuales (por defecto ~$SUDO_USER/.local/share/debfactory)
#   --rclone  configuración de rclone con el remote r2
#             (por defecto ~$SUDO_USER/.config/rclone/rclone.conf)
#
# Copia llavero, almacén, generaciones y registros de sync a
# /srv/debfactory/data y la configuración de rclone a
# /srv/debfactory/rclone/rclone.conf, con dueño dfpub. Comprueba como dfpub que
# `repo verify` pasa allí y solo entonces aparta los originales a
# <from>.migrated-<fecha> (no los borra). El rclone.conf original se queda: lo
# sigue usando el mantenedor para r2test. Nunca imprime el contenido de
# gnupg/ ni de rclone.conf.
set -eu

die() { echo "migrate-data: $*" >&2; exit 1; }
say() { echo "migrate-data: $*"; }

[ "$(id -u)" -eq 0 ] || die "ejecútalo con sudo"
[ -n "${SUDO_USER:-}" ] || die "ejecútalo con sudo desde la cuenta del mantenedor"
home=$(getent passwd "$SUDO_USER" | cut -d: -f6)
from="$home/.local/share/debfactory"
rclone_src="$home/.config/rclone/rclone.conf"
while [ $# -gt 0 ]; do
    case "$1" in
        --from) from=${2:?}; shift 2 ;;
        --rclone) rclone_src=${2:?}; shift 2 ;;
        *) die "opción desconocida $1" ;;
    esac
done

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
data=/srv/debfactory/data
rclone_dst=/srv/debfactory/rclone/rclone.conf

id dfpub >/dev/null 2>&1 || die "no existe dfpub; ejecuta antes sudo ci/host/setup.sh"
if systemctl is-active --quiet debfactory-runner@pub; then
    die "el runner pub está activo; sudo systemctl stop debfactory-runner@pub"
fi
[ -d "$from/gnupg" ] && [ -L "$from/public/current" ] || die "$from no tiene gnupg/ ni public/current"
[ -f "$rclone_src" ] || die "no existe $rclone_src"
if [ -n "$(ls -A "$data" 2>/dev/null)" ]; then
    die "$data no está vacío; revísalo antes de migrar"
fi

say "copiando $from → $data"
cp -a "$from/." "$data/"
rm -f "$data/lock"
chown -R dfpub:dfpub "$data"
chmod 700 "$data" "$data/gnupg"
chmod 600 "$data/gnupg/passphrase"
install -o dfpub -g dfpub -m 600 "$rclone_src" "$rclone_dst"

# Verificación como dfpub, con un binario y una copia del repositorio que
# dfpub pueda leer (el home del mantenedor es 700).
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
chmod 755 "$tmp"
(cd "$repo_root" && git archive HEAD) | tar -x -C "$tmp"
(cd "$repo_root" && GOTOOLCHAIN=local PATH="/usr/lib/go-1.26/bin:$PATH" \
    runuser -u "$SUDO_USER" -- go build -o "$tmp/debfactory.bin" ./cmd/debfactory) 2>/dev/null ||
    install -m 755 "$repo_root/bin/debfactory" "$tmp/debfactory.bin"
chown -R dfpub:dfpub "$tmp"
say "comprobando como dfpub: repo verify y repo status"
if ! runuser -u dfpub -- env HOME=/var/lib/dfpub "$tmp/debfactory.bin" repo verify --root "$tmp" --data "$data"; then
    die "repo verify falla en $data; los originales siguen en $from"
fi
runuser -u dfpub -- env HOME=/var/lib/dfpub RCLONE_CONFIG="$rclone_dst" \
    "$tmp/debfactory.bin" repo status --root "$tmp" --data "$data" || true

stamp=$(date +%Y%m%d-%H%M%S)
mv "$from" "$from.migrated-$stamp"
say "listo. Originales apartados en $from.migrated-$stamp (bórralos cuando el pipeline publique)."
say "A partir de ahora solo el pipeline publica en r2 (un único escritor)."
