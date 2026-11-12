---
description: "Task list for 002-signed-apt-repo"
---

# Tasks: Repositorio APT firmado

**Input**: Design documents from `/specs/002-signed-apt-repo/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: se incluyen donde la spec los exige:

- las pruebas de aceptación ejecutan la guía (FR-032, SC-001, SC-002);
- los fallos provocados (SC-004);
- el ciclo de vida (SC-003, SC-008);
- la búsqueda de secretos (SC-005);
- la llave (SC-006);
- la reconstrucción (SC-009).

Los tests Go viven junto al código (`*_test.go`). Los que usan aptly o gpg reales llevan el build
tag `integration`. Los que usan Docker van en `internal/repotest` (tag `e2e`).

**Organization**: por historia de usuario. Cada fase deja algo comprobable por sí solo.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: se puede hacer en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia a la que pertenece (US1…US6)

## Path Conventions

Es el mismo módulo Go de la fase 001: `cmd/debfactory/`, `internal/<etapa>/`, `config/`,
`testdata/`, `justfile`. Lo nuevo es `internal/{repo,sign,sync,redact,repotest}/`, `docs/` y
`repo/state.yaml` (plan.md § Project Structure). Entorno: `PATH=/usr/lib/go-1.26/bin:$PATH`,
`GOTOOLCHAIN=local`. Herramientas del host: `aptly`, `rclone` y `gpg` (`sudo apt install aptly
rclone`).

Convenciones que usan varias tareas:

- **`<data>`**: la opción global `--data`, por defecto `~/.local/share/debfactory`. Contiene:
  - `gnupg/` (`700`) con `passphrase` (`600`);
  - `store/sha256/<aa>/<sha>.deb`;
  - `public/gen-<N>/`, `public/gen-<N>.json` y `public/current`;
  - `sync/<dest>.json`;
  - `lock`.
- **Versión publicada**: siempre la versión Debian completa `<upstream>-0~pav<N>`.
- **Llave de prueba**: ningún test usa la llave real. `testutil.TestKey(t)` crea maestra y
  subllave desechables.
- **Errores**: formato de la fase 001 (`internal/report`). Etapas nuevas: `state`, `inputs`,
  `generate`, `sign`, `verify-repo`, `swap`, `sync` y `lock-repo` (contracts/cli.md).

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: dependencias del host, configuración y esqueleto.

- [ ] T001 Añadir la sección `repo` a `config/debfactory.yaml` exactamente como en contracts/config.md:
  - `url: ""`, `origin` y `label` `debfactory`, `component: main`, `keep: 3`;
  - `signing.masters: []`, `signing.subkey: ""`, `signing.warn_days: 60`;
  - destinos `local` (`remote: /var/tmp/debfactory-repo`, `cache_control: false`) y `r2` (`remote: "r2:debfactory-apt"`, `public_url: ""`, `cache_control: true`);
  - los dos valores de `cache_control`.

  Copiar la misma sección al bloque de ejemplo de `specs/001-manifest-deb-builder/contracts/config.md`, con una línea que remita a 002.
- [ ] T002 [P] Crear `repo/state.yaml` vacío válido: cabecera `# Generado por debfactory publish/retire. No editar a mano.`, `schema: 1`, `serial: 0`, `packages: {}`, `retired: []`.
- [ ] T003 [P] Añadir al `justfile` estas recetas (contracts/cli.md § justfile):
  - `publish pkg version=""`, `retire pkg version reason`;
  - `sync dest`, que ejecuta `repo sync` y después `repo verify --dest`;
  - `serve`, `e2e` (`go test -tags e2e -run 'TestE2E(Repo|Lifecycle|Failures)' ./internal/repotest/ -timeout 40m`) y `e2e-key` (`-run TestE2EKey`).
- [ ] T004 [P] Actualizar `README.md`:
  - en Requisitos, añadir `aptly` y `rclone`;
  - añadir un componente **Repositorio** de un párrafo con sus órdenes;
  - enlazar `docs/usuario.md` y `docs/operacion-llave.md`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: estado, almacén, llave, bloqueo, filtrado de secretos y el cambio en `test.json`. Todo lo comparten US1–US6.

**⚠️ CRITICAL**: ninguna historia empieza antes de terminar esta fase.

- [ ] T005 Añadir a `internal/report/errors.go` las etapas `State`, `Inputs`, `Generate`, `Sign`, `VerifyRepo` (`"verify-repo"`), `Swap`, `Sync` y `LockRepo` (`"lock-repo"`). Añadir a `report.Error` un campo opcional `Suite` (FR-033): si no está vacío, se imprime tras la arquitectura (`<pkg> <ver> <arch> <suite>: <stage>: …`) y se incluye en `MarshalJSON`; los errores de la fase 001 no cambian. En `EnvError` (código 3), documentar que lo usan también las comprobaciones de aptly, gpg y rclone.
- [ ] T006 Ampliar `internal/config/config.go` con el tipo `Repo` (URL, Origin, Label, Component, Keep, Signing{Masters, Subkey, WarnDays}, Destinations map[string]Destination{Remote, PublicURL, CacheControl}, CacheControl{Immutable, Mutable}). Validar como en contracts/config.md:
  - `keep` ≥ 1;
  - fingerprints de 40 caracteres hex en mayúsculas;
  - nombre de destino `^[a-z][a-z0-9-]*$`;
  - `remote` sin `@`, `password`, `key=` ni `secret`.

  Ampliar `internal/config/config_test.go` con un caso por regla.
