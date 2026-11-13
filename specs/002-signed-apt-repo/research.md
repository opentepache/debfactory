# Research: Repositorio APT firmado

Fecha: 2026-11-12. Host: Debian trixie amd64, docker-ce 29.9. Los datos de versiones salen de
`apt-cache policy` en el host. Los resultados marcados como **verificado** salen de un spike en
contenedores, con los `.deb` reales de la fase 001 y una llave de prueba desechable.

## 1. Spike

| Qué | Resultado |
|---|---|
| aptly 1.6.1 (trixie) publica snapshots en un `FileSystemPublishEndpoint` con `linkMethod: hardlink`. | **Verificado.** Hay una condición: la raíz de aptly y el endpoint deben estar en el mismo sistema de archivos (si no, `invalid cross-device link`). |
| Un `.deb` usado en varias suites se guarda una sola vez en `pool/`. | **Verificado.** `pool/main/f/fnm/fnm_1.39.0-0~pav1_amd64.deb` es un solo archivo, con dos enlaces (pool de aptly y árbol publicado), para 3 suites. |
| `-acquire-by-hash` | **Verificado.** `Release` contiene `Acquire-By-Hash: yes` y existe `by-hash/SHA256/<hash>` para cada índice. aptly crea además enlaces simbólicos **absolutos** con el nombre del índice dentro de `by-hash/` (p. ej. `by-hash/SHA512/Contents-amd64.gz`). apt no los usa y no deben subirse. |
| `Valid-Until` | **Verificado.** aptly no lo añade por defecto, como pide FR-004a. `Release` sí lleva `Date`, así que los índices firmados no son reproducibles byte a byte. Los `Packages` sí lo son. |
| Firma con una subllave ed25519 `[S]` cuya llave maestra ed25519 `[C]` no está en el llavero (`sec#`) | **Verificado** con `gpgProvider: gpg2`, `-gpg-key=<subllave>!` y `-passphrase-file`. |
| apt acepta la firma sin advertencias | **Verificado** en bookworm (apt 2.6.2, gpgv), trixie (apt 3.x, sqv) y resolute (apt 3.2.1). `apt-get update` no da `W:` ni `E:` y `apt-get install fnm` funciona. noble (apt 2.8.4) y sid (apt 3.3.6) usan los mismos verificadores; quedan para las pruebas E2E. |
| Varias versiones del mismo paquete en una suite | **Verificado.** Con 3 versiones de `fnm` publicadas, `apt-cache policy` las lista todas, `apt install` instala la más alta y `apt install --allow-downgrades fnm=1.39.0-0~pav1` hace el downgrade. |
| Quitar la frase de paso de la subllave exportada | **No es viable con GnuPG 2.4.** `--export-options export-reset-subkey-passwd` ya no existe. `--passwd` en un llavero con la maestra solo como stub falla (`No secret key`), y una frase de paso vacía por loopback no se acepta. Lo que sí funciona es cambiar, en una copia temporal del llavero maestro, la frase de paso a una propia de la PC de build, exportar la subllave y firmar con `--passphrase-file` (decisión D4). |
| Caddy 2.6.2 (Debian) sirve el árbol, en contenedor, a clientes en una red de Docker | **Verificado.** `caddy file-server --root /srv --listen :80`. |
| Imágenes de cliente | Ni las Debian ni las Ubuntu traen `curl`, `wget`, `gpg` ni `ca-certificates`. El `VERSION_CODENAME` de `debian:sid` es `forky`, así que la guía no puede deducir la suite de `/etc/os-release` (D9). |

## 2. Decisiones

### D1. Gestor del repositorio: aptly (frente a reprepro)

- **Decisión:** aptly 1.6.1 del paquete Debian, en el host.
- **Motivo:**
  1. Admite varias versiones de un paquete por suite (FR-013, rollback con apt; verificado).
  2. Publica snapshots en un endpoint nuevo, lo que permite atomicidad por generación (D3).
  3. Genera `by-hash` (D6) y `Contents`.
  4. Firma con gpg2 y archivo de frase de paso.
