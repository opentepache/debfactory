#!/bin/sh
# Lanzador de debfactory en la imagen del runner (/usr/local/bin/debfactory).
# Ejecuta la versión que fija ./debfactory.version del checkout de packages,
# instalada por el mantenedor con `just tool-install` en
# /srv/debfactory/tools/<commit>/ y montada aquí de solo lectura
# (specs/004-split-repos, contracts/herramienta.md § Lanzador).
#
# No lee nada más del checkout ni del entorno, salvo DEBFACTORY_TOOLS, que
# solo existe para las pruebas.
set -eu
tools=${DEBFACTORY_TOOLS:-/opt/debfactory/tools}

die() { echo "debfactory: $*" >&2; exit 2; }

[ -f debfactory.version ] || die "debfactory.version falta o no es un commit (40 hex)"
pin=$(head -n 1 debfactory.version)
[ "$(wc -l < debfactory.version)" -le 1 ] || die "debfactory.version falta o no es un commit (40 hex)"
case "$pin" in
    *[!0-9a-f]* | "") die "debfactory.version falta o no es un commit (40 hex)" ;;
esac
[ "${#pin}" -eq 40 ] || die "debfactory.version falta o no es un commit (40 hex)"

bin="$tools/$pin/debfactory"
if [ ! -x "$bin" ]; then
    installed=$(ls "$tools" 2>/dev/null | tr '\n' ' ')
    echo "debfactory: la versión $pin no está instalada en el runner" >&2
    echo "instaladas: ${installed:-ninguna}" >&2
    echo "para instalarla: en el checkout de debfactory, git checkout $pin && just tool-install" >&2
    exit 2
fi
exec "$bin" "$@"
