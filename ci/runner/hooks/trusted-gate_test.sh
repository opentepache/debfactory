#!/bin/sh
# Prueba de trusted-gate.sh (la ejecuta `just ci-lint`).
set -u
hook=$(dirname "$0")/trusted-gate.sh
fails=0
repo=owner/debfactory
ok_wf="$repo/.github/workflows/main.yml@refs/heads/main"

check() { # want name repo event ref workflow_ref
    want=$1 name=$2
    out=$(env -i PATH="$PATH" DEBFACTORY_REPOSITORY="$repo" GITHUB_REPOSITORY="$3" \
        GITHUB_EVENT_NAME="$4" GITHUB_REF="$5" GITHUB_WORKFLOW_REF="$6" sh "$hook" 2>&1)
    got=$?
    if [ "$got" -ne "$want" ]; then
        echo "FALLA: $name: código $got, se esperaba $want ($out)"
        fails=$((fails + 1))
    fi
}

check 0 "push a main" "$repo" push refs/heads/main "$ok_wf"
check 0 "dispatch en main" "$repo" workflow_dispatch refs/heads/main "$ok_wf"
check 1 "pull_request" "$repo" pull_request refs/pull/1/merge "$repo/.github/workflows/main.yml@refs/pull/1/merge"
check 1 "pull_request con ref falsa" "$repo" pull_request refs/heads/main "$ok_wf"
check 1 "schedule" "$repo" schedule refs/heads/main "$ok_wf"
check 1 "otro workflow" "$repo" push refs/heads/main "$repo/.github/workflows/evil.yml@refs/heads/main"
check 1 "otra rama" "$repo" push refs/heads/feature "$repo/.github/workflows/main.yml@refs/heads/feature"
check 1 "otro repositorio" fork/debfactory push refs/heads/main "fork/debfactory/.github/workflows/main.yml@refs/heads/main"
check 1 "repositorio de la herramienta" owner/tool push refs/heads/main "owner/tool/.github/workflows/main.yml@refs/heads/main"
check 1 "variables vacías" "" "" "" ""
out=$(env -i PATH="$PATH" GITHUB_REPOSITORY="$repo" GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main \
    GITHUB_WORKFLOW_REF="$ok_wf" sh "$hook" 2>&1) && { echo "FALLA: sin DEBFACTORY_REPOSITORY"; fails=$((fails + 1)); }
out=$(env -i PATH="$PATH" TRUSTED_GATE_MODE=log DEBFACTORY_REPOSITORY="$repo" GITHUB_REPOSITORY="$repo" \
    GITHUB_EVENT_NAME=pull_request GITHUB_REF=refs/pull/1/merge GITHUB_WORKFLOW_REF=x sh "$hook" 2>&1) ||
    { echo "FALLA: modo log debería dejar pasar"; fails=$((fails + 1)); }

if [ "$fails" -ne 0 ]; then
    echo "trusted-gate_test: $fails fallos"
    exit 1
fi
echo "trusted-gate_test: ok"