- **Comparación con reprepro** (razón fuerte en contra):
  - **Versiones:** trixie trae `reprepro 5.4.6+really5.3.2`, es decir, 5.3.2 revertido. La
    opción `Limit` para guardar varias versiones de un paquete por distribución llegó en la
    serie 5.4. Con 5.3, cada distribución tiene una sola versión por paquete, así que no
    cumpliría FR-013 ni US4 sin trucos (una distribución por versión).
  - **Atomicidad:** reprepro modifica el árbol en sitio y no tiene snapshots. La atomicidad
    habría que construirla igual por fuera.
  - **A su favor:** su configuración (`conf/distributions`) es más simple que la base de datos
    de aptly.
- **Cómo se reduce el coste de aptly:** su base de datos (goleveldb) se trata como caché
  desechable (D3). La fuente de verdad es `repo/state.yaml` en git (FR-035).

### D2. Flujo por operación

- **Decisión:** cada `publish`, `retire` o `repo rebuild` calcula el estado nuevo y genera desde
  cero una **generación** del árbol:
  1. Raíz de aptly temporal `gen-<N>.aptly/`, con un repositorio local por suite.
  2. `repo add` desde el almacén de `.deb`.
  3. `snapshot create <suite>-g<N>`.
  4. `publish snapshot` de las 5 suites al endpoint `gen-<N>/`.
  5. Verificar.
  6. Cambio atómico (D3).
- **Motivo:** cumple el flujo pedido (repositorio local → snapshot → publish). Hace que el árbol
  sea función pura de `state.yaml` y del almacén (FR-036), así que no hay estado de aptly que
  pueda divergir.
- **Coste:** con los volúmenes previstos (4 paquetes × 3 versiones × 2 arquitecturas × 5
  suites), una generación son segundos.
- **Alternativa descartada:** `publish switch` en sitio.
  - Cambia cada suite por separado. Si falla a mitad (p. ej. firma), quedan suites nuevas y
    viejas mezcladas (contra FR-008).
  - Mantiene una base de datos de aptly de larga vida que hay que reconciliar con `state.yaml`.

### D3. Atomicidad local

- **Decisión:** `<data>/public/current` es un enlace simbólico a `gen-<N>/`.
- **Pasos de una operación:**
  1. Escribir `gen-<N>/` completo y firmado.
  2. Verificarlo como lo hace `repo verify` (firmas e índices contra archivos).
  3. Escribir `state.yaml.tmp`.
  4. Crear el enlace `current.tmp` y renombrarlo sobre `current` (`rename(2)` es atómico).
  5. Renombrar `state.yaml.tmp` sobre `state.yaml`.
  6. Borrar las generaciones anteriores a la previa (se guarda una para el periodo de gracia de
     D6).
- **Si se interrumpe:** antes del paso 4, `current` y `state.yaml` siguen en N-1. Entre 4 y 5,
  `repo verify` detecta que `gen-<N>/.state-sha256` no coincide con `state.yaml` y sugiere
  `repo rebuild` (FR-037).
- **Serialización:** `flock` sobre `<data>/lock`; una segunda orden falla de inmediato
  (FR-012).

### D4. Llave de firma

- **Decisión:**
  - **Llave maestra** ed25519, solo certificación (`[C]`), sin caducidad, con frase de paso
    fuerte. Vive solo en el respaldo.
  - **Subllave de firma** ed25519 `[S]` con caducidad de 2 años.
- **Cómo llega la subllave a la PC de build:**
  1. En una copia temporal del llavero maestro (en tmpfs), cambiar la frase de paso a una
     aleatoria propia de la PC de build.
  2. Exportar solo la subllave (`--export-secret-subkeys <sub>!`).
  3. Destruir la copia.
  4. En la PC, el llavero dedicado `~/.local/share/debfactory/gnupg/` (permisos `700`) tiene la
     subllave y el stub de la maestra. La frase de paso de build está en `passphrase` (`600`).
  5. aptly firma con `-passphrase-file`.