- [ ] T007 [P] Añadir el campo `DebSHA256 string \`json:"deb_sha256"\`` a `Report` en `internal/installtest/report.go`. `TestPackage` (`internal/installtest/package.go`) lo rellena con `fetch.SHA256File` del `.deb` antes de instalar (research D8). Actualizar el informe de ejemplo de `specs/001-manifest-deb-builder/data-model.md`.
- [ ] T008 [P] Crear `internal/redact/redact.go` con `Lines(b []byte, secrets ...string) []string`:
  - devuelve las líneas con `error|ERROR|fail` de la salida de un proceso;
  - sustituye por `***` cada `secret` literal y los valores tras `passphrase`, `secret_access_key`, `access_key_id` y `token`, sin distinguir mayúsculas;
  - nunca devuelve una línea que contenga `PRIVATE KEY`.

  Tests en `internal/redact/redact_test.go`.
- [ ] T009 Crear `internal/repo/state.go`. Tipos `State{Schema, Serial, Packages map[string][]Published, Retired []Retired}`, `Published{Version, Published time.Time, Suites []string, Debs map[string]string}` y `Retired{Package, Version, Date, Reason}`. Funciones:
  - `ReadState(root)` estricta;
  - `Validate(cfg)` con las 6 reglas y mensajes exactos de contracts/state.md;
  - `Write(root)` determinista y atómica, con la cabecera, paquetes en orden alfabético, versiones de mayor a menor con `dpkg --compare-versions` (reutilizar `installtest.compare` exportándolo como `installtest.CompareVersions`) y `retired` por fecha;
  - `SHA256()` del archivo serializado.

  Reglas de data-model.md §1 que se aplican literalmente:
  - `serial` "Sube en 1 con cada operación que cambia el estado";
  - `reason` "No vacío y de una línea";
  - "Ninguna `(package, version)` está a la vez en `packages` y en `retired`".
- [ ] T010 [P] Crear `internal/repo/state_test.go` con fixtures en `testdata/repo/state/`: `ok.yaml` y un archivo inválido por cada regla de contracts/state.md, cada uno con un comentario `# want:` con el mensaje esperado, como en `testdata/manifests/`. Añadir un test de ida y vuelta: leer, escribir y obtener bytes idénticos.
- [ ] T011 [P] Crear `internal/repo/store.go` con `Store{Dir}`:
  - `Put(src, sha)` verifica el sha256 y copia a `sha256/<aa>/<sha>.deb` de forma atómica;
  - `Path(sha)`;
  - `Has(sha)`.

  Test en `internal/repo/store_test.go`: un hash erróneo da error y no deja archivo.
- [ ] T012 [P] Crear `internal/repo/lock.go` con `Acquire(dataDir) (release func(), error)`. Usa `syscall.Flock` con `LOCK_EX|LOCK_NB` sobre `<data>/lock`. Si está ocupado: `lock-repo: otra operación está en curso sobre <data>` (FR-012). Test con dos adquisiciones.
- [ ] T013 Crear `internal/sign/keyring.go`:
  - `Check(cfg, gnupgHome)`, que devuelve `report.EnvError` en estos casos:
    - el directorio no existe o no tiene permisos `700`;
    - `passphrase` no tiene permisos `600`;
    - `gpg --with-colons -K` no muestra la `subkey` configurada como `ssb` con capacidad `s`;
    - la subllave no pertenece a una de `signing.masters`;
    - la maestra tiene secreto presente (`sec` en vez de `sec#`), con el mensaje `la llave maestra no debe estar en la PC de build (FR-018a)`.
  - `Expiry(subkey) (time.Time, error)`.
  - `ExportKeyring(masters, dst)`, que hace `gpg --export <masters…>` en binario.

  Todos los `gpg` corren con `GNUPGHOME=<data>/gnupg`, `--batch` y `--no-tty`, y su salida pasa por `redact`.
- [ ] T014 [P] Crear `internal/testutil/gpgkey.go` con `TestKey(t) (home, master, subkey string)`, que reproduce operacion-llave.md §1 en un `t.TempDir()`:
  - maestra ed25519 `cert never`;
  - subllave `ed25519 sign 2y`;
  - una copia temporal con `--passwd` para la frase de paso de build (con `--command-fd`, como en el spike de research §1);
  - `--export-secret-subkeys <sub>!`;
  - importar en `home` (`700`) y escribir `passphrase` (`600`).

  Test en `internal/sign/keyring_test.go` (tag `integration`): `Check` pasa con la llave de prueba, falla con `passphrase` en `644` y falla si se importa también la maestra secreta.

**Checkpoint**: estado, almacén, bloqueo y llave listos; `go test ./...` pasa.

---

## Phase 3: User Story 1 - Publicar los paquetes en un repositorio firmado (Priority: P1) 🎯 MVP

**Goal**: `debfactory publish <pkg> [<version>]` genera, firma, verifica y expone una generación completa del árbol a partir de `state.yaml`.

**Independent Test**: con la llave de prueba, publicar los 8 `.deb` de `dist/` en un `<data>` vacío y comprobar con `repo verify`:

- índices firmados por suite y arquitectura;
- `bob` ausente en bookworm;
- un solo archivo por `.deb` en `pool/`;
- republicar el mismo `.deb` no hace nada.

### Implementation for User Story 1

- [ ] T015 [US1] Crear `internal/repo/inputs.go` con `Inputs(root, cfg, name, version) (*Published, map[arch]debPath, error)`, según research D8 y los mensajes de contracts/cli.md § Etapas. Para cada arquitectura de `cfg.Arches`, comprobar en este orden:
  1. existe `dist/<name>_<version>_<arch>.deb`;
  2. `build.json` tiene `lintian: pass` y un `sha256` igual al sha256 real;
  3. `test.json` tiene `overall: pass` y un `deb_sha256` igual al sha256 real; si falta el campo, `vuelve a ejecutar \`debfactory test <pkg>\``;
  4. `test.json` tiene un resultado `pass` o `excluded` para **cada** suite de `cfg.Suites`. Si falta alguna, `inputs: test.json no cubre <suite>; ejecuta \`debfactory test <pkg>\` sin --suite` (constitución VII; research D8);
  5. suites = las de `test.json` con `status: pass`, que deben ser iguales entre arquitecturas.

  Sin `version`, se usa `manifest.DebVersion()` del manifiesto. Devuelve todos los errores, no solo el primero.
