# Recompilar un paquete de nivel 2

Los paquetes de nivel 2 se compilan desde el código fuente, uno por cada suite. Junto a cada
binario el repositorio publica su paquete fuente (`.dsc`, `.orig.tar.*`, `.debian.tar.xz`) y el
`.buildinfo` con el entorno exacto de la compilación. Con esta guía obtienes el mismo `.deb`, byte
a byte, a partir de ese código fuente.

Se necesita un sistema limpio de la misma suite y arquitectura que el binario (un contenedor sirve:
`docker run --rm -it debian:trixie`) con el repositorio agregado como en la
[guía de usuario](usuario.md). Los ejemplos usan `tepache-hello`; cambia el nombre por el del paquete.

## 1. Activar el código fuente del repositorio

```sh debfactory:srcadd
sudo sed -i 's/^Types: deb$/Types: deb deb-src/' /etc/apt/sources.list.d/debfactory.sources
sudo apt-get update
sudo apt-get install -y dpkg-dev
```

## 2. Bajar el código fuente

En la misma ruta que usaron las compilaciones publicadas (la de los buildd de Debian):

```sh debfactory:source
sudo mkdir -p /build/reproducible-path
sudo chown "$(id -u):$(id -g)" /build/reproducible-path
cd /build/reproducible-path
apt-get source tepache-hello
```

`apt-get source` comprueba el `.dsc` y sus archivos contra los índices firmados del repositorio.

## 3. Instalar las dependencias de compilación

```sh debfactory:builddeps
cd /build/reproducible-path
sudo apt-get build-dep -y ./tepache-hello-*/
```

Para una versión publicada hace tiempo, las dependencias de tu suite pueden haber cambiado de
versión. El `.buildinfo` publicado junto al `.deb` (en el mismo directorio de `pool/`) lista las
versiones exactas (`Installed-Build-Depends`); instálalas desde el archivo histórico de la
distribución (`snapshot.debian.org` para Debian) antes de compilar.

## 4. Compilar

```sh debfactory:rebuild
cd /build/reproducible-path/tepache-hello-*/
export SOURCE_DATE_EPOCH=$(dpkg-parsechangelog -STimestamp) DPKG_DEB_COMPRESSOR_TYPE=xz
dpkg-buildpackage -b -us -uc
```

`DPKG_DEB_COMPRESSOR_TYPE=xz` hace falta en Ubuntu, cuyo `dpkg` comprime con zstd por defecto: los
paquetes publicados usan xz en todas las suites.

## 5. Comparar con el paquete publicado

```sh debfactory:compare
mkdir -p /tmp/publicado && cd /tmp/publicado
apt-get download tepache-hello
cmp /tmp/publicado/tepache-hello_*.deb /build/reproducible-path/tepache-hello_*.deb && echo "idéntico al publicado"
```

`cmp` no imprime nada y la última línea dice `idéntico al publicado` si los dos `.deb` son iguales
byte a byte.
