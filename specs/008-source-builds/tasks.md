---
description: "Task list for 008-source-builds"
---

# Tasks: Paquetes de nivel 2 (compilación desde el código fuente)

**Input**: Design documents from `/specs/008-source-builds/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/build.md, quickstart.md

**Tests**: se incluyen porque la spec define criterios verificables (SC-001–SC-005): binarios por
suite que funcionan, `apt-get source`, recompilación idéntica, nivel 1 intacto, pool sin nombres
repetidos. Tests Go junto al código; `integration` para git real; `e2e` para Docker. Lo que se hace
en los runners reales es **(manual)**.

**Organization**: la fase 1 atacó el mayor riesgo (sbuild `unshare` en Docker rootless, research D1); el spike llevó a D1′.
US4 (nivel 1 intacto) se comprueba en cada fase con las pruebas existentes y al final con la
reconstrucción completa.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia de usuario de la spec (US1–US4)

---

## Phase 1: Setup (spike y paquete de prueba)

- [X] T001 **(manual)** Spike de research D1: sbuild `--chroot-mode=unshare`, mmdebstrap y piuparts dentro de un contenedor (opciones por defecto, `apparmor=unconfined`, `seccomp=unconfined`). Resultado en `specs/008-source-builds/research.md` § D1 (Resultado del spike): no funcionan sin abrir seccomp/AppArmor y no caben en los 65 536 subuids del Docker rootless de `dfci`/`dfpub`; la alternativa (contenedor de la suite, deps con red, red desconectada, `dpkg-buildpackage`) funcionó sin privilegios, con binarios distintos por suite y doble build idéntico. Aprobada: research D1′; T011–T015 y T025 ajustadas.
- [X] T002 [P] Paquete de prueba (research D14): `testdata/fixtures/level2/src/tepache-hello-1.0.0/` con `hello.c` (imprime `OpenSSL_version(OPENSSL_VERSION)` enlazando con libcrypto; `--version` imprime la versión), `Makefile` y `LICENSE` (MIT); el tarball reproducible `testdata/fixtures/level2/tepache-hello-1.0.0.tar.gz` lo genera y vigila `TestFixturesLevel2` (`go test ./internal/build -run TestFixturesLevel2 -update`). Vive fuera de `testdata/project` para que los paquetes reales no cambien (SC-004).
- [X] T003 [P] Manifiesto de prueba `testdata/fixtures/level2/tepache-hello.yaml` (data-model §1, `level: 2`, `justification`, `source` del tarball del tag, `test: {command: tepache-hello, expect: OpenSSL}`), `testdata/fixtures/level2/tepache-hello/debian/{control,rules,copyright,source/format}` (`Build-Depends: debhelper-compat (= 13), libssl-dev`; `rules` con `dh $@`, ejecutable) y su lockfile `tepache-hello.lock.yaml` con `source_sha256`. El manifiesto excluye `forky`, `sid` y `resolute` con el motivo "acotar la prueba de nivel 2" y conserva bookworm, trixie y noble (noble cubre Ubuntu y el mirror `ports.ubuntu.com` de arm64). Las pruebas lo copian a su raíz temporal.

---

## Phase 2: Foundational

**Purpose**: manifiesto, lockfile, configuración y estado de nivel 2; todas las historias los usan.

- [X] T004 Esquema `schema/manifest.v1.schema.json` y tipos `internal/manifest/types.go`: `level` admite `1` o `2`; si `level: 2`, obligatorios `justification` ("texto no vacío (constitución II)", mínimo 20 caracteres) y `source` (`url` https con plantillas `{{version}}`/`{{tag}}`, `format`: "`tar.gz`, `tar.xz` o `tar.bz2`"); si `level: 1`, obligatorios `arch_map`, `artifact` e `install` como antes; opcional `build.tmpfs`. Los campos del otro nivel los rechaza `Check` con un mensaje claro (`semantic.go`, `levelFields`). En nivel 1 nada cambia (FR-014).
- [X] T005 Validación semántica en `internal/manifest/semantic.go` y `internal/manifest/debian.go` (FR-003): `source.url` dentro de los releases o del tarball del tag de `upstream`; `license.file.archive` solo con `tar.gz`; `build.tmpfs` ≤ `level2.tmpfs_max`; la carpeta `packages/<p>/debian/` existe con `control`, `rules` (ejecutable) y `copyright`, sin `changelog` ni enlaces simbólicos; `Source:` es `name`; `control` declara exactamente un `Package:` igual a `name` con `Architecture: any` (clarify, FR-003). Tests en `internal/manifest/level2_test.go` (sin justificación, sin `source`, con `artifact`/`install`, nivel 1 con `source`/`justification`, `debian/` incompleta, con `changelog`, enlace, `Source:` y `Package:` distintos, dos `Package:`, `Architecture: all`).
- [X] T006 [P] Lockfile en `internal/lock/`: `source_sha256` para nivel 2 (la fecha del release ya está en `source_date_epoch` desde la fase 001 y alimenta el changelog, FR-002); `debfactory lock` descarga el tarball, extrae y verifica la licencia, pasa el gate, calcula el sha256 y lo escribe (`internal/build/lockcmd.go`). Tests.
- [X] T007 [P] Configuración en `internal/config/config.go`: bloque `level2` opcional (`jobs` por defecto 3, `tmpfs` por defecto `6g`, `tmpfs_max` por defecto `12g`), `ParseSize`. Validación: `jobs ≥ 1`, tamaños con sufijo `g`/`m`, `tmpfs ≤ tmpfs_max`. Sin `mirror` por suite: el contenedor usa los repos oficiales que trae la imagen oficial de la suite (research D1′). Tests.
- [X] T008 Estado `schema: 2` en `internal/repo/state.go` (data-model §2, research D11): `Published.Level`, `Orig`, `Builds map[suite]Build{Version, DSC, DebianTar, Debs, Buildinfo}`; `suites` se sigue escribiendo y `Validate` exige que sea igual a las claves de `builds` en el orden de la configuración (el código existente lo usa); lectura de `schema: 1` sin cambios; escritura como 2 solo si hay alguna versión de nivel 2; cada `builds.<s>.version` es la base + `+<level2_suffix de s>`; ningún sha256 con dos nombres (SC-005). Fixtures `testdata/repo/state/{ok-level2,level2-*}.yaml` y pruebas de ida y vuelta.

**Checkpoint**: la herramienta acepta y valida manifiestos, lockfiles y estados de nivel 2; el nivel 1 sigue igual (`go test ./...`).

---

## Phase 3: User Story 1 - Declarar y construir (Priority: P1) 🎯 MVP

**Goal**: `debfactory build tepache-hello` produce fuente y binarios por suite y arquitectura, reproducibles, con lintian, prueba de ciclo de vida y prueba de instalación.

**Independent Test**: los `.deb` de bookworm y trixie (amd64 y arm64) dependen de `libssl3` y `libssl3t64` respectivamente, se instalan y ejecutan en su suite, y las dos compilaciones de cada uno son idénticas.

### Tests for User Story 1

- [X] T009 [P] [US1] Tests de `internal/srcpkg/` (sin Docker): changelog de cada suite exactamente como research D6 (versión con sufijo, distribución, maintainer de la configuración, fecha RFC 2822 de la publicación upstream); nombre del `.orig` (`<fuente>_<upstream>.orig.tar.<ext>`); orden con `dpkg --compare-versions` (`1.2.3-0~tepache1+deb12` < `1.2.3-1`; `+deb12` < `+deb13`; `0~tepache2+deb12` > `0~tepache1+deb13`) (principio VI).

### Implementation for User Story 1

- [X] T010 [US1] `internal/srcpkg/srcpkg.go`: prepara el directorio de entrada de una suite (tarball verificado como `.orig`, copia de `packages/<p>/debian/`, `debian/changelog` generado con la `date` del lockfile, `debian/source/format` si falta) y genera el paquete fuente **una sola vez por suite** con `dpkg-source -b` en un contenedor de construcción, con `SOURCE_DATE_EPOCH` del changelog (research D6). El `.dsc`, el `.orig` y el `.debian.tar.*` resultantes son los que se compilan y se publican (constitución II).
- [X] T011 [US1] Imágenes de construcción en `internal/srcbuild/srcbuild.go` (research D16): `debfactory-build/<suite>:<arch>` desde la imagen oficial de la suite con `build-essential fakeroot dpkg-dev debhelper xz-utils` precargados y el usuario sin privilegios `debfactory`, renovada cada 7 días con `container.EnsureImage` (se construye al primer uso; sin receta aparte).
- [X] T012 [US1] `internal/srcbuild/build.go` (research D1′, D2, D5): `Build(ctx, suite, arch, srcDir, outDir, opts)` arranca un contenedor desechable de la imagen de construcción `--platform linux/<arch>` con `--tmpfs /build:size=<tmpfs>,exec`, sin `--privileged` ni opciones de seguridad extra, sin montar `<data>` ni credenciales (solo la entrada de lectura con el `.dsc` de T010 y una salida); instala las `Build-Depends` con `mk-build-deps` (con red), desconecta la red (`container.DisconnectNetwork`) y comprueba que no hay, y corre `dpkg-source -x` en `/build/reproducible-path/` y `dpkg-buildpackage -b -us -uc` con `SOURCE_DATE_EPOCH` del changelog (nunca regenera el fuente); devuelve los `.deb`, `.buildinfo` y el final del log si falla (etapa `build`). Limitador propio con `level2.jobs`, amd64 primero (D3).
- [X] T013 [US1] Doble compilación en `internal/srcbuild/reproduce.go` (FR-008, D8): segunda compilación del mismo `.dsc` de T010 en la misma ruta fija con otro entorno (`umask 077`, `TZ`, `LANG`, hostname); compara los sha256 de todos los `.deb`; si difieren, etapa `compare` con los dos sha256.
- [X] T014 [US1] Despacho por nivel en `internal/build/build.go`: `level: 2` → fetch y verificación del tarball (`internal/fetch`, sha256 del lockfile), gate de licencias sobre el tarball verificado antes de compilar (FR-006), `srcpkg` + `srcbuild` + `reproduce` por suite no excluida y arquitectura, lintian sobre `.deb` y `.dsc`; escribe en `dist/` los archivos de contracts/build.md y un `build.json` por suite y arquitectura (sha256, lintian, reproducible). Flags `--suite`, `--arch`, `--check-reproducible` (en CI siempre). El nivel 1 sigue por el camino actual sin cambios.
- [X] T015 [US1] Pruebas por suite en `internal/installtest/`: para nivel 2, cada suite prueba su propio `.deb` (versión con sufijo) en contenedor limpio, y la prueba de ciclo de vida (research D9, en lugar de piuparts) en un contenedor limpio sin privilegios ni socket de Docker: lista de archivos y estado de dpkg antes, instalar, `apt-get remove`, `apt-get purge --autoremove`, sin archivos nuevos fuera de `/var/log`, `/var/cache`, `/var/lib/apt`, `/tmp`, sin el paquete en `dpkg -l` y sin fallos de scripts de mantenimiento (etapa `lifecycle`); resultados en el `test.json` de cada suite. Principio VII: un fallo en una suite no excluida falla la versión (FR-007).
- [X] T016 [US1] Test E2E en `internal/repotest/level2_test.go` (`TestE2ELevel2`, tag `e2e`, en la receta `e2e`): construye `tepache-hello` para bookworm, trixie y noble × amd64/arm64, comprueba con `dpkg-deb -f` que bookworm depende de `libssl3` y trixie y noble de `libssl3t64` y que cada versión lleva su sufijo, que las dos compilaciones son idénticas, y que cada binario pasa la prueba de instalación y de ciclo de vida en su suite (SC-001). Anota el tiempo (objetivo < 15 min, plan.md). La misma prueba cubre T024 y T027.

**Checkpoint**: el nivel 2 se construye y prueba en local y en el runner `ci`.

---

## Phase 4: User Story 2 - Publicar fuente y binarios (Priority: P1)

**Goal**: el repositorio publica por suite el `.dsc`, el `.orig` compartido, el `.debian.tar.*` y los `.deb`, con estado y auditoría completos.

**Independent Test**: en el repositorio de prueba, `apt-get install` y `apt-get source` de `tepache-hello` en bookworm y trixie dan la versión con el sufijo de cada suite.

### Tests for User Story 2

- [X] T017 [P] [US2] Tests en `internal/repo/` (tag `integration` donde usen aptly): `Inputs` de nivel 2 lee los archivos de cada suite y sus `test.json`; `Apply` agrega una versión de nivel 2 con `builds` y retención y retiro sobre la versión base; `Generate` publica la arquitectura `source` y el `.orig` es un único archivo en el pool para todas las suites (FR-010); `repo verify` falla si un nombre del pool tiene dos contenidos (SC-005).

### Implementation for User Story 2

- [X] T018 [US2] `internal/repo/inputs.go` y `apply.go`: entradas y operación `Add` de nivel 2 (data-model §2); almacén de `.dsc`, `.debian.tar.*` y `.orig` además de `.deb` (`internal/repo/store.go`).
- [X] T019 [US2] `internal/repo/generate.go` (research D10): `aptly repo add` por suite con sus `.deb` y su `.dsc`; publicación con `-architectures=amd64,arm64,source` (o `source` añadida a la lista de la configuración); verificación de que `Sources` existe y lista la versión con sufijo.
- [X] T020 [US2] `internal/repo/verify.go` y `internal/sync/verify.go`: comprobación "un nombre, un contenido" sobre el pool (SC-005) y de que cada `.dsc` publicado corresponde al `Sources` de su suite.
- [X] T021 [US2] Auditoría en `internal/ci/audit.go`: `publish`/`extend` de nivel 2 con `orig` y `builds` (data-model §3) en lugar de `debs`; `CheckAudit` cubre los sha256 de `builds`. Tests.
- [X] T022 [US2] Pipeline en `internal/ci/` (`run.go`, `manual.go`, `level2.go`): una versión de nivel 2 pasa por los gates de T014–T015 y se publica con el resto; la comparación de FR-010 y la de una ampliación usan las claves `<suite>/<arch>` y `<suite>/dsc` (`distShas`, `compareShas`); la ampliación a una suite nueva recompila todas las suites, exige que las publicadas salgan idénticas y publica solo la nueva (FR-015, research D12) con una entrada `extend` que lleva el `.orig` y los `builds` nuevos. Test de integración `internal/ci/level2_test.go` (tag `integration`, `citest.FakeDistLevel2` con `dpkg-source` real): publicar nivel 2 junto a nivel 1; dejar de excluir una suite con el gate fallando → pendiente y notificado; la ejecución siguiente amplía solo esa suite y cierra el aviso.
- [X] T023 [US2] Sitio en `internal/site/` (research D13): la página de un paquete de nivel 2 muestra la versión de cada suite (con sufijo) en la disponibilidad y el comando `apt-get source <p>`. Tests en `internal/site/render_test.go`.
- [X] T024 [US2] E2E (en `TestE2ELevel2`, `internal/repotest/level2_test.go`): publica `tepache-hello` en un repositorio de prueba firmado y lo sirve; en contenedores limpios de bookworm, trixie y noble (amd64 y arm64), `apt-get install tepache-hello` instala la versión `+deb12`/`+deb13`/`+u2404` y la ejecuta; `apt-get source tepache-hello` (con `deb-src` en el `.sources`) descarga el fuente de esa versión exacta, verificado por la firma de los índices (SC-002).

**Checkpoint**: fuente y binarios publicados y descargables por suite.

---

## Phase 5: User Story 3 - Recompilar y obtener el mismo binario (Priority: P2)

**Goal**: guía documentada y prueba automática que recompila desde `apt-get source` y obtiene el mismo sha256.

**Independent Test**: siguiendo la guía en un contenedor limpio de cada suite, el `.deb` recompilado tiene el sha256 publicado.

- [X] T025 [P] [US3] Guía `docs/recompilar-nivel2.md` en el repositorio de pruebas (`testdata/project/docs/`) y plantilla para `packages` (contracts/build.md § Guía): contenedor limpio de la suite y arquitectura (docker o podman), `apt-get source`, instalar las `Build-Depends` (o las versiones exactas del `.buildinfo` publicado), `dpkg-buildpackage -b` en `/build/reproducible-path/` con `SOURCE_DATE_EPOCH` del changelog y comparar el sha256; nota sobre el archivo histórico de la distribución para versiones antiguas (clarify).
- [X] T026 [US3] `.buildinfo` publicado (research D10): aptly no lo publica, así que `internal/repo/generate.go` lo copia a `pool/` junto a su `.deb` (`pool/.../<p>_<versión>_<arch>.buildinfo`), entra en `GenInfo.Files` (y así en la sincronización), su sha256 figura en el estado (data-model §2) y `repo verify` lo comprueba.
- [X] T027 [US3] E2E (en `TestE2ELevel2`): en un contenedor limpio de cada suite y arquitectura sigue `docs/recompilar-nivel2.md` tal cual (bloques `debfactory:srcadd`, `source`, `builddeps`, `rebuild`, `compare`) y compara con `cmp` el `.deb` recompilado con el publicado: 6 de 6 (bookworm, trixie y noble × 2 arquitecturas; SC-003).

**Checkpoint**: cualquiera puede verificar que el binario sale de ese fuente.

---

## Phase 6: User Story 4 - El nivel 1 no cambia (Priority: P1)

- [X] T028 [US4] Comprobación de convivencia: con la herramienta nueva, `bin/debfactory build --root testdata/project --all --check-reproducible` deja los `.deb` de nivel 1 con los mismos sha256 que antes de la fase y `repo/state.yaml` de prueba sigue en `schema: 1` mientras no haya nivel 2 publicado; test `TestLevel1Unchanged` en `internal/build/` que compara contra los sha256 conocidos de `testdata/project/repo/state.yaml` (SC-004).

---

## Phase 7: Polish & cierre

- [X] T029 [P] `docs/operacion-ci.md` § 9 "Paquetes de nivel 2": cuándo se justifica, la carpeta `debian/`, imágenes de construcción, tmpfs y `level2.jobs` (31 GB), tiempos de arm64 emulado, fallos típicos (red en el build, no reproducible, dependencia que falta en una suite → exclusión) y cómo recompilar.
- [X] T030 [P] `ci doctor` en `internal/ci/doctor.go` (`Level2Checks`): `level2.jobs × level2.tmpfs ≤ 18g` (data-model §4). Las imágenes de construcción no se comprueban: se construyen al primer uso en el Docker de cada runner. Test.
- [X] T031 [P] Notas en specs anteriores: `specs/001-manifest-deb-builder/` (nivel 2 llegó en la fase 008), `specs/002-signed-apt-repo/contracts/` (arquitectura `source`, estado `schema: 2`) y `specs/003-ci-pipeline/contracts/cli.md` (gates de nivel 2 y ampliación por suite).
- [X] T032 `just check` y `just e2e` en verde (incluidas las E2E de nivel 2 y la reconstrucción del nivel 1 con los mismos sha256).
- [ ] T033 **(manual)** `just tool-install`; PR en `packages` que fija la herramienta nueva y agrega el bloque `level2` a `config/debfactory.yaml` sin ningún paquete de nivel 2 (clarify: el primero llega aparte); comprobar que `verify` y `main` siguen sin cambios para los 5 paquetes publicados (SC-004 en producción).

---

## Dependencies & Execution Order

- T001 (spike) antes de T011–T013: si cambia D1, ajustar esas tareas.
- Fase 2 tras T002–T003; T004 → T005; T006, T007 y T008 en paralelo.
- US1 tras la fase 2: T009 → T010 → T011 → T012 → T013 → T014 → T015 → T016.
- US2 tras US1 (necesita `dist/` de nivel 2): T017 → T018 → T019 → T020 → T021 → T022 → T023 → T024.
- US3 tras US2: T025 y T026 → T027.
- US4: T028 en cualquier momento tras T014; se repite en T032.
- Polish al final; T033 después de T032.

### Parallel Opportunities

- T002 y T003; T006, T007 y T008.
- T009 con T011.
- T025 con T026; T029, T030 y T031.

## Implementation Strategy

1. Spike (T001) y paquete de prueba.
2. MVP: fases 2 y US1, es decir, construir y probar el nivel 2 en local y en el runner `ci`.
3. US2: publicar fuente y binarios; ampliación a suites nuevas.
4. US3: la recompilación verificable.
5. Comprobar US4, documentar y activar en `packages` sin paquetes de nivel 2.
