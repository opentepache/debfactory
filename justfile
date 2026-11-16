# Atajos de debfactory (specs/001-manifest-deb-builder/contracts/cli.md).
# La lógica vive en el binario; aquí solo se encadenan órdenes.

export GOTOOLCHAIN := "local"
export PATH := "/usr/lib/go-1.26/bin:" + env_var("PATH")

# Compila bin/debfactory.
bin:
    go build -o bin/debfactory ./cmd/debfactory

# Construye <pkg> para amd64 y arm64, o solo para <arch>.
build pkg arch="": bin
    bin/debfactory build {{pkg}} {{ if arch == "" { "" } else { "--arch " + arch } }}

# Construye todos los paquetes de packages/.
build-all: bin
    bin/debfactory build --all

# Pasa lintian a los .deb de <pkg> que hay en dist/.
lint pkg: bin
    bin/debfactory lint dist/{{pkg}}_*.deb

# Prueba la instalación de <pkg> en cada suite.
test pkg arch="": bin
    bin/debfactory test {{pkg}} {{ if arch == "" { "" } else { "--arch " + arch } }}

# Genera o actualiza packages/<pkg>.lock.yaml.
lock pkg: bin
    bin/debfactory lock {{pkg}}

# Valida todos los manifiestos.
validate: bin
    bin/debfactory validate

# Comprobación completa antes de dar algo por bueno (objetivo orientativo: < 30 min).
check: bin
    #!/bin/sh
    set -eu
    start=$(date +%s)
    test -z "$(gofmt -l .)" || { gofmt -l .; echo "gofmt: hay archivos sin formatear"; exit 1; }
    go vet ./...
    go test ./...
    go test -tags integration ./internal/repo/ ./internal/sign/ ./internal/ci/
    just ci-lint
    bin/debfactory validate
    bin/debfactory build --all --check-reproducible
    bin/debfactory test --all
    if [ -L "${DEBFACTORY_DATA:-$HOME/.local/share/debfactory}/public/current" ]; then bin/debfactory repo verify; fi
    echo "just check: $(( $(date +%s) - start )) s"

# Publica <pkg> (versión del manifiesto, o la indicada) en el repositorio APT.
publish pkg version="": bin
    bin/debfactory publish {{pkg}} {{version}}

# Retira <version> de <pkg> de todas las suites.
retire pkg version reason: bin
    bin/debfactory retire {{pkg}} {{version}} --reason "{{reason}}"

# Sincroniza el repositorio con <dest> y lo verifica.
sync dest: bin
    bin/debfactory repo sync {{dest}}
    bin/debfactory repo verify --dest {{dest}}

# Sirve el destino local en http://localhost:8080/.
serve: bin
    bin/debfactory repo serve

# Pruebas E2E del repositorio (SC-001, SC-003, SC-004).
e2e: bin
    go test -tags e2e -run 'TestE2E(Repo|Lifecycle|Failures|SyncLocal|RebuildFromGit)' ./internal/repotest/ -timeout 60m -v

# Prueba E2E de la llave (SC-006).
e2e-key: bin
    go test -tags e2e -run TestE2EKey ./internal/repotest/ -timeout 30m -v

# Sincroniza a un bucket S3 real con una llave de prueba (T048). Usa las credenciales de rclone.
e2e-r2 dest="r2test": bin
    DEBFACTORY_E2E_R2={{dest}} go test -tags e2e -run TestE2ESyncR2 ./internal/repotest/ -timeout 30m -v

# --- Pipeline (specs/003-ci-pipeline/contracts/cli.md § justfile) ---

# Construye las imágenes de los runners (ci y pub) desde ci/runner/Dockerfile.
runner-image:
    #!/bin/sh
    set -eu
    . ci/runner/runner.env
    tag="${RUNNER_VERSION}-$(git rev-parse --short HEAD)"
    for t in ci pub; do
        docker build --pull --target "$t" \
            --build-arg RUNNER_VERSION="$RUNNER_VERSION" --build-arg RUNNER_SHA256="$RUNNER_SHA256" \
            -t "debfactory-runner:${tag}-${t}" ci/runner
    done
    echo "imágenes: debfactory-runner:${tag}-{ci,pub}"
    echo "para usarlas: DEBFACTORY_RUNNER_TAG=${tag} en /etc/default/debfactory-runner"

# Comprueba la configuración de GitHub y de la PC (GH_TOKEN del mantenedor).
ci-doctor *args: bin
    bin/debfactory ci doctor {{args}}

# Ejecuta en local lo mismo que el check de un PR.
ci-local-pr: bin
    bin/debfactory ci pr --base origin/main

# Comprueba los scripts de ci/ (POSIX sh) y el hook.
ci-lint:
    #!/bin/sh
    set -eu
    for f in $(find ci -name '*.sh'); do sh -n "$f"; done
    if command -v checkbashisms >/dev/null; then checkbashisms $(find ci -name '*.sh'); else echo "ci-lint: falta checkbashisms (devscripts)"; exit 1; fi
    sh ci/runner/hooks/trusted-gate_test.sh
