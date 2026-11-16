#!/bin/sh
# Prueba de launcher.sh (la ejecuta `just ci-lint`).
set -u
launcher=$(cd "$(dirname "$0")" && pwd)/launcher.sh
fails=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
a=0123456789abcdef0123456789abcdef01234567
b=fedcba9876543210fedcba9876543210fedcba98
mkdir -p "$tmp/tools/$a" "$tmp/work"
cat > "$tmp/tools/$a/debfactory" <<'EOT'
#!/bin/sh
printf 'args:'; for x in "$@"; do printf ' [%s]' "$x"; done; echo
EOT
chmod 755 "$tmp/tools/$a/debfactory"

run() { # contenido de debfactory.version ("-" = sin archivo), argumentos…
    content=$1; shift
    rm -f "$tmp/work/debfactory.version"
    [ "$content" = - ] || printf '%s\n' "$content" > "$tmp/work/debfactory.version"
    (cd "$tmp/work" && DEBFACTORY_TOOLS="$tmp/tools" sh "$launcher" "$@" 2>&1)
}
expect() { # nombre código-esperado texto-esperado código-obtenido salida
    if [ "$4" -ne "$2" ] || ! printf '%s' "$5" | grep -qF -- "$3"; then
        echo "FALLA: $1: código $4, salida «$5»; se esperaba $2 y «$3»"
        fails=$((fails + 1))
    fi
}

out=$(run -); expect "sin archivo" 2 "debfactory.version falta" $? "$out"
out=$(run "v1.2.3"); expect "formato inválido" 2 "no es un commit" $? "$out"
out=$(run "$(echo $a | tr a-f A-F)"); expect "mayúsculas" 2 "no es un commit" $? "$out"
out=$(run "${a}0"); expect "41 caracteres" 2 "no es un commit" $? "$out"
out=$(run "$b"); expect "no instalada" 2 "la versión $b no está instalada" $? "$out"
out=$(run "$b"); expect "lista de instaladas" 2 "instaladas: $a" $? "$out"
out=$(run "$b"); expect "orden para instalarla" 2 "git checkout $b && just tool-install" $? "$out"
out=$(run "$a" ci pr --base "origin/main" "con espacio"); expect "instalada" 0 "args: [ci] [pr] [--base] [origin/main] [con espacio]" $? "$out"
printf '%s\n%s\n' "$a" "$b" > "$tmp/work/debfactory.version"
out=$(cd "$tmp/work" && DEBFACTORY_TOOLS="$tmp/tools" sh "$launcher" 2>&1); expect "dos líneas" 2 "no es un commit" $? "$out"

if [ "$fails" -ne 0 ]; then
    echo "launcher_test: $fails fallos"
    exit 1
fi
echo "launcher_test: ok"
