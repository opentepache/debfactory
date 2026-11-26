# Research: Paquetes de nivel 2 (compilación desde el código fuente)

Decisiones de la fase 008. Referencias `FR-`/`SC-` de [spec.md](spec.md). Stack pedido:
sbuild con chroots `unshare` por suite, builds en tmpfs, paralelismo por suite, qemu-user para
arm64 (o compilación cruzada), lintian y piuparts antes de publicar.

## D1. sbuild `--chroot-mode=unshare` dentro de un contenedor de construcción

**Decisión**: cada compilación corre en un contenedor nuevo y desechable de la imagen
`debfactory-build/<suite>:<arch>`, que contiene `sbuild`, `mmdebstrap` y `devscripts` de esa misma
suite. Dentro, `sbuild --chroot-mode=unshare --dist=<suite> --arch=<arch>` crea el chroot desde los
repositorios oficiales de la suite (`mmdebstrap`, en tmpfs: D2), instala las dependencias de
compilación con red y compila **sin red** (el modo `unshare` de sbuild aísla la red durante el
build salvo `--enable-network`). Produce `.dsc`, `.debian.tar.*`, los `.deb`, `.buildinfo` y
`.changes`.

El contenedor de construcción **no monta** `<data>` ni ninguna credencial: solo un directorio de
entrada (fuente y `debian/`) y uno de salida (FR-005). Lo arranca la herramienta con el Docker
rootless del usuario del runner, como las pruebas de instalación.

**Riesgo (spike, tasks fase 1)**: un `unshare` de user namespace dentro de un contenedor de Docker
rootless puede chocar con el perfil seccomp y AppArmor por defecto y con el montaje de `/proc`. El
spike comprueba qué opciones mínimas lo permiten (`--security-opt seccomp=…` con un perfil que
añada solo `unshare`/`clone` de namespaces, `--security-opt apparmor=unconfined`, nunca
`--privileged`) en los runners `ci` y `pub` reales.

**Alternativa si el spike falla** (documentada, no elegida): el contenedor de la suite **es** el
chroot. Contenedor limpio de `debian:<suite>`, `apt-get build-dep` con red,
`docker network disconnect` (la herramienta ya lo usa: `container.DisconnectNetwork`) y
`dpkg-buildpackage`, que también genera `.buildinfo`. Pierde la equivalencia exacta con los buildd
de Debian, pero no necesita namespaces anidados.

**Motivo**: sbuild es la herramienta estándar de Debian para compilar en limpio, empaquetada en
Debian (principio IX), y su modo `unshare` no necesita root ni schroot. Encerrarlo en un contenedor
desechable sin montajes da la misma barrera que hoy tienen las pruebas de instalación.

## D2. tmpfs dimensionado para 31 GB

**Hecho**: la PC tiene **31 GB** de RAM (constitución IX), no 64; hoy hay ~24 GB disponibles con los
servicios en marcha.

**Decisión**: el chroot y el árbol de compilación van en un tmpfs del contenedor de construcción
(`--tmpfs /build:size=<n>`), con `n` en la configuración (`level2.tmpfs`, por defecto `6g`). La
concurrencia de compilaciones (`level2.jobs`, por defecto 3) cumple `jobs × tmpfs ≤ 18 GB` y lo
comprueba `ci doctor`. Un paquete que necesite más lo declara en su manifiesto
(`build.tmpfs`), dentro del mismo límite.

**Motivo**: compilar en RAM evita el disco en lo más lento (desempaquetar el chroot, objetos
intermedios) sin arriesgar el OOM de la PC, que también corre los runners.

## D3. Paralelismo por suite y arquitectura

**Decisión**: las compilaciones de una versión (suites × arquitecturas × 2 por D8) se reparten con el
mismo limitador que las pruebas de instalación (`installtest.Limiter`), con `level2.jobs` como tope
propio. Las de amd64 empiezan primero (son rápidas) y las de arm64 se encolan detrás.

## D4. arm64 con qemu-user; compilación cruzada fuera de esta fase

