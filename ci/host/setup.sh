#!/bin/sh
# Prepara la PC para los runners de debfactory (specs/003-ci-pipeline,
# contracts/runner-host.md). Idempotente. Se ejecuta como root:
#
#   sudo ci/host/setup.sh [--repository OWNER/NOMBRE] [--runner-tag TAG] [--token-stdin]
#
#   --repository   repositorio de GitHub (se guarda en /etc/default/debfactory-runner)
#   --runner-tag   etiqueta que imprimió `just runner-image`: carga las dos imágenes
#                  (TAG-ci y TAG-pub) del Docker del mantenedor en el Docker
#                  rootless de dfci y dfpub, y fija su ID de contenido
#   --token-stdin  lee el fine-grained PAT de la entrada estándar (sin eco) y lo
#                  guarda en /etc/credstore/debfactory-gh-runner (root, 600).
#                  Nunca se pasa como argumento ni se imprime.
#
# No arranca los runners: eso lo hace el mantenedor tras `just runner-image`
# (systemctl enable --now debfactory-runner@ci debfactory-runner@pub).
set -eu

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
libdir=/usr/local/lib/debfactory
defaults=/etc/default/debfactory-runner
credstore=/etc/credstore/debfactory-gh-runner

repository= tag= token_stdin=0
while [ $# -gt 0 ]; do
    case "$1" in
        --repository) repository=${2:?}; shift 2 ;;
        --runner-tag) tag=${2:?}; shift 2 ;;
        --token-stdin) token_stdin=1; shift ;;
        *) echo "setup: opción desconocida $1" >&2; exit 2 ;;
    esac
done

say() { echo "setup: $*"; }
die() { echo "setup: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "ejecútalo con sudo"

# 1. Requisitos (paquetes de Debian; docker-ce-rootless-extras ya está instalado).
missing=
for c in dockerd-rootless-setuptool.sh:docker-ce-rootless-extras newuidmap:uidmap \
    machinectl:systemd-container jq:jq curl:curl nft:nftables runuser:util-linux; do
    command -v "${c%%:*}" >/dev/null 2>&1 || missing="$missing ${c#*:}"
done
[ -z "$missing" ] || die "faltan paquetes:$missing (sudo apt install$missing)"

# 2. El home del mantenedor no debe ser legible por otros usuarios (FR-012).
if [ -n "${SUDO_USER:-}" ]; then
    home=$(getent passwd "$SUDO_USER" | cut -d: -f6)
    perm=$(stat -c %a "$home")
    [ "$perm" = 700 ] || die "$home tiene permisos $perm; ejecuta 'chmod 700 $home' y repite"
fi

# 3. Usuarios de sistema, sin contraseña ni shell, con home propio.
start=2100000000
for level in ci pub; do
    user=df$level
    if ! id "$user" >/dev/null 2>&1; then
        adduser --system --group --home "/var/lib/$user" --shell /usr/sbin/nologin "$user" >/dev/null
        say "usuario $user creado"
    fi
    chmod 700 "/var/lib/$user"
    for g in docker sudo; do
        if id -nG "$user" | tr ' ' '\n' | grep -qx "$g"; then
            die "$user está en el grupo $g; quítalo (gpasswd -d $user $g)"
        fi
    done
    # Rangos propios de subuid/subgid para Docker rootless.
    if ! grep -q "^$user:" /etc/subuid; then
        usermod --add-subuids "$start-$((start + 65535))" --add-subgids "$start-$((start + 65535))" "$user"
    fi
    start=$((start + 65536))
    loginctl enable-linger "$user"
done

# 4. Docker rootless de cada usuario (necesita su sesión de systemd).
for level in ci pub; do
    user=df$level
    if ! systemctl --user -M "$user@" is-active --quiet docker 2>/dev/null; then
        say "instalando Docker rootless para $user"
        machinectl shell "$user@" /usr/bin/dockerd-rootless-setuptool.sh install >/dev/null
        systemctl --user -M "$user@" enable --now docker >/dev/null
    fi
    systemctl --user -M "$user@" is-active --quiet docker || die "docker de $user no está activo"
done

# 4b. DNS de dfci. La regla de nftables le cierra la red local, que es donde
# suele estar el resolvedor del host (el router, o 100.100.100.100 con
# Tailscale), así que su Docker usa resolvedores públicos (Quad9): los
# contenedores por daemon.json y el propio dockerd (descargas de imágenes) por
# dockerd-ci-dns.sh, que la unidad lanza en lugar de dockerd.
install -d -m 755 "$libdir"
install -m 755 "$here/dockerd-ci-dns.sh" "$libdir/dockerd-ci-dns.sh"
cfg=/var/lib/dfci/.config
want_json='{"dns":["9.9.9.9","149.112.112.112"]}'
want_unit=$(printf '[Service]\nEnvironment=DOCKERD=%s\n' "$libdir/dockerd-ci-dns.sh")
dropin=$cfg/systemd/user/docker.service.d/debfactory-dns.conf
if [ "$(cat "$cfg/docker/daemon.json" 2>/dev/null)" != "$want_json" ] ||
    [ "$(cat "$dropin" 2>/dev/null)" != "$want_unit" ]; then
    install -d -o dfci -g dfci -m 700 "$cfg"
    install -d -o dfci -g dfci -m 755 "$cfg/docker" "$cfg/systemd" "$cfg/systemd/user" "$(dirname "$dropin")"
    printf '%s\n' "$want_json" > "$cfg/docker/daemon.json"
    printf '%s\n' "$want_unit" > "$dropin"
    chown dfci:dfci "$cfg/docker/daemon.json" "$dropin"
    chmod 644 "$cfg/docker/daemon.json" "$dropin"
    systemctl --user -M dfci@ daemon-reload
    systemctl --user -M dfci@ restart docker
    systemctl --user -M dfci@ is-active --quiet docker || die "docker de dfci no arranca; journalctl --user -M dfci@ -u docker"
    say "DNS del Docker de dfci (contenedores y daemon): 9.9.9.9 y 149.112.112.112"
fi

# 5. Directorios (contracts/runner-host.md § Usuarios y directorios).
install -d -m 755 -o root -g root /srv/debfactory
install -d -m 755 -o dfci -g dfci /srv/debfactory/ci
install -d -m 700 -o dfci -g dfci /srv/debfactory/ci/work
install -d -m 755 -o dfpub -g dfpub /srv/debfactory/pub
install -d -m 700 -o dfpub -g dfpub /srv/debfactory/pub/work
install -d -m 700 -o dfpub -g dfpub /srv/debfactory/data /srv/debfactory/rclone /srv/debfactory/cache-pub

# 6. Scripts y unidades.
install -d -m 755 "$libdir"
for f in jitconfig.sh run-runner.sh cleanup.sh dispatch-refresh.sh; do
    install -m 755 "$repo_root/ci/runner/$f" "$libdir/$f"
done
for f in debfactory-runner@.service debfactory-refresh.service debfactory-refresh.timer; do
    install -m 644 "$here/$f" "/etc/systemd/system/$f"
done

# 7. Red del runner no confiable (research D14).
install -d -m 755 /etc/nftables.d
install -m 644 "$here/debfactory-ci.nft" /etc/nftables.d/debfactory-ci.nft
if ! grep -qs 'include "/etc/nftables.d/\*.nft"' /etc/nftables.conf; then
    printf '\ninclude "/etc/nftables.d/*.nft"\n' >> /etc/nftables.conf
fi
nft list table inet debfactory_ci >/dev/null 2>&1 && nft delete table inet debfactory_ci
nft -f /etc/nftables.d/debfactory-ci.nft
systemctl enable nftables >/dev/null 2>&1 || true

# 8. Credencial (PAT) por la entrada estándar.
if [ "$token_stdin" = 1 ]; then
    install -d -m 700 /etc/credstore
    if [ -t 0 ]; then
        printf 'PAT (no se mostrará): ' >&2
        stty -echo
        trap 'stty echo' EXIT
    fi
    IFS= read -r pat
    [ -t 0 ] && { stty echo; trap - EXIT; echo >&2; }
    case "$pat" in
        github_pat_*) ;;
        *) die "el token no parece un fine-grained PAT (github_pat_…)" ;;
    esac
    umask 077
    printf '%s' "$pat" > "$credstore.tmp"
    unset pat
    chown root:root "$credstore.tmp"
    chmod 600 "$credstore.tmp"
    mv "$credstore.tmp" "$credstore"
    say "PAT guardado en $credstore (root, 600)"
