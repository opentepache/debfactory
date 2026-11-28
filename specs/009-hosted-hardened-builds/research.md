# Research: Nivel 2 con toolchains de upstream, compilación hospedada y cadena de suministro

Decisiones de la fase 009. Referencias `FR-`/`SC-` de [spec.md](spec.md). Base: fase 008 (nivel 2
en la PC), spike de ghostty del 2026-11-27 y constitución 1.10.0.

## D1. Toolchain de upstream

**Decisión**: el manifiesto declara `toolchain` (nombre, versión, URL por arquitectura con plantilla
y, si upstream firma, su llave minisign). `debfactory lock` registra el sha256 por arquitectura; cada
compilación lo descarga, verifica sha256 y firma, y lo instala en `/opt/toolchain` del contenedor
antes de cortar la red. Para Zig: `https://ziglang.org/download/{{version}}/zig-{{arch}}-linux-{{version}}.tar.xz`
con su `.minisig` y la llave pública de Zig fijada en el manifiesto.

**Motivo**: un compilador que no está en la suite solo es aceptable si su origen y contenido están
fijados igual que el fuente (principio III). **Alternativa descartada**: compilar Zig desde su
fuente (otra cadena de bootstrap entera, sin ganancia de confianza frente a la firma de upstream).

## D2. Dependencias del lenguaje (Zig)

**Decisión**: `debfactory lock` resuelve el cierre transitivo leyendo los `build.zig.zon` del árbol
(sin `example/`) y de cada dependencia obtenida, en rondas hasta que no aparece nada nuevo; cada una
se obtiene **por separado** con `zig fetch` dentro de un contenedor (ningún binario de upstream
corre en el host) y el hash que devuelve MUST ser el declarado. El lockfile guarda por dependencia
la URL, el hash de Zig y el sha256 del árbol extraído.

**Hechos del spike**: `zig build --fetch` da hashes distintos (descargas en paralelo y relectura de
su caché) y `fetch-zig-cache.sh` de upstream no encuentra todas; `zig fetch` uno por uno sí da el
hash declarado de las 39. La caché de Zig 0.17 guarda `p/<hash>.tar.gz` y `zig build --system`
espera directorios: se extrae quitando el envoltorio de la caché y el directorio superior único.

## D3. Las dependencias viajan dentro del paquete fuente

**Decisión**: las dependencias verificadas se empaquetan, en un tar determinista (orden fijo,
dueño `0:0`, mtime fijo, `xz -9`), como componente adicional del paquete fuente `3.0 (quilt)`:
`<name>_<upstream>.orig-zigdeps.tar.xz`, que `dpkg-source -x` extrae en `zigdeps/` dentro del árbol.
Es el mismo archivo en todas las suites (un nombre, un contenido; SC-005 de 008). `debian/rules`
compila con `zig build --system zigdeps`.

**Motivo**: así `apt-get source` trae todo lo necesario para recompilar sin red (la promesa de
la fase 008, US3) y la compilación no necesita red para las dependencias. El toolchain no va en el
fuente: el paquete fuente incluye `debian/upstream/toolchain` con URL y sha256, y la guía de
recompilación explica cómo instalarlo.

## D4. Dónde corre cada paso (constitución IX, clarify de la spec)

Modelo de la constitución 1.11.0 (decisión posterior al clarify): GitHub compila y prueba, la PC
verifica de forma independiente, firma y publica.

| Paso | Dónde | Red | Secretos |
|---|---|---|---|
| `lock` (toolchain, dependencias, tarball contra tag, firmas) | PC del mantenedor, en contenedor (o job de la PR del detector) | sí | no |
| Fuente de nivel 2: verificar tarball (sha256, firma, contra tag), dependencias, paquete fuente por suite | job hospedado `l2-source` | sí | no |
| Compilación de nivel 2 (suite × arquitectura × variante) | jobs hospedados `l2-build` (`ubuntu-24.04`, `ubuntu-24.04-arm`) | solo para `Build-Depends`, luego cortada | no |
| Nivel 1: construir | job hospedado `l1-build` (y otra vez en la PC, D17) | sí | no |
| Pruebas: lintian, instalación y ciclo de vida por suite × arquitectura no excluida (nivel 1 y 2) | jobs hospedados `test` (arm64 nativo) | instalación con red, ejecución sin red | no |
| Verificación independiente (D17), firma, publicación, sincronización, sitio | PC, runner `pub` | sí | solo `pub` |
| PR completas (todo lo anterior salvo la PC) | runners hospedados | — | no |

