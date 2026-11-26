# Quickstart: validar el nivel 2

## 1. Spike (antes de todo)

En los runners `ci` y `pub` reales: un contenedor de construcción de trixie arranca `sbuild
--chroot-mode=unshare` de un paquete trivial con las opciones mínimas de research D1; anotar en D1
las opciones que funcionan, o la decisión de usar la alternativa.

## 2. Local

```sh
just build-image-level2          # imágenes debfactory-build/<suite>:<arch>
bin/debfactory build --root testdata/project tepache-hello --check-reproducible
bin/debfactory test --root testdata/project tepache-hello
ls testdata/project/dist/tepache-hello_*
```

Esperado: `.dsc`, `.debian.tar.xz`, `.deb` y `.buildinfo` por suite y arquitectura; las dos
compilaciones idénticas; lintian y piuparts aprobados; `dpkg -I` del `.deb` de bookworm depende de
`libssl3` y los de trixie y noble de `libssl3t64`.

## 3. Pruebas

```sh
just check   # unitarias (manifiesto, estado v2, changelog, versiones) e integración (ci con nivel 2)
just e2e     # publica tepache-hello en el repositorio de prueba, apt-get install y apt-get source
             # en bookworm, trixie y noble (amd64 y arm64), y recompila siguiendo la guía (SC-001–SC-003)
```

Esperado además: los 5 paquetes de nivel 1 con los mismos sha256 (SC-004) y `repo verify` sin
nombres repetidos en el pool (SC-005).