**Decisión**: arm64 se compila en un contenedor `--platform linux/arm64` con qemu-user (binfmt ya
registrado en la PC). **No** se usa compilación cruzada en esta fase.

**Motivo** contra la compilación cruzada: US3 exige que quien verifica, con `apt-get source` en un
sistema arm64, obtenga el mismo binario; un binario compilado en cruzado casi nunca coincide byte a
byte con uno nativo (rutas de la herramienta, `dpkg-architecture`, autodetecciones del configure).
Además el `.buildinfo` de un build cruzado no describe un entorno reproducible nativo. Queda como
posible fase futura, por paquete y con su propia verificación.

**Coste medido** (fase 007, E2E): instalar en arm64 emulado es ~10× más lento que en amd64; compilar
puede ser 10–30×. Por eso el paquete de prueba es pequeño (D14) y los tiempos se miden en T00x.

## D5. Red y aislamiento (FR-005)

**Decisión**: con red solo `mmdebstrap` y la instalación de dependencias de compilación, ambos desde
los mirrors oficiales de la suite que fija la configuración (`suites.<s>.mirror`, por defecto
`deb.debian.org`/`archive.ubuntu.com`, `ports.ubuntu.com` para arm64 de Ubuntu). El build en sí, sin
red (D1). Un `rules` que intente descargar falla y se notifica (edge case de la spec).

## D6. El paquete fuente que se genera

**Decisión**: formato `3.0 (quilt)`. El `.orig.tar.*` es exactamente el archivo de upstream
descargado y verificado (checksum del lockfile o firma, como el nivel 1: principio III), renombrado
a `<fuente>_<upstream>.orig.tar.<ext>`: es el mismo archivo para todas las suites (FR-010). La
herramienta copia `packages/<p>/debian/` del manifiesto, genera `debian/changelog` por suite
(FR-002):

```text
<fuente> (<upstream>-0~tepache<N>+<sufijo>) <suite>; urgency=medium

  * OpenTepache build of <upstream> for <suite>.

 -- <maintainer de la configuración>  <fecha de publicación del release upstream, RFC 2822>
```

y construye el `.dsc` con `dpkg-source -b` **una sola vez por suite**, en un paso propio dentro de un
contenedor de construcción, con `SOURCE_DATE_EPOCH` tomado de la fecha del changelog para que el
`.debian.tar.*` sea reproducible. Las dos compilaciones de D8 consumen **ese mismo** `.dsc` (sbuild
compila desde el `.dsc`), y se publica ese: así el fuente publicado es exactamente el que produjo
los binarios (constitución II). La herramienta comprueba que el sha256 del `.dsc` que reporta cada
compilación es el del generado.

## D7. Validación del manifiesto (FR-001, FR-003)

Campos nuevos (contracts/manifest.md): `level: 2`, `justification` (obligatorio en nivel 2: una o
varias líneas), `source` (URL con plantillas y formato `tar.gz|tar.xz|tar.bz2`; la verificación sale
del lockfile como hoy) y la carpeta `packages/<p>/debian/` con `control`, `rules` (ejecutable) y
`copyright`, sin `changelog`. El campo `Source:` de `control` debe ser `name`. `artifact`,
`install`, `completions` y `arch_map` no aplican en nivel 2 (el esquema los rechaza).

## D8. Doble compilación y comparación (FR-008)

**Decisión**: por suite y arquitectura, dos compilaciones del mismo `.dsc` (D6) en contenedores
distintos, la segunda con otra ruta de build (`/build/a` y `/build/b`) y otro `umask`, para detectar
no reproducibilidad. Se
comparan los sha256 de todos los `.deb`; si difieren, falla la etapa `compare` con los dos sha256 y,
si está instalado en la imagen, un resumen de `diffoscope`. Se publica el `.buildinfo` de la primera.

## D9. lintian y piuparts