## D5. Cómo obtienen la herramienta los runners hospedados

**Decisión** (mantenedor, constitución 1.11.0 VII): `opentepache/debfactory` se hace público. Cada
job hospedado compila la herramienta desde el commit fijado en `debfactory.version` (`go build`
determinista con `GOTOOLCHAIN=local` y el Go de la imagen del runner fijado por `actions/setup-go`
con versión exacta) y comprueba que el commit es el fijado. Revisión previa del historial: sin
secretos ni datos de sesiones (2026-11-27).

## D6. Atestaciones y su verificación

**Decisión**: cada job hospedado atesta sus archivos con `actions/attest-build-provenance`
(permisos `contents: read`, `id-token: write`, `attestations: write`; ningún secreto, ninguna
escritura en el repositorio). La PC descarga las atestaciones por digest con la API de GitHub y las
verifica con `cosign verify-blob-attestation` (empaquetado en Debian; el `gh` de trixie no trae
`gh attestation`): emisor `https://token.actions.githubusercontent.com`, identidad del workflow
`opentepache/packages/.github/workflows/<main|pr>.yml` en la ref esperada, y commit de la
predicación igual al del run.

**Nota de constitución**: `id-token: write` y `attestations: write` no son secretos ni escriben el
repositorio; la enmienda (D15) precisa "sin secretos ni permisos de escritura sobre el repositorio".

## D16. Pruebas en runners hospedados

**Decisión**: la matriz `test` (paquete × suite × arquitectura no excluida) corre la prueba de
instalación y la de ciclo de vida de la fase 008 dentro del contenedor oficial de la suite en un
runner nativo de cada arquitectura (Docker del runner), y lintian en amd64. Cada combinación sube
su `test.json` (con el sha256 del `.deb` probado) atestado. La PC exige un informe atestado y
aprobado por cada combinación no excluida (constitución VII) además de su verificación propia.

## D17. Verificación independiente en la PC

**Decisión**: antes de firmar, el runner `pub`:
1. verifica la atestación de cada artefacto e informe (D6);
2. nivel 2: compara los sha256 de las dos variantes;
3. nivel 1: reconstruye cada paquete en la PC (segundos) y exige el mismo sha256 que el artefacto
   de GitHub, lo que detecta un artefacto alterado sin depender de la atestación;
4. pasa lintian a todos los `.deb`;
5. prueba de humo: instala y ejecuta cada paquete en un contenedor amd64 de la primera suite no
   excluida (no la matriz completa).

El modo local (`ci main --local`, FR-011b) repite todo en la PC como en la fase 008.

## D18. Página de procedencia y `debfactory verify`

**Decisión**: el estado de cada versión enlaza a un `provenance.json` publicado junto al sitio
(`/<p>/<versión>/provenance.json`) con: URLs y sha256 de upstream, resultado de firma, procedencia y
tarball contra tag; commit del manifiesto; URL del run; digest y URL de la atestación en Rekor
(`search.sigstore.dev`); `.buildinfo` (nivel 2); resultados de prueba; número de la entrada de
auditoría. El sitio lo muestra en `/<p>/<versión>/`. `debfactory verify` lee ese archivo, baja el
`.deb` del repositorio, verifica la firma del `InRelease` con el keyring publicado, la atestación con
`cosign` y reconstruye o recompila con la herramienta pública.

## D19. Commits firmados en `packages`

**Decisión**: regla de rama "Require signed commits" en `main` de `packages`; los commits de la App
de GitHub van firmados por GitHub (API de contenidos); los del mantenedor, con su llave SSH o GPG
registrada en GitHub. `ci doctor` comprueba la regla. Las integraciones por "squash" las firma
GitHub.

## D20. Retiro del runner `ci`

**Decisión**: `pr.yml` deja de usar `[self-hosted, debfactory-pr]`; la documentación de operación
explica cómo desregistrar el runner y borrar el usuario `dfci` (`ci/runner/setup.sh --remove ci`).
`ci doctor` deja de exigirlo.

