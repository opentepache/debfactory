#!/bin/sh
# debfactory-refresh.service: POST .../actions/workflows/main.yml/dispatches con
# action=refresh. El token entra por stdin a curl; solo va a api.github.com.
set -eu
: "${DEBFACTORY_REPOSITORY:?falta en /etc/default/debfactory-runner}"
: "${CREDENTIALS_DIRECTORY:?ejecutar desde la unidad}"
code=$({
    printf 'header = "Authorization: Bearer %s"\n' "$(cat "$CREDENTIALS_DIRECTORY/gh")"
} | curl -sS --config - -o /dev/null -w '%{http_code}' -X POST \
    -A debfactory \
    -H 'Accept: application/vnd.github+json' \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    --data '{"ref":"main","inputs":{"action":"refresh"}}' \
    "https://api.github.com/repos/$DEBFACTORY_REPOSITORY/actions/workflows/main.yml/dispatches")
if [ "$code" != 204 ]; then
    echo "debfactory-refresh: HTTP $code al lanzar refresh" >&2
    exit 1
fi
echo "debfactory-refresh: refresh lanzado en $DEBFACTORY_REPOSITORY"
