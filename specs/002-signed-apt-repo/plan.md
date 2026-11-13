# Implementation Plan: Repositorio APT firmado

**Branch**: `002-signed-apt-repo` | **Date**: 2026-11-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/002-signed-apt-repo/spec.md`

## Summary

El binario `debfactory` gana las órdenes `publish`, `retire`, `repo sync`, `repo verify`,
`repo rebuild` y `repo status`.

- **Fuente de verdad:** `repo/state.yaml`, versionado en git.
- **Cada operación:**
  1. Comprueba que los `.deb` de la fase 001 tengan lintian y la prueba de instalación
     aprobados, con el mismo sha256.
  2. Calcula el estado nuevo, con la retención de 3 versiones.
  3. Genera desde cero, con aptly, una **generación** completa del árbol: repositorio local →
     snapshot por suite → `publish snapshot` a un endpoint de archivos nuevo, con
     `Acquire-By-Hash`, firmado por una subllave ed25519.
  4. La verifica.
  5. La expone con un cambio atómico de enlace simbólico.
- **Sincronización:** rclone, a cualquier destino (directorio local, R2, otro S3 o un VPS por
  sftp). Orden de subida: pool → by-hash → índices → `Release` → `InRelease`, y el borrado
  diferido una sincronización.
- **Clientes:** usan un `.sources` deb822 con `Signed-By: /etc/apt/keyrings/debfactory.gpg`.
  El repositorio publica el keyring; el `.sources` lo escribe la guía, para que el árbol no
  dependa de la URL (research D9). Agregarlo son 4 comandos.
- **Pruebas E2E:** levantan Caddy en contenedor y ejecutan los bloques de `docs/usuario.md` en
  las 5 suites × 2 arquitecturas.
- **Spike** ([research.md](research.md) §1): confirmó el flujo de aptly, la firma con subllave y
  archivo de frase de paso, la aceptación sin advertencias en bookworm, trixie y resolute, y el
  downgrade con apt. También descartó reprepro: trixie trae 5.3.2, sin varias versiones por
  suite.

## Technical Context

**Language/Version**: Go 1.26 (el mismo módulo `debfactory` de la fase 001), `GOTOOLCHAIN=local`.

**Primary Dependencies**: las de la fase 001, más tres herramientas externas de Debian trixie:

- aptly 1.6.1: generación de índices, snapshots, by-hash y firma;
- gnupg 2.4.7: firma, la usa aptly;
- rclone 1.60.1: sincronización.

En pruebas, Caddy 2.6.2 en contenedor. Sin bibliotecas Go nuevas: aptly, gpg y rclone se
invocan como procesos, y `state.yaml` usa `go.yaml.in/yaml/v3`.

**Storage**: archivos.

- **En git:** `repo/state.yaml`.
- **Fuera de git** (`~/.local/share/debfactory`):
  - `gnupg/`: llavero con la subllave y la frase de paso `600`;
  - `store/`: `.deb` por sha256;
  - `public/gen-<N>/` y `current`;
  - `sync/<destino>.json`;
  - `lock`.
- **Destinos:** donde diga rclone.

**Testing**: `go test`.

- **Unitarias:** estado y retención, entradas, plan de subida, filtrado de secretos y
  extracción de bloques de la guía.
- **Integración** con aptly y gpg reales y una llave desechable (build tag `integration`).
- **E2E** con Docker (`just e2e`, `just e2e-key`).

**Target Platform**: la PC Debian trixie amd64 de la fase 001. Clientes: bookworm, trixie, sid,
noble y resolute, en amd64 y arm64.

**Project Type**: CLI de un solo binario (amplía la fase 001).

**Performance Goals**: son orientativos, no un SLA.

- `publish` de un paquete con 4 paquetes × 3 versiones ya publicados: menos de 60 s.
- `repo sync` incremental: sube solo lo que cambió; una versión nueva son unos pocos MB.
- `just e2e` (SC-001): menos de 20 min con arm64 por qemu.
- SC-007: publicar y sincronizar en menos de 5 min.

**Constraints**:

- Nada se publica sin lintian y la prueba aprobados (VII).
- Las versiones no se reescriben ni se reutilizan (VI).
- Ningún secreto en git ni en la salida (III).
- Las órdenes no son interactivas (VIII).
- Proveedor de hosting por configuración (IX).
- Los índices no caducan (FR-004a).

**Scale/Scope**: 4 paquetes iniciales, pensados para decenas; 3 versiones por paquete; 5
suites; 2 arquitecturas; 2 destinos (local y R2). Fuera de alcance: CI, detección de versiones,
sitio web, nivel 2 y espejos.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principio | Cómo lo cumple el diseño | Estado |
|---|---|---|---|
| I | Declarativo primero | Publicar no necesita datos por paquete: las suites salen de `test.json`, que a su vez sale de las exclusiones del manifiesto. Ningún paquete tiene código ni configuración de repositorio propios. | ✅ |
| II | Dos niveles | Solo se publica nivel 1: un mismo `.deb` en todas sus suites, compartido en `pool/`. El esquema de `state.yaml` admite añadir luego el sufijo de suite del nivel 2 (un `.deb` por suite) sin romper el formato. No se publican paquetes fuente. | ✅ |
| III | Cadena de suministro | Índices firmados (`InRelease` y `Release.gpg`) con una llave dedicada. La maestra está fuera de línea y la subllave en un llavero fuera de git, con la frase de paso en un archivo `600`. La salida de gpg, aptly y rclone se filtra. Respaldo, restauración y rotación están documentados en [operacion-llave.md](contracts/operacion-llave.md). `publish` exige que el sha256 del `.deb` coincida con los informes de build y de prueba, y `repo verify` comprueba firmas y hashes. | ✅ |
| IV | Gate de licencias | Sin cambios. Solo se publica lo que pasó el build de la fase 001, que incluye el gate. | ✅ |
| V | No duplicar | Sin cambios: los manifiestos ya documentan `value`. La guía solo ofrece los paquetes publicados. | ✅ |
| VI | Versionado | `state.yaml` impide reescribir una versión (mismo nombre con otro sha256) y reutilizar una retirada. La retención ordena con `dpkg --compare-versions`, y las pruebas E2E comprueban upgrade y downgrade con apt. | ✅ |
| VII | Reproducible y verificable | Gate previo: `build.json` con `lintian: pass`, `test.json` con `overall: pass` y el mismo sha256. Si falla en cualquier arquitectura o suite no excluida, no se publica en ninguna (FR-007), y `publish --all` es todo o nada. El árbol es función de `state.yaml` y de los `.deb` reproducibles (SC-009). | ✅ |
| VIII | Automatización | Todavía no hay camino automático (CI fuera de alcance). El diseño lo deja listo: órdenes no interactivas (firma con archivo de frase de paso), `--json` con eventos, códigos de salida estables y mensajes con paquete, etapa y causa. | ✅ (no aplica aún) |
| IX | Simplicidad operativa | aptly, gnupg, rclone y caddy están empaquetados en Debian trixie. Hosting S3-compatible (R2) con proveedor, endpoint y bucket en el remote de rclone; un VPS es otro remote. Cada orden se explica en una línea ([cli.md](contracts/cli.md)). La base de datos de aptly es desechable, así que no hay estado que operar. | ✅ |
| X | La IA propone | Ningún componente usa IA. La llave y las credenciales nunca están al alcance de un job. | ✅ |

**Gate (pre-research)**: PASS. **Re-check (post-design)**: PASS contra la constitución v1.5.0,
sin desviaciones (la tabla de complejidad está vacía).

**Re-check (post-implementación, 2026-11-12)**: PASS. Lo que cambió al implementar, sin tocar
ningún principio:

- III: aptly 1.6.1 acepta `-gpg-key` repetido pero firma con una sola subllave (T049). Por eso
  la rotación de la maestra se hace por ventana (keyring doble y cambio de subllave al final;
  docs/operacion-llave.md §5) y `repo.signing.subkey` sigue siendo un único valor.
- III y VII: el gate de entradas exige además que `test.json` cubra las 5 suites; un informe
  parcial de `test --suite` no permite publicar.
- VII: E2E en verde. Guía en 5 suites × 2 arquitecturas (10/10, 4 min 21 s); fallos
  provocados sin cambios en el repositorio; ciclo publicar → upgrade → retirar → downgrade en
  las 5 suites; sincronización interrumpida; procedimiento de la llave; búsqueda de secretos.
- IX: además de las órdenes de docker de la fase 001, las pruebas usan `network create`,
  `network inspect` y `--publish` (red de pruebas y `repo serve`). Siguen valiendo docker.io de
  Debian y docker-ce.
- El registro de sincronización guarda los borrados pendientes, para que repetir `repo sync`
  con la misma generación nunca borre antes de tiempo.

**Re-check (recorrido del quickstart con los paquetes reales, 2026-11-13, T061)**: PASS contra la
constitución v1.6.0:

- VI: versiones `<upstream>-0~tepache<N>` (campo `revision` del manifiesto). Origin y Label son
  OpenTepache, y `repo verify` lo comprueba en cada Release.
- III: primera publicación firmada con la subllave real. `repo verify --dest r2` pasa, y la guía
  funciona en un contenedor limpio de cada suite contra `apt.opentepache.org`.
- VII: SC-009 a mano. Un clon nuevo, sin `dist/` ni caché, regenera el mismo `pool/` y los
  mismos `Packages` (121 s). Solo cambian Release e InRelease, más el enlace
  `by-hash/*/Contents-<arch>.gz` que aptly deja apuntando al mismo archivo y que no se sube.
- §4 no se pudo recorrer con una versión nueva real (los 4 paquetes están en la última versión
  upstream); lo cubre `TestE2ELifecycle`.
- Corregido al recorrerlo:
  - la guía hace `apt-get update` antes de instalar curl, porque en una máquina sin índices de
    APT fallaba;
  - `repo verify --dest` espera hasta 2 min a que un destino HTTP deje de servir la generación
    anterior, porque justo después de `sync` fallaba sin motivo;
  - en el quickstart: el orden de las opciones globales (§7) y la búsqueda de secretos (§5).

Notas del re-check:

- **III, frase de paso en archivo:** equivale a una subllave sin protección. Se acepta porque
  el mantenedor pidió firma no interactiva, la maestra está fuera de línea y una subllave
  comprometida se revoca sin cambiar el fingerprint. La spec (FR-018a, FR-034 y Assumptions) ya
  lo refleja.
- **III y VII, sincronización:** el orden de subida no basta para la consistencia remota; por
  eso se usan `Acquire-By-Hash` y el borrado diferido (research D6, FR-026 actualizado).

## Project Structure

### Documentation (this feature)

```text
specs/002-signed-apt-repo/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md                # publish, retire, repo …
│   ├── config.md             # sección repo de config/debfactory.yaml
│   ├── state.md              # repo/state.yaml
│   ├── usuario.md            # estructura de docs/usuario.md y bloques ejecutables
│   └── operacion-llave.md    # creación, respaldo, rotación y revocación
└── tasks.md                  # /speckit-tasks
```

### Source Code (repository root)

```text
cmd/debfactory/               # + publish, retire, repo {sync,verify,rebuild,status}
internal/
├── config/                   # + sección repo (validación de contracts/config.md)
├── installtest/              # + deb_sha256 en test.json (research D8)
├── build/                    # + guarda de lock --prune contra state.yaml (FR-038)
├── repo/
│   ├── state.go              # leer, validar y escribir state.yaml; retención; retiros
│   ├── inputs.go             # verificación previa de .deb, build.json y test.json (D8)
│   ├── store.go              # almacén por sha256
│   ├── generate.go           # aptly: conf temporal, repo add, snapshot, publish y keyring
│   ├── verify.go             # firmas (gpgv con debfactory.gpg), índices contra archivos, guía
│   ├── swap.go               # gen-N.json, enlace current, limpieza de generaciones
│   └── lock.go               # flock de <data>/lock
├── sign/                     # llavero dedicado: permisos, fingerprints, caducidad, export del keyring
├── sync/                     # plan de subida por fases, rclone, borrado diferido, Cache-Control
├── redact/                   # filtro de secretos para la salida de procesos externos
└── repotest/                 # E2E: Caddy, clientes por suite y arquitectura, bloques de la guía, llave desechable
docs/
├── usuario.md                # guía del usuario final (contracts/usuario.md)
└── operacion-llave.md        # procedimiento de la llave (contracts/operacion-llave.md)
repo/
└── state.yaml                # estado publicado (en git)
testdata/repo/                # states válidos e inválidos y test.json de casos negativos
justfile                      # + publish, retire, sync, serve, e2e, e2e-key
```

**Structure Decision**: el mismo módulo y binario de la fase 001, con paquetes `internal/` por
etapa: `repo` (estado, generación y swap), `sign`, `sync` y `repotest`. Cada etapa se mapea a un
`stage` de error ([cli.md](contracts/cli.md)). Los datos publicados (`repo/state.yaml`) y la
guía (`docs/`) quedan separados del código.

## Complexity Tracking

Sin violaciones que justificar.

## Riesgos que se atacan primero

Del [research §3](research.md#3-riesgos-que-se-atacan-primero):

1. Si aptly 1.6.1 admite varias `-gpg-key`, que hace falta para rotar la llave maestra.
2. R2 con rclone 1.60.1: subida multiparte y encabezados por pasada, contra un bucket de prueba.
3. El tiempo de la matriz E2E con qemu.

Los tres van en las primeras tareas de sus fases, antes de construir el resto.