## D21. Paquete autocontenido

**Decisión**: todo lo de un paquete vive en `packages/<p>/`: `packages/<p>/<p>.yaml`,
`packages/<p>/<p>.lock.yaml`, `packages/<p>/debian/`. La herramienta acepta las dos ubicaciones
durante la transición (las actuales `packages/<p>.yaml`) y una orden la migra
(`debfactory migrate layout`).

## D7. Doble compilación en jobs distintos

**Decisión**: la matriz de `level2-build` incluye `variant: [0, 1]` (entornos distintos de la fase
008); la PC exige sha256 iguales de los dos `.deb` de cada combinación antes de probar. Se publica el
`.buildinfo` de la variante 0.

## D8. Exclusión de arquitecturas

**Decisión**: `exclude.arches: {arm64: "<motivo>"}`; la validación exige al menos una arquitectura.
En el estado, una versión puede llevar `arches` (omitido si son todas, para que el estado de nivel 1
no cambie); `Validate` exige `.deb` exactamente para esas. `MissingArches` (análogo a
`MissingSuites` de 007) detecta una arquitectura antes excluida y el plan la amplía: se compila solo
esa arquitectura (nivel 1: construir; nivel 2: compilar en runner), se prueba, y `Apply` agrega sus
archivos a la versión vigente sin tocar los demás (`extend` con `arches` en la auditoría). El sitio
muestra las arquitecturas y el motivo.

## D9. Tarball de release contra tag de git (FR-014)

**Decisión**: `debfactory lock` descarga, además del tarball de release, el archivo del tag que
genera GitHub (`/archive/refs/tags/<tag>.tar.gz`), registra su sha256 y compara archivo por archivo
(sin el directorio superior). Cada archivo añadido o distinto en el tarball de release MUST estar
declarado en `source.extra_files` por ruta exacta con motivo, y su sha256 queda en el lockfile;
cualquier otro, o uno declarado cuyo sha256 cambió, falla. Archivos del tag que faltan en el
tarball (p. ej. `export-ignore`) no fallan. Se repite en cada compilación con los archivos del
lockfile.

**Resultado de la medición (T001, 2026-11-27)**: el tarball de release de ghostty 1.4.0 (5934
archivos) frente al archivo del tag `v1.4.0` (5930, sha256 `2e2a2516…`) trae solo **4 archivos de
más** y ninguno distinto ni faltante: `VERSION`, `src/apprt/gtk/ghostty_resources.c`,
`src/apprt/gtk/ghostty_resources.h` y `src/build/framegen/framedata.compressed`. Son generados al
empaquetar y van a `source.extra_files` con su motivo. Nota de riesgo: `ghostty_resources.c` es un
blob de recursos de GTK compilado a C, el tipo de archivo donde podría esconderse algo; queda fijado
por sha256 y cualquier cambio entre versiones vuelve a revisión. Regenerarlo desde los `.blp`/`.ui`
y compararlo se anota como mejora futura.

## D10. Llaves de firma fijadas (FR-015)

**Decisión**: `signing` en el manifiesto, uno de:
`minisign: {key, file}`, `gpg: {fingerprint, key_file, file}` o
`sigstore: {identity, issuer, bundle}`, donde `file`/`bundle` es la plantilla de la URL de la firma
(del archivo de checksums o del artefacto). Se verifica con `minisign`, `gpgv` o `cosign`
(empaquetados en Debian) antes de usar lo firmado. `debfactory lock` avisa si el release trae
archivos de firma (`.minisig`, `.sig`, `.asc`, `.sigstore.json`) y el manifiesto no fija llave.
Estado actual de upstream: buf (minisign sobre `sha256.txt`), aqua (sigstore sobre los checksums),
ghostty (minisign); fnm y bob no firman.

## D11. Procedencia de upstream (FR-016)

