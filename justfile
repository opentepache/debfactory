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
    bin/debfactory validate
    bin/debfactory build --all --check-reproducible
    bin/debfactory test --all
    echo "just check: $(( $(date +%s) - start )) s"
