#!/bin/sh
# Arranca el runner con la configuración JIT de un solo uso que deja
# jitconfig.sh y run-runner.sh pasa por la entrada estándar. Nunca ejecuta
# config.sh: no hay token de registro dentro del contenedor (research D4).
set -eu
jit=$(head -c 65536)
if [ -z "$jit" ]; then
    echo "debfactory-runner: falta la configuración JIT en la entrada estándar" >&2
    exit 3
fi
# El contenedor es de solo lectura (--read-only) y el runner escribe en su
# propio directorio (.runner, _diag): se copia a /tmp, que es tmpfs. Sin
# conservar el dueño: con --cap-drop ALL no hay CAP_CHOWN.
cp -a --no-preserve=ownership /opt/runner /tmp/runner
exec /tmp/runner/run.sh --jitconfig "$jit"
