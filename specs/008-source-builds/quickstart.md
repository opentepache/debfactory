# Quickstart: validar el nivel 2

## 1. Spike (antes de todo)

Hecho (T001): sbuild/piuparts no corren en los contenedores de los runners; se usa el contenedor de
la suite como entorno de construcción (research D1′).

## 2. Local

```sh
just build-image-level2          # imágenes debfactory-build/<suite>:<arch>
bin/debfactory build --root testdata/project tepache-hello --check-reproducible
bin/debfactory test --root testdata/project tepache-hello
ls testdata/project/dist/tepache-hello_*
```

Esperado: `.dsc`, `.debian.tar.xz`, `.deb` y `.buildinfo` por suite y arquitectura; las dos
compilaciones idénticas; lintian y la prueba de ciclo de vida aprobados; `dpkg -I` del `.deb` de bookworm depende de
`libssl3` y los de trixie y noble de `libssl3t64`.

## 3. Pruebas

```sh
just check   # unitarias (manifiesto, estado v2, changelog, versiones) e integración (ci con nivel 2)
just e2e     # publica tepache-hello en el repositorio de prueba, apt-get install y apt-get source
             # en bookworm, trixie y noble (amd64 y arm64), y recompila siguiendo la guía (SC-001–SC-003)
```

Esperado además: los 5 paquetes de nivel 1 con los mismos sha256 (SC-004) y `repo verify` sin
nombres repetidos en el pool (SC-005).