- **Motivo:**
  - Firma no interactiva, como pidió el mantenedor (FR-018a, FR-034).
  - La frase de paso de build no protege a la maestra.
  - ed25519 lo aceptan gpgv (bookworm) y sqv (trixie, sid, resolute) sin advertencias
    (verificado). Es más corto que rsa4096 y no hay razón de compatibilidad para preferir RSA.
- **Rotación:**
  - **Subllave:** se renueva la caducidad o se crea otra con la maestra. Se publica el keyring
    actualizado; el fingerprint de la maestra no cambia.
  - **Maestra (por pérdida o compromiso):** se publica un keyring con las dos maestras durante
    la transición. aptly firma con las dos subllaves (`-gpg-key` repetido; si 1.6.1 no lo
    admite, se cambia de llave tras el periodo de transición). Detalle en
    [operacion-llave.md](contracts/operacion-llave.md).
- **Alternativas descartadas:**
  - Subllave sin frase de paso: no es viable con GnuPG 2.4 (spike).
  - Frase de paso por el agente: es interactiva.
  - Token hardware: descartado en la aclaración.

### D5. Almacén de `.deb` publicados

- **Decisión:** `<data>/store/sha256/<aa>/<sha256>.deb`, direccionado por contenido. `publish`
  copia ahí el `.deb` de `dist/` tras verificar su hash. aptly lo importa con enlace duro.
- **Motivo:** `dist/` es un directorio de trabajo y un build nuevo puede reemplazar su contenido.
  Las versiones retenidas deben seguir disponibles.
- **Si falta un `.deb`** al regenerar, se busca en este orden: almacén, `dist/`, y por último se
  reconstruye desde el historial de git (manifiesto y lockfile en el commit que fijó esa
  versión) con `debfactory build`. Su sha256 debe coincidir con `state.yaml` (FR-036).

### D6. Sincronización: rclone

- **Decisión:** rclone 1.60.1 (Debian) para todos los destinos. Cada destino declara un
  *remote* de rclone; cambiar de proveedor es cambiar configuración (constitución IX):
  - Cloudflare R2 por el backend S3 (provider `Cloudflare`, con `no_check_bucket = true` y `no_head = true`: rclone 1.60.1 comprueba cada subida con `HEAD ?versionId=`, que R2 responde con 501 aunque el objeto ya se subió; la integridad la cubre `Content-MD5` en el PUT y `repo verify --dest`);
  - otro S3, como MinIO o AWS;
  - un directorio local;
  - un VPS por `sftp`.
- **Credenciales:** las define el mantenedor en `~/.config/rclone/rclone.conf` o en variables
  `RCLONE_CONFIG_<REMOTE>_*`, nunca en el repositorio. debfactory solo guarda el nombre del
  remote.
- **Orden de subida** (FR-026), con `rclone copy --skip-links --checksum`:
  1. `pool/**` y el keyring publicado, con
     `Cache-Control: public, max-age=31536000, immutable` para `pool/`.
  2. `dists/**/by-hash/**`, también inmutables.
  3. `dists/**` salvo `InRelease`, `Release` y `Release.gpg` (los `Packages*` sin hash),
     `max-age=60`.
  4. `Release` y `Release.gpg` de cada suite, `max-age=60`.
  5. `InRelease` de cada suite, `max-age=60`.
  6. Borrado diferido. En la sincronización N se borran los archivos de la generación
     sincronizada en N-2 (`previous_files`) que no están ni en la actual ni en la de N-1
     (`files`), es decir, `previous_files − (actual ∪ files)`. Así siempre queda una generación
     de gracia. Ambas listas se guardan en `<data>/sync/<destino>.json`.
- **Por qué by-hash es necesario:** el orden de subida no basta. Un cliente que tenga el
  `InRelease` viejo pediría `Packages` por nombre y recibiría el nuevo, con otro hash. Con
  `Acquire-By-Hash`, ese cliente pide `by-hash/SHA256/<hash-viejo>`, que sigue en el destino
  hasta la siguiente sincronización.
