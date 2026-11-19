#!/bin/sh
# Prueba de dispatch.sh (la ejecuta `just ci-lint`): un curl falso registra la
# petición; el token nunca aparece en argv.
set -u
dispatch=$(cd "$(dirname "$0")" && pwd)/dispatch.sh
fails=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/cred"
printf 'ghp_SECRETO_de_prueba' > "$tmp/cred/gh"
cat > "$tmp/bin/curl" <<'EOT'
#!/bin/sh
printf '%s\n' "$@" > "$CURL_ARGS"
cat > "$CURL_STDIN"
printf '%s' "${CURL_CODE:-204}"
EOT
chmod 755 "$tmp/bin/curl"

run() {
    PATH="$tmp/bin:$PATH" CURL_ARGS="$tmp/args" CURL_STDIN="$tmp/stdin" \
        DEBFACTORY_REPOSITORY=opentepache/packages CREDENTIALS_DIRECTORY="$tmp/cred" sh "$dispatch" "$@" 2>&1
}
expect() { # nombre código-esperado texto-esperado código-obtenido salida
    if [ "$4" -ne "$2" ] || ! printf '%s' "$5" | grep -qF -- "$3"; then
        echo "FALLA: $1: código $4, salida «$5»; se esperaba $2 y «$3»"
        fails=$((fails + 1))
    fi
}

out=$(run main.yml '{"action":"refresh"}'); expect "refresh" 0 "main.yml lanzado en opentepache/packages" $? "$out"
grep -qxF 'https://api.github.com/repos/opentepache/packages/actions/workflows/main.yml/dispatches' "$tmp/args" ||
    { echo "FALLA: URL de refresh"; fails=$((fails + 1)); }
grep -qxF '{"ref":"main","inputs":{"action":"refresh"}}' "$tmp/args" ||
    { echo "FALLA: cuerpo de refresh"; fails=$((fails + 1)); }
if grep -q SECRETO "$tmp/args"; then echo "FALLA: el token está en argv"; fails=$((fails + 1)); fi
grep -q 'Authorization: Bearer ghp_SECRETO_de_prueba' "$tmp/stdin" ||
    { echo "FALLA: el token no llega por stdin"; fails=$((fails + 1)); }

out=$(run watch.yml); expect "watch sin entradas" 0 "watch.yml lanzado" $? "$out"
grep -qxF '{"ref":"main","inputs":{}}' "$tmp/args" || { echo "FALLA: cuerpo de watch"; fails=$((fails + 1)); }

out=$(CURL_CODE=404 run watch.yml); expect "HTTP 404" 1 "HTTP 404 al lanzar watch.yml" $? "$out"
out=$(run); expect "sin workflow" 2 "uso: dispatch.sh" $? "$out"
out=$(run '../x.yml'); expect "workflow con ruta" 2 "workflow inválido" $? "$out"
out=$(run main.yml 'action=refresh'); expect "entradas no JSON" 2 "objeto JSON" $? "$out"

[ "$fails" -eq 0 ] && echo "dispatch_test: OK" || { echo "dispatch_test: $fails fallos"; exit 1; }