fi

# 9. Imágenes del runner en el Docker rootless de cada usuario. Se usan por ID
# de contenido: un job no puede sustituirlas retagueando otra imagen.
old_repo= old_tag= id_ci= id_pub=
if [ -f "$defaults" ]; then
    old_repo=$(sed -n 's/^DEBFACTORY_REPOSITORY=//p' "$defaults")
    old_tag=$(sed -n 's/^DEBFACTORY_RUNNER_TAG=//p' "$defaults")
    id_ci=$(sed -n 's/^DEBFACTORY_RUNNER_ID_CI=//p' "$defaults")
    id_pub=$(sed -n 's/^DEBFACTORY_RUNNER_ID_PUB=//p' "$defaults")
fi
if [ -n "$tag" ]; then
    for level in ci pub; do
        user=df$level
        uid=$(id -u "$user")
        img="debfactory-runner:$tag-$level"
        docker image inspect "$img" >/dev/null 2>&1 || die "no existe $img en tu Docker (just runner-image)"
        docker save "$img" | runuser -u "$user" -- env XDG_RUNTIME_DIR="/run/user/$uid" \
            DOCKER_HOST="unix:///run/user/$uid/docker.sock" docker load >/dev/null
        id=$(runuser -u "$user" -- env XDG_RUNTIME_DIR="/run/user/$uid" \
            DOCKER_HOST="unix:///run/user/$uid/docker.sock" docker image inspect --format '{{.Id}}' "$img")
        eval "id_$level=\$id"
        say "imagen $img cargada para $user ($id)"
    done
fi

# 10. /etc/default/debfactory-runner: conserva lo que no se pasa.
repository=${repository:-$old_repo}
tag=${tag:-$old_tag}
case "$repository" in
    "" | */*) ;;
    *) die "--repository debe ser OWNER/NOMBRE" ;;
esac
cat > "$defaults.tmp" <<EOT
# Generado por ci/host/setup.sh. Sin secretos.
DEBFACTORY_REPOSITORY=$repository
DEBFACTORY_RUNNER_TAG=$tag
DEBFACTORY_RUNNER_ID_CI=$id_ci
DEBFACTORY_RUNNER_ID_PUB=$id_pub
EOT
chmod 644 "$defaults.tmp"
mv "$defaults.tmp" "$defaults"

systemctl daemon-reload
if [ -n "$repository" ] && [ -s "$credstore" ]; then
    systemctl enable --now debfactory-refresh.timer >/dev/null
    say "temporizador de refirmado activo"
fi

say "listo."
[ -n "$repository" ] || say "falta --repository OWNER/NOMBRE"
[ -s "$credstore" ] || say "falta el PAT: sudo ci/host/setup.sh --token-stdin"
[ -n "$tag" ] || say "falta --runner-tag (just runner-image)"
say "para arrancar: sudo systemctl enable --now debfactory-runner@ci debfactory-runner@pub"