- [ ] T016 [P] [US1] Crear `internal/repo/inputs_test.go` con fixtures en `testdata/repo/inputs/`: `.deb` falsos de pocos bytes y `build.json`/`test.json` escritos a mano. Casos:
  - todo correcto;
  - falta arm64;
  - `lintian: fail`;
  - `overall: fail`;
  - sha distinto en `build.json`;
  - sha distinto en `test.json`;
  - falta `deb_sha256`;
  - `test.json` parcial, con solo `trixie` (como lo deja `test --suite trixie`);
  - suites distintas entre arquitecturas.

  Comparar el mensaje exacto de cada caso.
- [ ] T017 [US1] Crear en `internal/repo/state.go` la función `(s *State) Apply(op) (*State, []Change, error)` para la operación `Add{Package, Published}`:
  - mismo sha → sin cambios y sin error;
  - misma versión con otro sha → `state: ya publicada con otro sha256 (las versiones no se reescriben; sube pav)`;
  - versión en `retired` → `state: versión retirada el <fecha> (las versiones no se reutilizan; sube pav)`;
  - sube `serial`.

  `Change{Kind: add|remove|keep, Package, Version, Suites, Reason}` alimenta la salida de contracts/cli.md § Salida. La retención se añade en US4 (T035); de momento no se recorta nada.
- [ ] T018 [US1] Crear `internal/repo/generate.go` con `Generate(ctx, cfg, st, store, dataDir) (genDir string, error)`, según research D2:
  1. `gen-<serial>.aptly/` y `gen-<serial>/` en `<data>/public/`, el mismo sistema de archivos que el almacén, porque el enlace duro lo exige (research §1).
  2. `aptly.conf` temporal: `rootDir`, `gpgProvider: gpg2`, `architectures` de `cfg.Arches` y `FileSystemPublishEndpoints: {gen: {rootDir: gen-<N>, linkMethod: hardlink}}`.
  3. Por cada suite: `repo create -distribution=<suite> -component=main`, `repo add` de las rutas del almacén de las versiones de esa suite, y `snapshot create <suite>-g<N>`.
  4. `publish snapshot -batch -gpg-key=<subkey>! -passphrase-file=<data>/gnupg/passphrase -acquire-by-hash -origin -label -architectures=amd64,arm64 <suite>-g<N> filesystem:gen:.`, con `GNUPGHOME=<data>/gnupg`.
  5. `sign.ExportKeyring` a `gen-<N>/debfactory.gpg`.
  6. Borrar `gen-<N>.aptly/`.

  El árbol no contiene la URL del repositorio: el `.sources` lo escribe la guía (research D9).

  Un fallo de aptly se clasifica como `sign` si su salida menciona gpg o la firma, y como `generate` en otro caso, con el mensaje filtrado por `redact`. Ante cualquier fallo se borra `gen-<N>/`.
- [ ] T019 [US1] Crear `internal/repo/verify.go` con `VerifyTree(cfg, st, genDir) error`:
  - cada `dists/<suite>/InRelease` verifica con `gpgv --keyring <genDir>/debfactory.gpg` y su firmante es `signing.subkey`;
  - cada índice listado en `Release` existe con su sha256;
  - cada entrada de `Packages` apunta a un archivo de `pool/` cuyo sha256 es el de `state.yaml` para esa versión y arquitectura;
  - el conjunto de paquetes y versiones por suite y arquitectura es exactamente el de `state.yaml`, así que `bob` no está en bookworm;
  - no hay `Valid-Until` (FR-004a).
- [ ] T020 [US1] Crear `internal/repo/swap.go`:
  - `Commit(root, dataDir, st, genDir)` sigue el orden de research D3:
    1. escribir `gen-<N>.json` con `serial`, `state_sha256`, `files` (rutas regulares y sha256, sin enlaces simbólicos), `signing_subkey` y `created`;
    2. `state.yaml.tmp`;
    3. enlace `current.tmp` → `gen-<N>`, y `rename` sobre `current`;
    4. renombrar `state.yaml.tmp` sobre `state.yaml`;
    5. borrar las generaciones anteriores a la previa.
  - `Current(dataDir) (*GenInfo, error)`.
  - `CheckConsistent(root, dataDir)` falla con `verify-repo: current (serial X) no corresponde a repo/state.yaml (serial Y); ejecuta \`debfactory repo rebuild\`` (FR-037).
- [ ] T021 [US1] Crear `internal/repo/publish.go` con `Publish(ctx, env, names []string, versions map[string]string, dryRun bool) error`. Pasos:
  1. `lock.Acquire`;
  2. `sign.Check` (código 3 si falla);
  3. `ReadState` y `Validate`;
  4. `CheckConsistent`, salvo si `serial == 0` y no existe `current`;
  5. `Inputs` de cada paquete; si alguno falla, abortar listando todos los errores;
  6. `Apply`;
  7. si no hay cambios, imprimir `sin cambios` y salir con 0;
  8. imprimir los `Change`;
  9. con `--dry-run`, salir aquí;
  10. `store.Put` de cada `.deb`, `Generate`, `VerifyTree` y `Commit`;
  11. imprimir `repo: serial A → B; firmado con <subkey[:16]>…`.
