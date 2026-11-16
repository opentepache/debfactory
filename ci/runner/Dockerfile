# Imagen de los runners efímeros de debfactory (specs/003-ci-pipeline,
# contracts/runner-host.md § Imagen; research D5).
#
# Paquetes de Debian trixie (y golang-1.26 de trixie-backports). El único
# binario upstream es actions/runner, verificado por sha256 y ejecutado solo
# dentro de este contenedor. Se construye con `just runner-image`, que lee
# RUNNER_VERSION y RUNNER_SHA256 de ci/runner/runner.env.
#
# Actualizar el runner es un cambio revisado de runner.env; nunca se actualiza
# solo dentro de un job (ver T021 del spike).

FROM debian:trixie@sha256:9c6e381421d0f48d1b8133e573f33c185ee16cbf505eda9c4a8a4791b25ea8a6 AS ci

ARG RUNNER_VERSION
ARG RUNNER_SHA256

RUN set -eu; \
    echo 'deb http://deb.debian.org/debian trixie-backports main' > /etc/apt/sources.list.d/backports.list; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates curl git jq just aptly gnupg gpgv rclone docker-cli docker-buildx \
        libicu76 libkrb5-3 zlib1g liblttng-ust1t64; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends -t trixie-backports golang-1.26-go; \
    rm -rf /var/lib/apt/lists/*

# actions/runner: descarga verificada por sha256; falla si no coincide.
RUN set -eu; \
    test -n "${RUNNER_VERSION}" && test -n "${RUNNER_SHA256}"; \
    mkdir -p /opt/runner; cd /opt/runner; \
    curl -fsSL -A debfactory -o runner.tar.gz \
        "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"; \
    echo "${RUNNER_SHA256}  runner.tar.gz" | sha256sum -c -; \
    tar xzf runner.tar.gz; rm runner.tar.gz

COPY entrypoint.sh /usr/local/bin/debfactory-runner-entrypoint

# Root del contenedor = el usuario sin privilegios del nivel en el host
# (Docker rootless), así los archivos de los montajes quedan a su nombre.
# Sin sudo.
ENV RUNNER_ALLOW_RUNASROOT=1 \
    PATH=/usr/lib/go-1.26/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOTOOLCHAIN=local \
    GOFLAGS=-mod=readonly

WORKDIR /opt/runner
ENTRYPOINT ["/usr/local/bin/debfactory-runner-entrypoint"]

# Nivel pub: añade el filtro previo al job (research D3).
FROM ci AS pub
COPY hooks/trusted-gate.sh /usr/local/lib/debfactory/trusted-gate.sh
ENV ACTIONS_RUNNER_HOOK_JOB_STARTED=/usr/local/lib/debfactory/trusted-gate.sh