**Decisión**: para cada artefacto o tarball, la herramienta consulta las atestaciones de GitHub de
`upstream` por su digest (API pública), elige las de tipo `https://slsa.dev/provenance/v1` y las
verifica con `cosign verify-blob-attestation --new-bundle-format --type slsaprovenance1` exigiendo
emisor `https://token.actions.githubusercontent.com`, **repositorio y ref de origen** con
`--certificate-github-workflow-repository <upstream>` y `--certificate-github-workflow-ref
refs/tags/<tag>`; la identidad del firmante (`--certificate-identity-regexp`) se acepta de
cualquier workflow de GitHub salvo que el manifiesto fije uno. Si no hay atestaciones, el informe
dice "sin procedencia".

**Resultado de la medición (T001)**: con el `cosign` de trixie y la atestación real de
`aqua_linux_amd64.tar.gz` (v2.66.1): repositorio y tag correctos → OK; otro repositorio → falla;
otro tag → falla; otro archivo → falla. aqua firma desde un workflow **reutilizable de otro
repositorio** (`suzuki-shunsuke/go-release-workflow`), por eso la identidad del certificado no es la
de aqua y la verificación MUST basarse en las extensiones de origen, no en la identidad. La primera
atestación de aqua (tipo `release/v0.2`, solo con sello de tiempo) se ignora.
aqua publica atestaciones; su `multiple.intoto.jsonl` (SLSA) no se usa porque las atestaciones de
GitHub ya lo cubren.

## D12. OpenSSF Scorecard (FR-017)

**Decisión**: un job hospedado de la PR consulta `api.securityscorecards.dev` (público, sin llave,
`User-Agent: debfactory`) para cada paquete agregado o actualizado y deja la puntuación y las 3
comprobaciones más bajas en el resumen del job y en un comentario, sin bloquear.

## D13. Resumen del cambio con IA (FR-018, clarify)

**Decisión**: un job hospedado sin secretos (`contents: read`, `models: read`) calcula de forma
determinista el diff entre el fuente de la versión publicada y la nueva (archivos cambiados,
binarios añadidos, cambios en scripts de build/instalación, URLs nuevas) y pide a GitHub Models un
resumen en español con una sección de "cambios sospechosos"; el resultado es un artefacto. Un job
distinto, sin IA, lo publica como comentario. Así el job con IA no tiene permisos de escritura. Sin
respuesta o sin cuota: "resumen no disponible", sin bloquear. Modelo configurable en
`config/debfactory.yaml` (`watch.ai_model`).

## D14. ghostty 1.4.0

- Suites: trixie y forky; excluidas sid (transiciones de librerías), bookworm (GTK 4.8) y noble
  (sin `gtk4-layer-shell`).
- `Build-Depends`: `libgtk-4-dev libadwaita-1-dev blueprint-compiler libgtk4-layer-shell-dev
  pandoc libonig-dev libfreetype-dev libharfbuzz-dev libfontconfig-dev libpng-dev zlib1g-dev
  libbz2-dev glslang-dev gettext libxml2-utils pkg-config`.
- `debian/rules`: `zig build --system zigdeps -fno-sys=harfbuzz -Doptimize=ReleaseFast
  -Dcpu=baseline -Dversion-string=$(DEB_VERSION_UPSTREAM) --prefix /usr` con `DESTDIR`.
- Memoria: 11,8 GB en el spike; cabe en los 16 GB del runner estándar sin tmpfs grande (se compila
  en el disco del runner).
- Tiempos del spike: 6 min en amd64 (16 hilos), 9,5 min con 4 núcleos.

## D15. Enmiendas de constitución necesarias

1. **Principio III**: además de las URLs de release declaradas, un paquete de nivel 2 MAY
   descargar (a) un toolchain de upstream declarado en el manifiesto, verificado por sha256 y, si
   upstream firma, por su firma, y (b) las dependencias que declara el gestor de paquetes del
   lenguaje en el propio fuente verificado, cada una con el hash declarado por upstream y el sha256
   registrado en el lockfile. Las llaves de firma de upstream, cuando existen, MUST fijarse en el
   manifiesto.
2. **Principio IX** (precisión): los jobs hospedados corren "sin secretos ni permisos de escritura
   sobre el repositorio", admitiendo solo los permisos de identidad y atestación.

**Aplicadas** en la constitución 1.11.0 (2026-11-27), junto con la ampliación de IX (pruebas
hospedadas, verificación independiente, respaldo local) y la transparencia (VII).