- [ ] T022 [US1] Añadir a `cmd/debfactory/main.go`:
  - la opción global `--data`, cuyo valor por defecto sale de `os.UserHomeDir()` + `/.local/share/debfactory`;
  - la orden `publish <pkg> [<version>] [--all] [--dry-run]`.

  Validar el nombre del paquete como en `build`: sin `<pkg>` ni `--all` da uso incorrecto (2). Comprobar con `exec.LookPath` que existan `aptly` y `gpg`; si falta alguno, código 3. Ampliar `cmd/debfactory/main_test.go` con los códigos de salida de `publish`.
- [ ] T023 [US1] Añadir `repo verify` (sin `--dest`) a `cmd/debfactory/main.go`. Ejecuta `Validate`, `CheckConsistent` y `VerifyTree` sobre `current`. Por ahora sin la comprobación de la guía (T030) ni la de caducidad (T051).
- [ ] T024 [US1] Crear `internal/repo/publish_test.go` (tag `integration`, con aptly y gpg reales y `testutil.TestKey`). Fixtures: dos paquetes falsos construidos con `dpkg-deb -b` en `t.TempDir()`, más sus `build.json` y `test.json`; uno excluye bookworm. Comprobar:
  - US1-1: 5 suites × 2 arquitecturas, un solo archivo en `pool/` por `.deb` y `VerifyTree` correcto;
  - US1-2: el paquete con bookworm excluida no aparece ahí;
  - US1-3: un `test.json` con `overall: fail` deja `state.yaml` y `current` sin cambios;
  - US1-4: republicar es "sin cambios", y un `.deb` distinto con la misma versión falla con el mensaje de T017.

**Checkpoint**: MVP. `debfactory publish --all` sobre `dist/` real deja un repositorio firmado y verificado en `<data>/public/current`.

---

## Phase 4: User Story 2 - Agregar el repositorio e instalar siguiendo solo la documentación (Priority: P1)

**Goal**: guía de usuario y pruebas E2E que la ejecutan literalmente en las 5 suites × 2 arquitecturas.

**Independent Test**: `just e2e` (`TestE2ERepo`) con el repositorio de US1 servido por Caddy: 10/10 combinaciones sin advertencias, con las versiones correctas y sin rastro tras `remove`.

### Implementation for User Story 2

- [ ] T025 [P] [US2] Escribir `docs/usuario.md` con la estructura y los bloques `sh debfactory:<paso>` exactos de contracts/usuario.md, y estas condiciones:
  - `add` tiene 4 comandos;
  - la tabla de suites aclara que en sid `os-release` dice `forky`;
  - la tabla de paquetes dice que `bob` no está en bookworm y por qué;
  - incluye un hueco para el fingerprint con el texto exacto `Fingerprint: <pendiente>`, que se rellena en T055;
  - la URL de ejemplo es `https://apt.example.org` mientras `repo.url` esté vacío.
- [ ] T026 [P] [US2] Crear `internal/repotest/guide.go` con `Blocks(path) (map[string][]string, error)`:
  - extrae los bloques con info string `sh debfactory:<paso>`, cada uno como un script entero (el heredoc del `.sources` es parte del bloque);
  - quita `sudo ` al principio de cada línea;
  - sustituye `https://apt.example.org` (o la `repo.url` configurada) por la URL de prueba y `<suite>` por la suite.

  `CountAdd()` devuelve el número de comandos de `add`, contando cada heredoc como uno, que debe ser ≤ 4 (FR-031). Tests sin Docker en `internal/repotest/guide_test.go` sobre `docs/usuario.md`.
- [ ] T027 [US2] Crear `internal/repotest/server.go`:
  - `EnsureServerImage` construye `debfactory-repo-server` desde `cfg.Lintian.Image` con `apt-get install -y --no-install-recommends caddy` (máximo 7 días; `container.EnsureImage`);
  - `StartServer(ctx, dir)` crea la red `debfactory-repotest` si no existe y arranca `caddy file-server --root /srv --listen :80` con `dir` montado de solo lectura, con nombre de host `repo`;
  - `Stop`.

  `dir` es `<data>/public/current` resuelto, montado en el destino del enlace para que los enlaces duros funcionen.
- [ ] T028 [US2] Crear `internal/repotest/client.go` con `RunGuide(ctx, suite, arch, steps []string, st *repo.State) (*ClientResult, error)`:
  1. arranca `debfactory-test/<suite>:<arch>` (fase 001) en la red `debfactory-repotest` y en la red por defecto, porque `curl` y las dependencias vienen de los mirrors;
  2. ejecuta cada comando del paso con `sh -c`; es el único uso de shell, aceptable porque el texto sale de la guía del repositorio;
  3. guarda la salida de `apt-get update` y la del paso;
  4. tras `install`, comprueba con `dpkg-query -W` que cada paquete de `state.yaml` disponible en esa suite está en su versión más alta;
  5. tras `remove`, comprueba que no existen ni `.sources` ni keyring y que `apt-get update` no pide nada a `http://repo`.
- [ ] T029 [US2] Crear `internal/repotest/e2e_test.go` (tag `e2e`) con `TestE2ERepo`:
  1. publica en un `<data>` temporal, con la llave de prueba, los `.deb` reales de `dist/` (si faltan, `t.Skip` con el comando para generarlos);
  2. levanta el servidor;
  3. por cada suite × arquitectura, ejecuta `add`, `install` y `remove`;
  4. falla si la salida de `apt-get update` tiene líneas `W:`, `E:` o `N:` que mencionen `repo`.

  Escribe el tiempo por combinación y el total, que debe ser menor de 20 min (research §3, riesgo 3), y lo anota en research.md §3. Cubre SC-001 y SC-002.
- [ ] T030 [US2] Ampliar `repo verify` (`internal/repo/verify.go`, `VerifyGuide(root, cfg)`): `docs/usuario.md` contiene `repo.url` (o `https://apt.example.org` si está vacía) y cada fingerprint de `signing.masters` en el formato de 10 grupos de 4 que muestra `gpg`. Además, la tabla de paquetes por suite de la guía coincide con `state.yaml`: un paquete marcado como ausente de una suite no está publicado en ella, y viceversa. Test en `internal/repo/verify_test.go`.

