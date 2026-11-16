#!/bin/sh
# Crea el clon local de opentepache/packages a partir de este repositorio
# (specs/004-split-repos, contracts/migracion.md pasos 2–4). Se ejecuta una
# vez, como el mantenedor, desde el checkout de debfactory:
#
#   ci/migrate/split.sh DESTINO COMMIT
#
#   DESTINO  directorio nuevo o vacío (p. ej. ~/Develop/packages)
#   COMMIT   commit de debfactory ya instalado con `just tool-install`
#
# Conserva con `git filter-repo` la historia de packages/, config/, repo/,
# docs/usuario.md y .github/, sin reescribir la autoría (research D8). Encima
# hace un commit con debfactory.version, README.md, LICENSE y la
# configuración de ci nueva. Después ejecuta validate y ci plan. No crea nada
# en GitHub ni hace push.
set -eu

die() { echo "split: $*" >&2; exit 1; }
say() { echo "split: $*"; }

[ $# -eq 2 ] || die "uso: ci/migrate/split.sh DESTINO COMMIT"
dest=$1 commit=$2
case "$commit" in
    *[!0-9a-f]* | "") die "COMMIT debe ser un commit completo (40 hex)" ;;
esac
[ "${#commit}" -eq 40 ] || die "COMMIT debe ser un commit completo (40 hex)"
command -v git-filter-repo >/dev/null || die "falta git-filter-repo (sudo apt install git-filter-repo)"
[ -x "/srv/debfactory/tools/$commit/debfactory" ] || die "la versión $commit no está instalada (just tool-install)"
if [ -e "$dest" ] && [ -n "$(ls -A "$dest" 2>/dev/null)" ]; then
    die "$dest no está vacío"
fi

here=$(cd "$(dirname "$0")" && pwd)
src=$(cd "$here/../.." && pwd)
[ -z "$(git -C "$src" status --porcelain)" ] || die "$src tiene cambios sin commit"
tool="/srv/debfactory/tools/$commit/debfactory"

say "clonando $src → $dest"
git clone --quiet --no-local "$src" "$dest"
cd "$dest"
git remote remove origin
git filter-repo --quiet --force \
    --path packages/ --path config/ --path repo/ --path docs/usuario.md --path .github/

say "añadiendo debfactory.version, README.md, LICENSE y la configuración de ci"
printf '%s\n' "$commit" > debfactory.version
cp "$here/packages/README.md" "$here/packages/LICENSE" .
awk '
    /^ci:/ { inci = 1 }
    inci && /^  repository:/ {
        print "  repository: \"opentepache/packages\"           # owner/nombre en GitHub"
        print "  tool_repository: \"opentepache/debfactory\"     # solo para el enlace de diferencias (004 D6)"
        next
    }
    inci && /^  code_paths:/ {
        print "  code_paths:              # un cambio en alguno reconstruye y compara todo (FR-010; 004 D6)"
        print "    - config/"
        print "    - debfactory.version"
        skip = 1
        next
    }
    skip && /^    - / { next }
    { skip = 0; print }
' config/debfactory.yaml > config/debfactory.yaml.new
mv config/debfactory.yaml.new config/debfactory.yaml
git add -A
git commit --quiet -m "packages: separate repository from debfactory (specs/004-split-repos)" \
    -m "Pins debfactory $commit, adds README and MIT LICENSE, and points ci at opentepache/packages."

say "comprobando"
"$tool" validate --root .
"$tool" ci plan --root .
git log --oneline | head -5
say "listo. Revisa 'git log --stat' (solo packages/, config/, repo/, docs/usuario.md y .github/)"
say "y crea opentepache/packages en GitHub (contracts/migracion.md, paso 5)."