- **Atomicidad remota:** cada suite es atómica para sus clientes, porque `InRelease` es un único
  objeto. Entre suites no lo es, pero un cliente solo consulta una suite. Si la sincronización
  se interrumpe, se repite (FR-027); rclone solo sube lo que difiere.
- **Alternativas:**
  - aws-cli 2.23 (Debian): solo S3, y habría que programar aparte el destino local y el VPS.
  - Publicar directamente a S3 con aptly: ata el diseño al soporte S3 de aptly y no permite
    verificar antes de exponer.

### D7. Servidor local para pruebas: Caddy en contenedor

- **Decisión:** imagen `debfactory-repo-server`, construida desde la imagen de lintian
  (`debian:trixie`) con `apt install caddy`. Se reconstruye cada 7 días, como las otras.
  `caddy file-server` sirve el destino `local` en la red de Docker `debfactory-repotest`. Los
  clientes de prueba lo alcanzan como `http://repo/`.
- **Motivo:** Caddy está en Debian (IX) y sirve los mismos archivos estáticos que el hosting. Los
  encabezados `Cache-Control` no se prueban en local (Caddy no los pone), sino en `repo verify
  --dest r2`.
- **Alternativa:** `aptly serve` o un servidor en Go. Ambos son posibles, pero el mantenedor pidió
  Caddy y el servidor separado del generador refleja mejor el hosting real.

### D8. Verificación previa a publicar (FR-007)

- **Decisión:** para cada arquitectura de `config.arches`:
  - existe `dist/<name>_<ver>_<arch>.deb`;
  - `build.json` tiene `lintian: pass` y un `sha256` igual al del `.deb`;
  - `test.json` tiene `overall: pass` y un `deb_sha256` igual al del `.deb`.
- **Cambio en la fase 001:** se añade el campo `deb_sha256` a `test.json`. `debfactory test`
  calcula el hash del `.deb` que instala; hoy no lo registra.
- **Suites de la versión:** las de `test.json` con `status: pass`. MUST coincidir entre
  arquitecturas; las `excluded` no entran. Además, `test.json` MUST tener un resultado
  (`pass` o `excluded`) para **cada** suite de `config.suites`. Un `test.json` parcial (de
  `debfactory test --suite S`) no sirve para publicar, porque dejaría suites no excluidas sin
  probar (constitución VII). Así la publicación no necesita releer el manifiesto,
  y se puede publicar una versión anterior que siga en `dist/`.

### D9. Guía de usuario y sus pruebas

- **Archivo que publica el propio repositorio:** `debfactory.gpg`, el keyring binario con la(s)
  llave(s) maestra(s) públicas y sus subllaves.
- **El `.sources` lo escribe la guía, no el repositorio.** Si el repositorio publicara un
  `.sources` por suite, el árbol llevaría la URL dentro: dependería del destino y la prueba E2E
  no podría servirlo desde otra URL. Así, la URL solo vive en la guía y en la configuración, y
  el árbol es el mismo para todos los destinos.
- **Agregar el repositorio** son 4 comandos, sin deducir la suite:
  1. `apt-get install -y curl ca-certificates`
  2. `curl -fsSLo /etc/apt/keyrings/debfactory.gpg <url>/debfactory.gpg`
  3. `tee /etc/apt/sources.list.d/debfactory.sources` con un heredoc del deb822 (`<suite>` se
     escribe a mano según la tabla de suites)
  4. `apt-get update`
- **Verificar el fingerprint** es opcional y requiere `gnupg`:
  `gpg --show-keys /etc/apt/keyrings/debfactory.gpg`.
- **Pruebas:** la guía es `docs/usuario.md`. Sus bloques ejecutables llevan un info string
  `sh debfactory:<paso>` (`add`, `install`, `downgrade`, `remove`). La prueba E2E extrae cada
  bloque entero (el heredoc es parte del bloque), sustituye la URL pública por `http://repo` y
  `<suite>` por la suite del contenedor, y lo ejecuta como root con `sh -e`.