**Checkpoint**: la guía funciona tal cual en las 10 combinaciones y no puede desactualizarse sin que falle `repo verify` o el E2E.

---

## Phase 5: User Story 3 - Publicar todo o nada (Priority: P1)

**Goal**: ningún fallo (entradas, firma, disco o interrupción) deja el repositorio a medias, y hay un camino de recuperación.

**Independent Test**: `just e2e` (`TestE2EFailures`) provoca cada fallo de quickstart §3 y comprueba después que `state.yaml`, `current` y un cliente servido siguen en el estado anterior.

### Implementation for User Story 3

- [ ] T031 [US3] Completar `publish --all` en `internal/repo/publish.go`:
  - toma la versión del manifiesto de cada `packages/*.yaml` (`build.ListPackages`);
  - acumula los errores de `Inputs` de todos los paquetes y, si hay alguno, no publica ninguno (US3-4);
  - sale con un único `Commit` para todos.
- [ ] T032 [US3] Implementar `repo rebuild` (`internal/repo/rebuild.go` y `cmd/debfactory/main.go`):
  1. lee `state.yaml`;
  2. obtiene cada sha del almacén; si no está, de `dist/` verificando el sha; si tampoco, devuelve el error `state: falta <name> <version> <arch> (sha …); reconstrúyelo con debfactory build` (la reconstrucción desde el historial de git va en T057);
  3. genera y verifica una generación con el mismo `serial` y hace `Commit` sin subir el `serial`.
- [ ] T033 [P] [US3] Crear `internal/repo/atomic_test.go` (tag `integration`). Casos:
  1. `Generate` falla por una subllave inexistente: `current` y `state.yaml` sin cambios y no queda `gen-<N>/`;
  2. falla en `VerifyTree` (un `Packages` alterado inyectado con un hook de test): ídem;
  3. un proceso con `Acquire` tomado hace que un segundo `Publish` falle de inmediato con `lock-repo`;
  4. `current` apunta a una generación con otro `state_sha256`: `CheckConsistent` falla y `repo rebuild` lo arregla;
  5. con `<data>/public` sin escritura (`chmod 555`): error `generate` y nada cambia.
- [ ] T034 [US3] Añadir a `internal/repotest/e2e_test.go` `TestE2EFailures`:
  1. publicar y servir;
  2. ejecutar las cuatro primeras filas de quickstart §3: mover el `.deb` arm64, `overall: fail`, `passphrase` en `644` (código 3) y subllave inexistente;
  3. tras cada una, comprobar que `state.yaml` no cambió (por sha256), que `current` apunta a la misma generación y que un cliente trixie amd64 con el repositorio agregado hace `apt-get update` sin errores (SC-004).

**Checkpoint**: SC-004 cubierto en el árbol local (el remoto se cubre en US5).

---

## Phase 6: User Story 4 - Actualizar, conservar versiones anteriores y retirar una defectuosa (Priority: P2)

**Goal**: retención configurable, `retire` atómico con registro en `state.yaml`, y upgrade y downgrade con apt.

**Independent Test**: `just e2e` (`TestE2ELifecycle`) recorre en cada suite: publicar v1 → instalar → publicar v2 → `apt upgrade` → retirar v2 → downgrade explícito. Además, con 5 versiones publicadas quedan 3.

### Implementation for User Story 4

- [ ] T035 [US4] Añadir la retención a `State.Apply` (`internal/repo/state.go`). Tras `Add`, para cada paquete y suite, conservar las `repo.keep` versiones más altas según `dpkg --compare-versions`; `-0~pav1` y `-0~pav2` cuentan como distintas. Las versiones que salen de todas sus suites se eliminan de `packages` con `Change{Kind: remove, Reason: "retención (keep N)"}`. Una versión que sale solo de algunas suites conserva las demás (Edge Cases, exclusión nueva). Si la versión nueva excluye una suite en la que estaba la anterior, emitir `Change{Kind: keep, Reason: "excluida en <versión nueva>; los usuarios de <suite> se quedan en <anterior>"}` como aviso en la salida.
- [ ] T036 [US4] Añadir la operación `Retire{Package, Version, Reason, Last bool}` a `State.Apply`. Reglas:
  - la versión debe estar publicada;
  - si es la única versión del paquete y `Last` es falso, error `state: es la única versión publicada de <pkg>; usa --last para retirarla`;
  - `reason` no vacío y de una línea;
  - quita la versión de `packages` y la añade a `retired` con la fecha UTC;
  - sube `serial`.
- [ ] T037 [P] [US4] Crear `internal/repo/retention_test.go` (unitario, sin aptly):
  - SC-008: con 5 versiones publicadas sucesivamente quedan exactamente 3;
  - `pav1` y `pav2` de la misma versión upstream cuentan como dos;
  - una versión que deja de excluir una suite entra en ella y las anteriores siguen fuera;
  - retirar la vigente deja como vigente la más alta restante;
  - retirar la única sin `--last` falla;
  - republicar una retirada falla con el mensaje de T017.
