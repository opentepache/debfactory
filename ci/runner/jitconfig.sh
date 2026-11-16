#!/bin/sh
# ExecStartPre=+ de debfactory-runner@<nivel>.service: corre como root, lee el
# PAT de la credencial de systemd y pide a GitHub una configuración JIT de un
# solo uso para un runner efímero (specs/003-ci-pipeline, research D4).
#
# El PAT nunca va en la línea de órdenes ni en la salida: curl lo lee de su
# configuración por la entrada estándar. La configuración JIT queda en
# $RUNTIME_DIRECTORY/config, legible solo por el usuario del nivel.
set -eu

level=${1:?uso: jitconfig.sh ci|pub}
case "$level" in
    ci) label=debfactory-pr ;;
    pub) label=debfactory-main ;;
    *) echo "jitconfig: nivel desconocido $level" >&2; exit 2 ;;
esac
. /etc/default/debfactory-runner
: "${DEBFACTORY_REPOSITORY:?falta en /etc/default/debfactory-runner}"
: "${CREDENTIALS_DIRECTORY:?ejecutar desde la unidad (LoadCredential=gh)}"
: "${RUNTIME_DIRECTORY:?ejecutar desde la unidad (RuntimeDirectory=)}"

user=df$level
id=$(cut -c1-8 /proc/sys/kernel/random/uuid)
body=$(jq -cn --arg name "debfactory-$level-$id" --arg label "$label" \
    --arg work "/srv/debfactory/$level/work/_work" \
    '{name: $name, runner_group_id: 1, labels: ["self-hosted", $label], work_folder: $work}')

out="$RUNTIME_DIRECTORY/config"
tmp="$RUNTIME_DIRECTORY/response.json"
umask 077
rm -f "$out" "$tmp"
# El token entra por stdin (--config -); nunca aparece en ps ni en el journal.
code=$({
    printf 'header = "Authorization: Bearer %s"\n' "$(cat "$CREDENTIALS_DIRECTORY/gh")"
} | curl -sS --config - -o "$tmp" -w '%{http_code}' -X POST \
    -A debfactory \
    -H 'Accept: application/vnd.github+json' \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    --data "$body" \
    "https://api.github.com/repos/$DEBFACTORY_REPOSITORY/actions/runners/generate-jitconfig")
if [ "$code" != 201 ]; then
    msg=$(jq -r '.message // "sin mensaje"' "$tmp" 2>/dev/null || echo "respuesta ilegible")
    rm -f "$tmp"
    echo "jitconfig: HTTP $code al pedir la configuración JIT: $msg" >&2
    exit 1
fi
jq -er '.encoded_jit_config' "$tmp" > "$out"
rm -f "$tmp"
chown "$user:$user" "$out"
chmod 600 "$out"
echo "jitconfig: runner debfactory-$level-$id ($label) listo"
