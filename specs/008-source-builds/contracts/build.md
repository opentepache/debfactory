# Contrato: construcción y publicación de nivel 2

## `debfactory build <pkg>` (nivel 2)

```text
debfactory build tepache-hello [--suite S] [--arch A] [--check-reproducible]
```

Por cada suite (no excluida) y arquitectura: genera el paquete fuente una sola vez (research D6),
compila ese `.dsc` en un contenedor limpio de la suite (D1′: dependencias con red, build sin red), y
con `--check-reproducible` lo compila otra vez con otro entorno y compara (D8; en CI siempre). La
prueba de ciclo de vida (D9) corre aparte, sin socket de Docker. Salida en `dist/`:

```text
dist/tepache-hello_1.0.0.orig.tar.gz
dist/tepache-hello_1.0.0-0~tepache1+deb12.dsc
dist/tepache-hello_1.0.0-0~tepache1+deb12.debian.tar.xz
dist/tepache-hello_1.0.0-0~tepache1+deb12_amd64.deb
dist/tepache-hello_1.0.0-0~tepache1+deb12_amd64.buildinfo
dist/tepache-hello_1.0.0-0~tepache1+deb12_amd64.build.json   # sha256, lintian, piuparts, reproducible
…
```

Errores (`report`): etapas `fetch`, `verify`, `license` (antes de compilar), `source` (generar el
paquete fuente), `build` (sbuild; con el final del log), `compare` (no reproducible), `lintian`,
`lifecycle` (instalar, quitar, purgar), `test` (instalación).

## `debfactory test <pkg>` (nivel 2)

Prueba en cada suite el `.deb` de esa suite (fase 001, contenedor limpio).

## `debfactory publish <pkg>` y `ci main`

Publican fuente y binarios por suite en una sola generación (D10); estado `schema: 2` y auditoría
con `builds` (data-model §2–§3). `ci main` amplía una versión de nivel 2 a una suite nueva
compilando solo esa suite (D12).

## Guía de recompilación (FR-013)

`docs/recompilar-nivel2.md` (en el repositorio de paquetes):

```sh
# en un contenedor limpio de la suite y arquitectura del binario (docker o podman)
apt-get source tepache-hello                       # con deb-src del repositorio
apt-get build-dep ./tepache-hello-<upstream>       # o las versiones exactas del .buildinfo publicado
cd tepache-hello-<upstream>                        # en /build/reproducible-path/
SOURCE_DATE_EPOCH=$(dpkg-parsechangelog -STimestamp) dpkg-buildpackage -b -us -uc
sha256sum ../tepache-hello_<versión>_<arch>.deb    # = el publicado
```

La prueba `TestE2ERebuildLevel2` sigue la guía en contenedores limpios.