- [ ] T038 [US4] Implementar `retire <pkg> <version> --reason TEXTO [--last] [--dry-run]` en `internal/repo/publish.go` (`Retire`) y `cmd/debfactory/main.go`. Usa el mismo flujo atómico de T021: `Apply(Retire)`, `Generate`, `VerifyTree` y `Commit`. Sin `--reason`, uso incorrecto (2).
- [ ] T039 [US4] Proteger `lock --prune` en `internal/build/lockcmd.go` (`Prune`, FR-038): leer `repo/state.yaml` y, si una versión que se va a borrar del lockfile sigue publicada, fallar con el mensaje exacto de contracts/cli.md. Si `state.yaml` no existe, se comporta como en la fase 001. Añadir el caso a `internal/build/lockcmd_test.go`.
- [ ] T040 [US4] Añadir a `internal/repotest/e2e_test.go` `TestE2ELifecycle`. Prepara dos versiones de un paquete de prueba: el `.deb` de `fnm` reempaquetado con `dpkg-deb -R`/`-b` y `Version: 1.39.0-0~pav2`, con sus informes escritos por el test (técnica del spike, research §1). Con la llave de prueba, en cada suite (amd64):
  1. publicar v1, `add`, `install` y comprobar v1;
  2. publicar v2, ejecutar `upgrade` y comprobar v2;
  3. retirar v2, ejecutar `apt-get update` y el bloque `downgrade` con v1, y comprobar v1;
  4. intentar republicar v2: falla.

  Cubre SC-003.

**Checkpoint**: ciclo de vida completo con apt en las 5 suites.

---

## Phase 7: User Story 5 - Sincronizar con el hosting local y el público (Priority: P2)

**Goal**: `repo sync <destino>` con rclone en el orden de research D6 y borrado diferido, y `repo verify --dest`.

**Independent Test**: sincronizar al destino `local`, servirlo con Caddy y repetir `TestE2ERepo` contra él. Interrumpir una sincronización tras la fase `pool` y comprobar que el cliente sigue funcionando y que repetirla la completa.

### Implementation for User Story 5

- [ ] T041 [US5] Crear `internal/sync/plan.go` con `Plan(cur *GenInfo, last *Record) []Phase`, donde `Phase{Name, Files []string, CacheControl string}` sigue el orden exacto de research D6:
  1. `pool`: `pool/**` y `debfactory.gpg`;
  2. `by-hash`: `dists/**/by-hash/**`;
  3. `indices`: el resto de `dists/**`, salvo `Release`, `Release.gpg` e `InRelease`;
  4. `release`: `Release` y `Release.gpg`;
  5. `inrelease`: `InRelease`;
  6. `delete`: `last.previous_files − (cur.files ∪ last.files)`, es decir, lo de la generación sincronizada hace dos veces que ya no está ni en la actual ni en la última sincronizada (research D6).

  Cada fase lista solo los archivos nuevos o cambiados respecto a `last` (por sha256). Los enlaces simbólicos nunca entran (research §1). `pool/` y `by-hash/` usan `cache_control.immutable`; el resto, `mutable`; nada si `cache_control: false`.
- [ ] T042 [P] [US5] Crear `internal/sync/plan_test.go`:
  - primera sincronización: todas las fases y el orden exacto;
  - sincronización sin cambios: fases vacías, que se informan con `sin cambios`;
  - tras una retirada: los archivos del `.deb` retirado no se borran en la primera sincronización tras retirarlo y sí en la segunda (fórmula de T041);
  - los enlaces de `by-hash` con nombre de índice nunca aparecen.
- [ ] T043 [US5] Crear `internal/sync/rclone.go` con `Run(ctx, dest config.Destination, srcDir string, ph Phase)`:
  - escribe la lista de archivos en un temporal;
  - ejecuta `rclone copy --files-from <tmp> --skip-links --checksum --no-traverse [--header-upload "Cache-Control: …"] <srcDir> <remote>`;
  - en `delete`, ejecuta `rclone delete --files-from <tmp> <remote>`.

  La salida de rclone pasa por `redact`. Un error se informa como `sync: <dest>: rclone: <línea>`. Si falta `rclone`, código 3.
- [ ] T044 [US5] Crear `internal/sync/sync.go` con `Sync(ctx, env, destName, dryRun)`:
  1. si el destino tiene `cache_control: true` y `repo.url` está vacía, error (research D11);
  2. `lock.Acquire` y `repo.Current`;
  3. leer `<data>/sync/<dest>.json`;
  4. `Plan`;
  5. con `--dry-run`, imprimir las fases con su número de archivos y salir;
  6. ejecutar las fases en orden, emitiendo `{"event":"upload","phase":…,"files":N}` en `--json`;
  7. escribir `sync/<dest>.json` de forma atómica (`serial`, `files` y `previous_files`) solo al terminar `inrelease`.

  Si se interrumpe, el registro anterior sigue vigente y la siguiente sincronización resube lo que falta (FR-027). Añadir la orden `repo sync <destino> [--dry-run]` a `cmd/debfactory/main.go`.
- [ ] T045 [US5] Implementar `repo verify --dest <nombre>` en `internal/sync/verify.go`. Con `net/http`, User-Agent `debfactory` y la `public_url` del destino:
  - descarga `dists/<suite>/InRelease` de cada suite y verifica la firma con el `debfactory.gpg` remoto, cuyo fingerprint debe ser uno de `signing.masters`;
  - compara el sha256 de los índices con `current`;
  - comprueba por `HEAD` 5 archivos de `pool/` elegidos de forma determinista;
  - si `cache_control: true`, comprueba los encabezados `Cache-Control` de un `InRelease` y de un `.deb`.

  Para el destino `local`, `public_url` puede ser `http://localhost:8080` (`just serve`).
- [ ] T046 [US5] Implementar `just serve`: añadir la orden `repo serve [--port 8080]` en `cmd/debfactory/main.go`, que arranca `debfactory-repo-server` (T027) sirviendo `destinations.local.remote` en `127.0.0.1:<port>` hasta Ctrl-C, y hacer que la receta `serve` del `justfile` la invoque.
- [ ] T047 [US5] Añadir a `internal/repotest/e2e_test.go` `TestE2ESyncLocal`:
  1. sincronizar a un destino `local` temporal y servirlo;
  2. ejecutar `add` e `install` en trixie amd64;
  3. publicar una versión nueva;
  4. interrumpir la sincronización tras la fase `pool` (cancelando el contexto con un hook de test);
  5. comprobar que el cliente hace `apt-get update` sin errores y sigue viendo el estado anterior completo;
  6. repetir la sincronización y comprobar que el destino es idéntico a `current` (salvo los archivos pendientes de borrado diferido) y que el cliente ve la versión nueva.