- **Coherencia:** un test de Go comprueba que la URL y el fingerprint escritos en la guía
  coinciden con la configuración (FR-030).

### D10. Pruebas E2E

- **Decisión:** se reutilizan las imágenes `debfactory-test/<suite>:<arch>` y el paquete
  `internal/container` de la fase 001. Los clientes van en la red `debfactory-repotest`, que
  tiene salida a Internet porque `curl` y las dependencias, como `ca-certificates`, vienen de
  los mirrors oficiales.
- **Matriz:** SC-001 cubre 5 suites × 2 arquitecturas (arm64 por qemu). SC-003 recorre el ciclo
  de vida en cada suite, en amd64.
- **Llave de prueba:** las pruebas generan su propio par maestra y subllave en un directorio
  temporal. Nunca usan la llave real.

### D11. Configuración del dominio

- **Decisión:** `repo.url` va en `config/debfactory.yaml` y se puede dejar vacío mientras no
  exista dominio. El árbol publicado no contiene la URL (D9), así que publicar no la necesita.
- **Comportamiento:**
  - `repo sync` a un destino con `cache_control: true` y `url` vacía falla.
  - Las pruebas usan el destino `local` y sustituyen la URL en los bloques de la guía.
  - La guía usa la URL configurada, o `https://apt.example.org` mientras esté vacía; el test de
    coherencia de D9 la compara.

## 3. Riesgos que se atacan primero

1. **Firma con varias subllaves** durante una rotación de la maestra. **Resultado (T049,
   `internal/repo/multikey_test.go`):** aptly 1.6.1 acepta `-gpg-key` repetido sin error, pero
   el `InRelease` lleva **una sola** firma. La rotación de la maestra usa, por tanto, la
   ventana: se publica primero el keyring con las dos maestras, se espera la transición y se
   cambia `repo.signing.subkey` al final. `repo.signing.subkey` sigue siendo un único valor.
2. **R2 con rclone 1.60.1:** subida multiparte de los `.deb` grandes (buf pesa unos 30 MB) y
   `--header-upload` por pasada. Se prueba contra un bucket de prueba antes del primer
   sincronizado público. **Resultado (T048, `just e2e-r2`, `internal/repotest/r2_test.go`,
   bucket `debfactory-apt-test` detrás de `apt-test.opentepache.org`):**
   - Pasa `repo sync`, luego `repo verify --dest` (60 archivos, firmas y encabezados), un
     segundo sync sin cambios (0 s) y un cliente trixie que agrega el repo por HTTPS con la
     guía e instala `fnm`. El primer sync de los 8 `.deb` tardó 1 min 1 s.
   - `buf` (33 MB) cabe por debajo del `upload_cutoff` de 200 MiB de rclone, así que se sube con
     un solo PUT y llega íntegro. No hace falta `--s3-upload-cutoff` ni otras opciones.
   - Con rclone 1.60.1 el remote necesita `no_head = true` (ver D6).
   - Cloudflare: `Packages.gz` y `.bz2` salían de la caché con `max-age=14400`, porque el
     *Browser Cache TTL* de la zona eleva el `max-age` del origen, y `InRelease` y los `.deb` no
     se cacheaban. Se resuelve con una Cache Rule para los hostnames del repo: *Eligible for
     cache*, *Edge TTL: use cache-control header*, *Browser TTL: respect origin*. Con ella
     todos los archivos se sirven desde la caché con los encabezados de debfactory. La regla
     está en quickstart.md §5.
3. **Tiempo de SC-001 con arm64 por qemu:** el objetivo es menos de 20 min para la matriz
   completa. **Resultado (T029):** 4 min 21 s con 4 clientes en paralelo; amd64 tarda de 10 a
   16 s por combinación y arm64 de 2 a 2,5 min, sobre todo por el `apt-get install curl` bajo
   qemu.
4. **Tiempo de `publish` (T024):** 3,6 s con 4 paquetes × 3 versiones ya publicados (objetivo
   orientativo: menos de 60 s).
