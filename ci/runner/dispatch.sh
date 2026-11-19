#!/bin/sh
# Lanza workflow_dispatch de un workflow de $DEBFACTORY_REPOSITORY sobre main.
# Lo usan debfactory-refresh.service (main.yml, action=refresh; 003 research
# D11) y debfactory-watch.service (watch.yml; specs/005-upstream-release-watch,
# research D3). El token (PAT del runner, por LoadCredential) entra por stdin a
# curl: nunca en argv ni en el entorno, y solo va a api.github.com.
#
#   dispatch.sh WORKFLOW [INPUTS_JSON]    p. ej. dispatch.sh main.yml '{"action":"refresh"}'
set -eu
workflow=${1:-}
inputs=${2:-'{}'}
case "$workflow" in
    '' | *[!A-Za-z0-9._-]* | .*)
        echo "debfactory-dispatch: uso: dispatch.sh WORKFLOW [INPUTS_JSON] (workflow inválido: «$workflow»)" >&2
        exit 2 ;;
esac
case "$inputs" in
    '{'*'}') ;;
    *) echo "debfactory-dispatch: INPUTS_JSON debe ser un objeto JSON" >&2; exit 2 ;;
esac
: "${DEBFACTORY_REPOSITORY:?falta en /etc/default/debfactory-runner}"
: "${CREDENTIALS_DIRECTORY:?ejecutar desde la unidad (LoadCredential=gh:…)}"
code=$({
    printf 'header = "Authorization: Bearer %s"\n' "$(cat "$CREDENTIALS_DIRECTORY/gh")"
} | curl -sS --config - -o /dev/null -w '%{http_code}' -X POST \
    -A debfactory \
    -H 'Accept: application/vnd.github+json' \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    --data "{\"ref\":\"main\",\"inputs\":$inputs}" \
    "https://api.github.com/repos/$DEBFACTORY_REPOSITORY/actions/workflows/$workflow/dispatches")
if [ "$code" != 204 ]; then
    echo "debfactory-dispatch: HTTP $code al lanzar $workflow en $DEBFACTORY_REPOSITORY" >&2
    exit 1
fi
echo "debfactory-dispatch: $workflow lanzado en $DEBFACTORY_REPOSITORY"