- [ ] T048 [US5] Riesgo 2 de research §3 (manual, con credenciales del mantenedor): crear un bucket R2 de prueba y un remote `r2test`. Ejecutar `repo sync r2test` con los 8 `.deb` y luego `repo verify --dest r2test`. Comprobar que la subida multiparte de `buf` (~30 MB) funciona y que los encabezados `Cache-Control` llegan. Anotar el resultado en research.md §3 y, si rclone 1.60.1 necesita opciones extra (p. ej. `--s3-upload-cutoff`), ponerlas en contracts/config.md.

**Checkpoint**: el mismo código sirve para el directorio local y para S3; la consistencia remota está probada en local.

---

## Phase 8: User Story 6 - Custodiar, respaldar y rotar la llave de firma (Priority: P3)

**Goal**: procedimiento documentado y probado de creación, respaldo, restauración, renovación, rotación y revocación; aviso de caducidad; ausencia de secretos.

**Independent Test**: `just e2e-key` (`TestE2EKey`) recorre operacion-llave.md §1–§4 con una llave desechable y un cliente trixie que hace `apt-get update` en cada paso sin errores.

### Implementation for User Story 6

- [ ] T049 [US6] Riesgo 1 de research §3: comprobar con la llave de prueba si `aptly publish snapshot` 1.6.1 acepta `-gpg-key` repetido y si firma `InRelease` con ambas subllaves.
  - Si lo acepta, `repo.signing.subkey` pasa a ser una lista (`subkeys`) y `Generate` usa todas; actualizar contracts/config.md.
  - Si no, dejarlo así y documentarlo.

  En los dos casos, anotarlo en research.md §3 y en contracts/operacion-llave.md §5.
- [ ] T050 [US6] Escribir `docs/operacion-llave.md` desde contracts/operacion-llave.md: comandos completos de §1–§5, un llavero temporal en `/dev/shm`, dónde se guarda cada parte, la comprobación anual del respaldo y una lista de verificación final. Incluir explícitamente lo que nunca va al repositorio ni a la salida: llave maestra, subllave, frase de paso y certificado de revocación.
- [ ] T051 [US6] Añadir el aviso de caducidad (FR-023) en `internal/repo/publish.go`, `Retire` y `repo verify`. Si `sign.Expiry(subkey)` está a menos de `signing.warn_days` días, imprimir `aviso: la subllave de firma caduca el <fecha> (en <n> días)` y emitir `{"event":"warning",…}` en `--json`. No hace fallar. Si ya caducó, `sign: la subllave … caducó el <fecha>` hace fallar (código 1).
- [ ] T052 [US6] Hacer que `ExportKeyring` (`internal/sign/keyring.go`) exporte todas las `signing.masters` presentes en el llavero: durante una rotación de la maestra, el keyring publicado contiene las dos (FR-022). Test (tag `integration`) con dos llaves de prueba.
- [ ] T053 [US6] Crear `internal/repotest/key_test.go` (tag `e2e`) con `TestE2EKey`, que recorre operacion-llave.md con una llave desechable:
  1. crear y publicar; un cliente trixie hace `add`;
  2. respaldar, borrar `<data>/gnupg`, restaurar y publicar: mismo fingerprint, `apt-get update` correcto (US6-2);
  3. comprobar `sec#` en el llavero de build (US6-3);
  4. renovar la caducidad: `apt-get update` correcto;
  5. publicar el keyring con una subllave nueva sin cambiar la que firma; el cliente refresca el keyring con la línea de `add`; cambiar `subkey` y publicar: `apt-get update` correcto (US6-4);
  6. revocar la subllave vieja y publicar: correcto.

  Cubre SC-006.
- [ ] T054 [US6] Crear `internal/repotest/secrets_test.go` (tag `e2e`) para SC-005:
  - capturar stdout y stderr de todas las órdenes ejecutadas en los E2E (un `io.Writer` compartido);
  - buscar la frase de paso de prueba y `PRIVATE KEY`; además, pasar cada archivo versionado y cada salida capturada que parezca OpenPGP binario por `gpg --list-packets` y fallar si aparece `secret key packet` o `secret sub key packet`. Los fingerprints son públicos y no se buscan (SC-005);
  - hacer `git grep -I -E 'PRIVATE KEY|secret_access_key|passphrase:'` sobre el árbol y `git log -p` del historial;
  - en todos los casos, 0 coincidencias.
- [ ] T055 [US6] Generar la llave **real** siguiendo `docs/operacion-llave.md` (paso manual del mantenedor):
  - poner sus fingerprints en `repo.signing` (`config/debfactory.yaml`) y en `docs/usuario.md`, sustituyendo `Fingerprint: <pendiente>`;
  - ejecutar `repo verify`, que debe pasar con la comprobación de la guía (T030).

  Comprobar con `git status` que no se ha añadido nada de `<data>`.

**Checkpoint**: llave real creada, respaldada y probada; procedimiento verificado de punta a punta con una llave desechable.

---

## Phase 9: Polish & Cross-Cutting Concerns

- [ ] T056 [P] Implementar `repo status` en `cmd/debfactory/main.go` e `internal/repo/status.go`. Muestra:
  - `serial` y generación servida;
  - tabla de paquete × versión × suites;
  - el último `serial` sincronizado de cada destino (`sync/*.json`);
  - la caducidad de la subllave.
