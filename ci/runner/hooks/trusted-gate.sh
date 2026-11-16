#!/bin/sh
# Filtro previo a cada job del runner confiable (nivel pub;
# specs/003-ci-pipeline, research D3, contracts/runner-host.md § Hook).
#
# El runner lo ejecuta antes del primer paso (ACTIONS_RUNNER_HOOK_JOB_STARTED).
# Si sale con un código distinto de 0, el job falla sin ejecutar ningún paso.
# Solo acepta main.yml sobre refs/heads/main del repositorio configurado, en
# push o workflow_dispatch. Nunca lee GITHUB_EVENT_PATH (texto del autor).
#
# DEBFACTORY_REPOSITORY lo fija run-runner.sh desde /etc/default/debfactory-runner,
# no el workflow. TRUSTED_GATE_MODE=log solo registra (spike T021).
set -u

reject() {
    echo "trusted-gate: rechazado ($1=$2)"
    if [ "${TRUSTED_GATE_MODE:-enforce}" = log ]; then
        echo "trusted-gate: modo log; se deja pasar"
        exit 0
    fi
    exit 1
}

repo=${DEBFACTORY_REPOSITORY:-}
[ -n "$repo" ] || reject DEBFACTORY_REPOSITORY ""

if [ "${TRUSTED_GATE_MODE:-enforce}" = log ]; then
    for v in GITHUB_REPOSITORY GITHUB_EVENT_NAME GITHUB_REF GITHUB_WORKFLOW_REF GITHUB_WORKFLOW GITHUB_ACTOR; do
        eval "val=\${$v:-}"
        echo "trusted-gate: $v=$val"
    done
fi

[ "${GITHUB_REPOSITORY:-}" = "$repo" ] || reject GITHUB_REPOSITORY "${GITHUB_REPOSITORY:-}"
case "${GITHUB_EVENT_NAME:-}" in
    push | workflow_dispatch) ;;
    *) reject GITHUB_EVENT_NAME "${GITHUB_EVENT_NAME:-}" ;;
esac
[ "${GITHUB_REF:-}" = refs/heads/main ] || reject GITHUB_REF "${GITHUB_REF:-}"
[ "${GITHUB_WORKFLOW_REF:-}" = "$repo/.github/workflows/main.yml@refs/heads/main" ] ||
    reject GITHUB_WORKFLOW_REF "${GITHUB_WORKFLOW_REF:-}"

echo "trusted-gate: aceptado ($GITHUB_EVENT_NAME de main.yml en main)"
exit 0