**Decisión**: lintian sobre los `.deb` y el `.dsc` (mismas reglas que el nivel 1: `fail_on`,
`never_override_patterns`). piuparts por suite y arquitectura como **paso aparte**, en su propio
contenedor de la imagen de construcción, con un chroot base de la suite (`--basetgz`, creado con
`mmdebstrap` desde el mirror oficial): instala, desinstala y purga, y comprueba que no deja
archivos. **Nunca** con el modo Docker de piuparts ni con el socket de Docker dentro del
contenedor: eso daría control del Docker del runner a un proceso que maneja código de upstream
(FR-005). Las capacidades mínimas que piuparts necesita para su `chroot` y sus montajes las fija el
spike (T001), igual que las de sbuild. Solo para nivel 2: el nivel 1 no cambia (FR-014). La prueba de instalación de siempre
(fase 001) también corre.

## D10. Publicación con aptly: fuente y binarios (FR-009, FR-010)

**Decisión**: `aptly repo add` de cada suite recibe sus `.deb` y su `.dsc` (aptly añade solo los
archivos que referencia); la publicación incluye la arquitectura `source`. `repo verify` y
`sync` ya recorren todo el árbol, `.dsc` y tarballs incluidos. aptly no publica `.buildinfo`: la
generación los copia a `pool/` junto a su `.deb`, entran en `GenInfo.Files` (y así en la
sincronización) y `repo verify` comprueba su sha256 contra el estado. La comprobación de "un nombre, un
contenido" (SC-005) se añade a `repo verify` sobre el pool.

## D11. Estado y auditoría (FR-011)

**Decisión**: `repo/state.yaml` pasa a `schema: 2`, compatible con el 1 (las entradas de nivel 1 no
cambian). Una versión de nivel 2 lleva `level: 2` y, en vez de `debs` por arquitectura, `builds` por
suite: versión con sufijo, sha256 de cada `.deb` por arquitectura, del `.dsc` y del `.debian.tar.*`,
y el sha256 del `.orig.tar.*` común (data-model §2). La auditoría registra lo mismo en la entrada
`publish`/`extend` (`builds`).

## D12. Suites nuevas para nivel 2 (FR-015)

**Decisión**: `MissingSuites` (fase 007) sirve igual; para nivel 2 el plan pone la versión en
`extend` con `level: 2`, y los gates compilan **solo** las suites que faltan (D1–D9) y publican en
ellas. Se registra como `extend` con sus `builds` nuevos.

## D13. Sitio (FR-012)

**Decisión**: la página de un paquete de nivel 2 muestra la versión de cada suite (con su sufijo) en
la tabla de disponibilidad y el comando `apt-get source <p>`; la portada sigue mostrando la versión
upstream.

## D14. Paquete de prueba: `tepache-hello`

**Decisión**: un programa C mínimo propio, en `testdata/`, que enlaza con `libcrypto` de OpenSSL e
imprime su versión. En bookworm el binario depende de `libssl3` y en trixie de `libssl3t64` (la
transición de time_t de 64 bits renombró el paquete): demuestra que cada suite tiene su binario
(SC-001). Compila en segundos incluso emulado. Su "upstream" es un tarball servido por el servidor
TLS de pruebas que ya existe (`citest.Upstream`), con checksum en su lockfile. Nunca va a producción
(clarify).

## D15. Dónde corre

| Ejecución | Runner | Qué hace |
|---|---|---|
| PR (`pr.yml`) | `ci` (dfci) | compila (doble), lintian, piuparts, instalación; no publica |
| `main` | `pub` (dfpub) | lo mismo y publica; la compilación va en contenedores aparte sin `<data>` (D1) |

**Riesgo aceptado**: el runner `pub` ejecuta código de upstream durante la compilación, aunque en un
contenedor sin montajes ni credenciales. Es el mismo modelo que las pruebas de instalación de nivel
1, que ya ejecutan los binarios de upstream en el runner `pub`.

## D16. Imágenes de construcción

**Decisión**: `debfactory-build/<suite>:<arch>` se construye desde la imagen de prueba de la suite
(fase 001) con `sbuild mmdebstrap devscripts lintian piuparts` de esa suite, y se renueva cada 7
días como las demás. Sin herramientas de terceros: todo empaquetado en Debian/Ubuntu (principio IX).