- [ ] T057 Completar `repo rebuild` con la reconstrucción desde git (FR-036, D5) en `internal/repo/rebuild.go`. Si un sha no está ni en el almacén ni en `dist/`:
  1. buscar con `git log --format=%H -- packages/<name>.yaml` el commit cuyo manifiesto tiene esa `version` y `pav`;
  2. `git worktree add` temporal en ese commit;
  3. `debfactory build <name> --arch <arch>` en él;
  4. comprobar que el sha256 coincide con `state.yaml` y meterlo en el almacén;
  5. borrar el worktree.

  Si el sha no coincide, el error es `state: <name> <version> <arch>: el build reproducible no coincide con state.yaml`.
- [ ] T058 Añadir a `internal/repotest/e2e_test.go` `TestE2ERebuildFromGit` (SC-009):
  1. clonar el repositorio en un temporal, sin `dist/` y con caché vacía;
  2. `--data` temporal con la llave de prueba restaurada;
  3. `repo rebuild`;
  4. comparar el sha256 de `pool/**` y de cada `Packages` con la generación original publicada por el mismo test. Solo pueden diferir `Release` e `InRelease`.
- [ ] T059 [P] Actualizar `specs/001-manifest-deb-builder/contracts/cli.md`: `test.json` lleva `deb_sha256`, `lock --prune` tiene la guarda de FR-038, y un enlace a `specs/002-signed-apt-repo/contracts/cli.md` para las órdenes del repositorio.
- [ ] T060 Ampliar la receta `check` del `justfile` con:
  - `go test -tags integration ./internal/repo/ ./internal/sign/`;
  - `bin/debfactory repo verify` si existe `<data>/public/current`.

  Los E2E siguen fuera de `check`, que se queda en unos 25 min, y se ejecutan con `just e2e`.
- [ ] T061 Recorrer quickstart.md §1–§7 a mano con los paquetes reales, salvo §5 con R2 si T048 aún no se hizo. Corregir lo que no coincida y marcar en plan.md un "Re-check (post-implementación)" del Constitution Check, como en la fase 001.
- [ ] T062 Primera publicación real:
  1. `publish --all`;
  2. revisar el diff de `repo/state.yaml` y hacer commit;
  3. `just sync r2` con el dominio definitivo en `repo.url`;
  4. desde una máquina limpia, seguir `docs/usuario.md` contra la URL pública;
  5. con una versión nueva de un paquete ya construido y probado, medir `publish` más `just sync r2`, que debe ser menor de 5 min (SC-007), y anotarlo en research.md §3.

  Requiere el dominio (research D11) y la llave real (T055).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: sin dependencias.
- **Foundational (Phase 2)**: depende de Setup; bloquea todas las historias.
- **US1 (Phase 3)**: depende de Foundational. Es el MVP.
- **US2 (Phase 4)**: depende de US1 (necesita un repositorio publicado que servir).
- **US3 (Phase 5)**: depende de US1. T034 reutiliza el servidor y el cliente de US2 (T027, T028).
- **US4 (Phase 6)**: depende de US1. T040 reutiliza el servidor y el cliente de US2.
- **US5 (Phase 7)**: depende de US1. T047 reutiliza el servidor y el cliente de US2.
- **US6 (Phase 8)**: T049–T052 dependen de US1; T053 depende de US2; T055 debe hacerse antes de T062.
- **Polish (Phase 9)**: T057 y T058 dependen de T032; T062 depende de todo.

### User Story Dependencies

- US1 → base de todas.
- US2 → aporta la infraestructura E2E (servidor y cliente) que usan US3, US4, US5 y US6.
- US3, US4 y US5 son independientes entre sí una vez hechos US1 y US2.
- US6 solo necesita US1 para el código (T049–T052) y US2 para su E2E.

### Within Each User Story

- Estado y entradas antes que generación; generación antes que verificación y swap; el swap
  antes que la orden de la CLI; la CLI antes que los E2E.
- Los tests unitarios marcados [P] pueden escribirse junto a su implementación.

### Parallel Opportunities

- Setup: T002, T003 y T004.
- Foundational: T007, T008, T010, T011, T012 y T014, tras T005, T006 y T009 según dependan.
- US1: T016 en paralelo con T017 y T018.
- US2: T025 y T026 en paralelo.
- US3, US4 y US5, una vez terminados US1 y US2, en paralelo entre sí.
- Polish: T056 y T059.

---

## Parallel Example: User Story 1

```text
Task: "T016 [US1] inputs_test.go con fixtures en testdata/repo/inputs/"
Task: "T018 [US1] generate.go: aptly temporal, snapshots por suite, publish a filesystem:gen"
```

## Parallel Example: tras US2

```text
Task: "T031–T034 [US3] todo o nada y repo rebuild"
Task: "T035–T040 [US4] retención, retire y ciclo de vida"
Task: "T041–T047 [US5] rclone y borrado diferido"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 y Phase 2.
2. Phase 3 (US1): `publish` genera un repositorio firmado y verificado en `<data>/public/current`.
3. Parar y validar: `repo verify` y una prueba manual con `just serve`.

### Incremental Delivery

1. US1 → repositorio firmado local.
2. US2 → guía de usuario probada en 10 combinaciones (lo mínimo para que alguien lo use).
3. US3 → garantías de atomicidad.
4. US4 → ciclo de vida (upgrade, retención, retiro).
5. US5 → hosting público.
6. US6 → llave real con procedimiento probado → primera publicación (T062).

---

## Notes

- Sin commits automáticos: el mantenedor los pide. `repo/state.yaml` se revisa en el diff como
  los lockfiles.
- La llave real y las credenciales de R2 solo se usan en T048, T055 y T062, que son pasos
  manuales. El resto de tests usa llaves y destinos desechables.
- Ninguna tarea debe imprimir el contenido de `<data>/gnupg` ni de `rclone.conf`; toda salida
  de procesos externos pasa por `internal/redact`.
