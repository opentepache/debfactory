# Feature Specification: Nivel 2 con toolchains de upstream, compilación hospedada y cadena de suministro endurecida

**Feature Branch**: `009-hosted-hardened-builds`

**Created**: 2026-11-27

**Status**: Draft

**Input**: User description: "Fase 009: compilar paquetes de nivel 2 que necesitan un toolchain y
dependencias de upstream, en runners de GitHub, con la cadena de suministro endurecida. Primer
paquete real: ghostty 1.4.0." (texto completo en la conversación del 2026-11-27).

## Contexto

La fase 008 compila paquetes de nivel 2 en la PC, con el compilador y las librerías de los
repositorios oficiales de cada suite. Hay programas que no caben en ese modelo: ghostty necesita un
compilador (Zig 0.17) que ninguna suite trae y ~40 dependencias que su propio gestor de paquetes
descarga, y compilarlo en `arm64` emulado tarda una hora. La constitución 1.10.0 ya permite excluir
arquitecturas por paquete y compilar el nivel 2 en runners gratuitos de GitHub; esta fase lo
implementa, agrega toolchains y dependencias de upstream verificadas, y endurece la cadena de
suministro para todo lo que viene de upstream.

**Spike (2026-11-27)**: ghostty 1.4.0 compila sin red en trixie/amd64 en 6 min (11,8 GB de RAM) y en
`arm64` emulado en 61 min; el binario es idéntico en 7 compilaciones; la firma minisign del tarball se
verifica; las 39 dependencias de Zig dan el hash declarado si se bajan una por una (las vías de
upstream `zig build --fetch` y `fetch-zig-cache.sh` no sirven); Zig 0.17.1 falla; noble no tiene
`gtk4-layer-shell` y bookworm tiene GTK 4.8.

## Clarifications

### Session 2026-11-27

- Q: ¿Con qué servicio de IA se genera el resumen del cambio entre versiones del PR? → A: GitHub
  Models con el `GITHUB_TOKEN` del job (permiso `models: read`), sin secretos ni costo; si se agota
  el límite o no está disponible, el PR lo indica y no bloquea.
- Q: ¿En qué suites se publica ghostty? → A: trixie y forky; sid, bookworm y noble excluidas con su
  motivo (sid por sus transiciones frecuentes de librerías; bookworm y noble por GTK, libadwaita o
  `gtk4-layer-shell` insuficientes).
- Q: ¿Cómo se declaran los archivos del tarball que no están en el tag de git? → A: por ruta exacta,
  con motivo, y con su sha256 fijado en el lockfile; si el archivo cambia en otra versión, vuelve a
  revisión. Sin patrones.
- Q: ¿La llave fijada y la procedencia son obligatorias también en nivel 1? → A: sí, siempre que
  upstream firme o publique procedencia; en esta fase se fijan en fnm, bob, buf y aqua. Si upstream
  no firma, basta el lockfile y se informa.

### Decisiones posteriores (2026-11-27, constitución 1.11.0)

- Modelo de cómputo: GitHub compila el nivel 2 y ejecuta la matriz de pruebas de todos los niveles y
  todo lo de las PR; la PC verifica de forma independiente (atestaciones, doble compilación,
  lintian, reconstrucción del nivel 1, prueba de humo amd64), firma y publica. El runner propio de
  PR (`ci`) se retira; queda solo `pub`.
- Transparencia: la herramienta (`opentepache/debfactory`) se hace pública; cada versión tiene una
  página de procedencia en el sitio; existe `debfactory verify` para terceros; los commits a `main`
  de `packages` van firmados.
- Organización: se mantiene el monorepo `packages`, con cada paquete autocontenido en
  `packages/<p>/` (manifiesto, lockfile, `debian/`) para poder separarlo en el futuro.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Publicar un paquete que necesita un toolchain de upstream (Priority: P1)

El mantenedor declara ghostty como paquete de nivel 2 con su toolchain de upstream (Zig 0.17.0) y
sus opciones de compilación en la carpeta `debian/`; la herramienta verifica el toolchain y todas las
dependencias del lenguaje antes de compilar, compila sin red en cada suite y arquitectura no
excluida, y la versión se publica tras las pruebas y la verificación independiente de la PC.

**Why this priority**: es el objetivo de la fase; sin esto ghostty no se puede publicar.

**Independent Test**: con el manifiesto de ghostty 1.4.0, el pipeline publica `ghostty` en trixie
y forky para amd64 y arm64; `apt install ghostty` en un contenedor limpio
instala la versión con el sufijo de la suite y `ghostty +version` dice `1.4.0`.

**Acceptance Scenarios**:

1. **Given** el manifiesto de ghostty con su toolchain y sus dependencias en el lockfile, **When** se
   compila, **Then** el toolchain y cada dependencia se verifican contra el lockfile y los hashes
   declarados por upstream, la compilación corre sin red, y el binario resultante es idéntico en las
   dos compilaciones.
2. **Given** sid, bookworm y noble excluidas con su motivo, **When** se publica, **Then** ghostty
   está en trixie y forky y no aparece en las excluidas, sin bloquear las demás.
3. **Given** una dependencia del lenguaje cuyo contenido no da el hash declarado, o un toolchain con
   otro sha256, **When** se compila, **Then** la versión falla antes de compilar, con el nombre de la
   dependencia o del toolchain y los dos valores.

---

### User Story 2 - GitHub compila y prueba; la PC verifica, firma y publica (Priority: P1)

Las compilaciones de nivel 2 y la matriz de pruebas (lintian, instalación y ciclo de vida por suite y
arquitectura, de todos los niveles) corren en paralelo en runners estándar gratuitos de GitHub, sin
secretos, con atestación de cada artefacto e informe. La PC del mantenedor solo acepta lo que tiene
atestación válida, verifica de forma independiente (doble compilación idéntica, lintian,
reconstrucción del nivel 1 con el mismo sha256, prueba de humo de instalación en amd64) y entonces
firma y publica. Las PR corren enteras en GitHub y el runner propio de PR se retira.

**Why this priority**: sin runners nativos, `arm64` tarda horas por versión y la carga de pruebas
crece con cada paquete y suite; además saca de la PC la ejecución de código de upstream y de PR, y
reduce la infraestructura propia a un solo runner.

**Independent Test**: tepache-hello (en una PR de prueba, sin integrar: nunca va a producción) y
ghostty (integrado y publicado desde la PC) se compilan y prueban en los runners de GitHub; un artefacto sin atestación, con atestación de otro workflow o
commit, o con compilaciones distintas, no se publica.

**Acceptance Scenarios**:

1. **Given** un paquete de nivel 2 en 3 suites y 2 arquitecturas, **When** corre el pipeline,
   **Then** sus 12 compilaciones corren en paralelo en runners estándar gratuitos y ninguna recibe
   secretos.
2. **Given** los artefactos y los informes de prueba, **When** la PC los recibe, **Then** verifica
   la atestación (repositorio, workflow y commit esperados), que las dos compilaciones de cada
   combinación sean idénticas, lintian, la reconstrucción del nivel 1 y una prueba de humo en
   amd64 antes de firmar; si algo falla, no se publica en ninguna suite.
4. **Given** GitHub no disponible, **When** el mantenedor lo indica, **Then** la PC construye, prueba
   y publica todo localmente como en la fase 008.
3. **Given** un paquete que no cabe en un runner estándar en una arquitectura, **When** el
   mantenedor la excluye en el manifiesto con su motivo, **Then** se publica en las demás.

---

### User Story 3 - Excluir y recuperar una arquitectura (Priority: P2)

Un manifiesto puede excluir una arquitectura con su motivo, igual que una suite; el paquete se
publica en las demás y el sitio y la guía muestran en qué arquitecturas está. Al quitar la exclusión,
la siguiente ejecución amplía la versión vigente a esa arquitectura sin reescribir lo publicado.

**Why this priority**: es la salida para paquetes que no caben en un runner o no compilan en una
arquitectura; sin ella, un problema de arm64 bloquea amd64.

**Independent Test**: tepache-hello con `arm64` excluido se publica solo en amd64; al quitar la
exclusión, la siguiente ejecución publica arm64 y amd64 no cambia.

**Acceptance Scenarios**:

1. **Given** `arm64` excluido con motivo, **When** se publica, **Then** solo hay binarios amd64,
   la regla "falla en una, no se publica en ninguna" ignora arm64, y el sitio muestra "solo amd64"
   con el motivo.
2. **Given** una versión publicada sin arm64, **When** se quita la exclusión, **Then** la ejecución
   siguiente agrega arm64 a la versión vigente y los archivos de amd64 no cambian.
3. **Given** un manifiesto que excluye todas las arquitecturas, **When** se valida, **Then** falla.

---

### User Story 4 - Detectar un upstream comprometido antes de compilar (Priority: P1)

Antes de compilar una versión, la herramienta comprueba que lo que publicó upstream es coherente con
su repositorio y con su identidad conocida: el tarball de release no trae archivos que no estén en el
tag de git (salvo excepciones declaradas), la firma es de la llave fijada en el manifiesto, y la
procedencia publicada por upstream (si la hay) corresponde a su repositorio y tag.

**Why this priority**: es la defensa contra los ataques reales más conocidos (tarball con puerta
trasera que no está en git, cuenta robada que firma con otra llave, binario que no corresponde al
código); el mantenedor lo pidió explícitamente.

**Independent Test**: con fixtures de prueba: un tarball con un archivo extra respecto del tag, uno
firmado con otra llave y uno con procedencia de otro repositorio fallan antes de compilar; el tarball
correcto pasa.

**Acceptance Scenarios**:

1. **Given** un tarball de release con un archivo que no está en el tag de git, **When** se prepara
   la versión, **Then** falla con la lista de archivos añadidos o distintos, salvo los declarados en
   el manifiesto con su motivo (p. ej. archivos generados por upstream al empaquetar).
2. **Given** un manifiesto con la llave de firma de upstream fijada, **When** la release está firmada
   con otra llave o sin firma, **Then** la versión falla y requiere revisión humana.
3. **Given** un upstream que publica procedencia (atestaciones o SLSA), **When** la procedencia no
   corresponde al repositorio y tag declarados, **Then** la versión falla; si upstream no publica
   procedencia, se registra como ausente y no falla.

---

### User Story 7 - Cualquiera puede verificar lo publicado (Priority: P1)

Un usuario o auditor puede comprobar, sin confiar en el mantenedor ni en GitHub, que una versión
publicada sale de código y procesos públicos: el sitio muestra su procedencia completa y una orden
de la herramienta (pública) repite la verificación.

**Why this priority**: es la base de la confianza en un repositorio de terceros; el mantenedor lo
pidió explícitamente.

**Independent Test**: para ghostty 1.4.0 y fnm, la página de procedencia enlaza el commit del
manifiesto, el run, la atestación en el registro público, el `.buildinfo` (nivel 2) y los
resultados; `debfactory verify ghostty 1.4.0-0~tepache1+deb13` en una máquina limpia termina con
"verificado" y un `.deb` alterado da "no verificado".

**Acceptance Scenarios**:

1. **Given** una versión publicada, **When** se abre su página de procedencia, **Then** muestra el
   origen upstream y sus verificaciones (firma, procedencia, tarball contra tag), el commit del
   manifiesto, el run, la atestación con enlace al registro público, el `.buildinfo` (nivel 2), los
   resultados de pruebas por suite y arquitectura y su entrada de auditoría.
2. **Given** `debfactory verify <paquete> <versión>` en una máquina sin acceso especial, **When** se
   ejecuta, **Then** verifica la firma del repositorio, la atestación y reconstruye (nivel 1) o
   recompila (nivel 2) para comparar el sha256; cualquier diferencia da "no verificado" con el
   motivo.
3. **Given** un commit sin firma en `main` de `packages`, **When** se intenta integrar, **Then**
   GitHub lo rechaza (regla de rama) y `ci doctor` lo comprueba.

---

### User Story 5 - Señales de aviso para la revisión humana (Priority: P3)

Al aceptar un paquete y en cada actualización, el PR muestra señales que ayudan al mantenedor a
decidir: la salud del proyecto upstream (OpenSSF Scorecard) y un resumen del cambio entre versiones
que señala lo sospechoso. No bloquean; informan.

**Why this priority**: ayudan a la revisión, pero no son verificaciones con resultado binario.

**Independent Test**: un PR de actualización de un paquete muestra la puntuación de Scorecard y un
resumen del diff entre versiones con una sección de cambios sospechosos (o "ninguno").

**Acceptance Scenarios**:

1. **Given** un PR que agrega o actualiza un paquete, **When** corre la verificación, **Then** el PR
   muestra la puntuación de Scorecard de upstream y sus comprobaciones más débiles.
2. **Given** una actualización, **When** se genera el resumen, **Then** lo produce un job sin secretos
   (principio X), lista endpoints de red nuevos, código ofuscado, binarios dentro del fuente y cambios
   en scripts de build o de instalación, y no aprueba ni rechaza nada.

---

### User Story 6 - Lo existente no cambia (Priority: P1)

Los paquetes de nivel 1 y tepache-hello se siguen construyendo, probando y publicando igual; el nivel
1 sigue en la PC.

**Why this priority**: constitución VI y VII; una fase nueva no puede romper lo publicado.

**Independent Test**: `just check` y `just e2e` en verde; los `.deb` de nivel 1 con los mismos sha256.

**Acceptance Scenarios**:

1. **Given** los paquetes publicados, **When** se ejecuta `main` con la herramienta nueva, **Then** no
   hay cambios en sus versiones ni en sus archivos.

### Edge Cases

- Upstream re-sube un tarball con el mismo nombre y otro contenido: el sha256 del lockfile falla.
- El sitio de dependencias de upstream sirve un archivo distinto del declarado: falla el hash; no se
  compila con una caché parcial.
- Un runner de GitHub se queda sin memoria, disco o tiempo: esa combinación falla, no se publica en
  ninguna; el mantenedor puede excluir la arquitectura con su motivo.
- GitHub no está disponible o la cuota se agota: la ejecución falla y se reintenta; nada se publica a
  medias.
- Un artefacto llega con atestación de un PR o de otro repositorio: se rechaza.
- El repositorio de paquetes deja de ser público: las compilaciones de nivel 2 en runners de GitHub
  quedan deshabilitadas (constitución IX) y la herramienta lo detecta y lo informa.
- Upstream cambia de llave legítimamente: el mantenedor actualiza la llave fijada por PR tras
  verificarla por otro canal.
- El tarball de release trae archivos generados (p. ej. scripts de configure) que no están en git: se
  declaran uno a uno en el manifiesto con su motivo.
- Una dependencia transitiva aparece solo dentro de otra dependencia: la resolución la encuentra.
- Las dependencias declaradas en ejemplos o pruebas de upstream que no forman parte del build no se
  bajan.

## Requirements *(mandatory)*

### Functional Requirements

**Toolchain y dependencias de upstream**

- **FR-001**: Un manifiesto de nivel 2 MUST poder declarar un toolchain de upstream (nombre, versión
  y URL oficial por arquitectura); su sha256 por arquitectura MUST quedar en el lockfile y
  verificarse antes de usarlo; si upstream publica firma o checksum del toolchain, también se
  verifica.
- **FR-002**: La herramienta MUST resolver el cierre transitivo de las dependencias declaradas por el
  gestor de paquetes del lenguaje (primero Zig: los archivos de dependencias del árbol del fuente y
  de cada dependencia ya obtenida, excluyendo ejemplos que no forman parte del build), obtener cada
  una por separado y exigir que dé exactamente el hash declarado por upstream.
- **FR-003**: El lockfile MUST registrar cada dependencia del lenguaje (URL, hash declarado, sha256
  del archivo); generarlo es una orden explícita (`lock`), nunca el build; cualquier diferencia
  posterior MUST hacer fallar la versión.
- **FR-004**: La compilación MUST usar solo la caché de dependencias verificada, sin red; la caché se
  prepara una vez por versión y sirve a todas las suites y arquitecturas.
- **FR-005**: Las opciones de compilación de cada paquete MUST vivir en su carpeta `debian/`
  estándar; la herramienta MUST NOT tener casos especiales por paquete.
- **FR-027**: Cada dependencia del lenguaje MUST pasar el gate de licencias (principio IV): su
  licencia se detecta en su propio árbol, MUST estar en la allowlist y se lista en
  `debian/copyright`; si alguna no pasa, la versión falla antes de compilar.

**Arquitecturas**

- **FR-006**: El manifiesto MUST admitir `exclude.arches` con motivo; MUST quedar al menos una
  arquitectura; las excluidas no cuentan para "falla en una, no se publica en ninguna" ni se prueban.
- **FR-007**: Quitar la exclusión de una arquitectura MUST ampliar la versión vigente a esa
  arquitectura en la ejecución siguiente, sin cambiar lo ya publicado; si falla, queda pendiente,
  notificado y se reintenta.
- **FR-008**: El estado, la auditoría, el sitio y la guía de usuario MUST reflejar las arquitecturas
  de cada versión y el motivo de las exclusiones.

**Compilación hospedada (constitución IX)**

- **FR-009**: Las compilaciones de nivel 2 MUST correr en runners estándar gratuitos de GitHub, una
  por suite, arquitectura y compilación de la doble compilación, en paralelo, dentro del contenedor
  oficial de la suite, sin secretos y con permisos de solo lectura.
- **FR-010**: Cada artefacto MUST llevar una atestación de procedencia; la PC MUST verificarla
  (repositorio, workflow y commit esperados) y que las dos compilaciones sean idénticas antes de
  usarlo.
- **FR-011**: La matriz de pruebas (lintian, instalación y ciclo de vida por suite y arquitectura no
  excluida, de nivel 1 y nivel 2) MUST correr en runners estándar de GitHub (`arm64` nativo), con
  un informe atestado por combinación.
- **FR-011a**: Antes de firmar, la PC MUST verificar de forma independiente: atestaciones de
  artefactos e informes, doble compilación de nivel 2 idéntica, lintian sobre todos los `.deb`,
  reconstrucción local del nivel 1 con el mismo sha256 y una prueba de humo de instalación en amd64
  por paquete. Solo la PC firma, publica y sincroniza.
- **FR-011b**: Con GitHub no disponible o el repositorio no público, la PC MUST poder construir,
  probar y publicar todo localmente (modo de la fase 008), a petición del mantenedor.
- **FR-012**: Si el repositorio de paquetes deja de ser público, la herramienta MUST negarse a usar
  runners de GitHub y decirlo; se usa el modo local (FR-011b).
- **FR-013**: Las PR MUST correr enteras en runners de GitHub (compilación, pruebas, informe de
  cadena de suministro, Scorecard y resumen), sin publicar; el runner propio de PR (`ci`) se retira
  y la documentación de operación lo refleja.

**Cadena de suministro**

- **FR-014**: Para nivel 2, la herramienta MUST comparar el contenido del tarball de release con el
  del tag de git de upstream; archivos añadidos o distintos MUST hacer fallar la versión salvo los
  declarados en el manifiesto por ruta exacta (sin patrones) y con su motivo; el sha256 de cada
  archivo declarado MUST quedar en el lockfile, y si cambia en una versión nueva la excepción deja de
  valer hasta que se revise.
- **FR-015**: Si upstream firma sus releases (minisign, GPG o identidad de sigstore), el manifiesto
  MUST fijar su llave o identidad, en nivel 1 y en nivel 2; la validación MUST avisar si upstream
  firma y el manifiesto no la fija. Con la llave fijada, una release sin firma o firmada con otra
  llave MUST hacer fallar la versión. Si upstream no firma, el lockfile basta y el informe lo dice.
- **FR-016**: Si upstream publica procedencia (atestaciones de GitHub o SLSA) para el artefacto, en
  nivel 1 o nivel 2, la herramienta MUST verificar que corresponde al repositorio y tag declarados;
  si no la publica, lo registra como ausente sin fallar.
- **FR-016a**: En esta fase, fnm, bob, buf y aqua MUST quedar con su llave o identidad de firma y su
  procedencia fijadas cuando su upstream las publique, sin cambiar sus `.deb` publicados.
- **FR-017**: Al agregar o actualizar un paquete, el PR MUST mostrar (sin bloquear) la puntuación de
  OpenSSF Scorecard de upstream y sus comprobaciones más débiles.
- **FR-018**: Al actualizar un paquete, el PR MUST mostrar (sin bloquear) un resumen del cambio entre
  versiones generado por un job sin secretos que señale endpoints de red nuevos, código ofuscado,
  binarios dentro del fuente y cambios en scripts de build o instalación. El modelo es GitHub
  Models, autenticado con el `GITHUB_TOKEN` del job con solo `contents: read` y `models: read`; si
  se agota el límite de uso o el servicio falla, el PR lo indica y nada se bloquea.
- **FR-019**: La guía de usuario MUST explicar qué garantiza OpenTepache (integridad, procedencia,
  reproducibilidad, retiro rápido) y qué no (que el proyecto upstream sea de fiar).

**Transparencia (constitución 1.11.0, VII)**

- **FR-022**: El repositorio de la herramienta MUST ser público antes de usarla en runners de GitHub
  (acción del mantenedor); los runners la compilan desde el commit fijado en `debfactory.version`.
- **FR-023**: Cada versión publicada MUST tener en el sitio una página de procedencia con: origen
  upstream y resultado de firma, procedencia y tarball contra tag; commit del manifiesto; run;
  atestación con enlace al registro público de transparencia; `.buildinfo` (nivel 2); resultados de
  pruebas por suite y arquitectura; entrada de auditoría.
- **FR-024a**: La guía de recompilación de nivel 2 MUST incluir el paso del toolchain (leer
  `debian/upstream/toolchain` del paquete fuente, descargarlo, verificar su sha256 y su firma y
  ponerlo en el `PATH`), y una prueba automática MUST seguirla con un paquete que use toolchain.
- **FR-024**: `debfactory verify <paquete> <versión> [--suite S --arch A]` MUST poder ejecutarlo un
  tercero sin credenciales: verifica la firma del repositorio, la atestación y reconstruye (nivel 1)
  o recompila (nivel 2, siguiendo la guía) para comparar el sha256; informa "verificado" o "no
  verificado" con el motivo.
- **FR-025**: Los commits a `main` de `packages` MUST ir firmados (mantenedor y App), exigido por una
  regla de rama; `ci doctor` MUST comprobarlo.
- **FR-026**: Cada paquete MUST quedar autocontenido en `packages/<p>/` (manifiesto, lockfile,
  `debian/`), sin dependencias de otros paquetes salvo la configuración y el estado comunes.

**Primer paquete y compatibilidad**

- **FR-020**: ghostty 1.4.0 MUST publicarse en trixie y forky, para amd64 y arm64; sid, bookworm y
  noble MUST quedar excluidas en su manifiesto con su motivo (sid: transiciones frecuentes de
  librerías; bookworm y noble: GTK, libadwaita o `gtk4-layer-shell` insuficientes).
- **FR-021**: Los paquetes de nivel 1 y tepache-hello MUST seguir construyéndose, probándose y
  publicándose igual que antes de la fase (mismos sha256 de nivel 1).

### Key Entities

- **Toolchain**: nombre, versión, URL y sha256 por arquitectura; opcionalmente firma de upstream.
- **Dependencia del lenguaje**: URL, hash declarado por upstream, sha256 del archivo, de qué archivo
  de dependencias sale.
- **Exclusión de arquitectura**: arquitectura y motivo, en el manifiesto.
- **Llave fijada de upstream**: tipo (minisign, GPG, sigstore) y valor, en el manifiesto.
- **Excepciones de tarball**: archivos del tarball que no están en el tag (o difieren), cada uno por
  ruta exacta con motivo en el manifiesto y sha256 en el lockfile.
- **Artefacto de compilación hospedada**: archivos de una suite, arquitectura y compilación, con su
  atestación.
- **Informe de cadena de suministro**: resultados de tarball contra tag, firma, procedencia,
  Scorecard y resumen del cambio, adjuntos al PR.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: ghostty 1.4.0 se publica en todas sus suites no excluidas para amd64 y arm64, se
  instala con `apt` en un contenedor limpio de cada una y `ghostty +version` informa `1.4.0`.
- **SC-002**: Desde el push hasta tener todos los artefactos e informes de prueba de una versión de
  ghostty (todas sus suites y arquitecturas, compilada dos veces) pasan menos de 30 minutos de reloj,
  sin costo económico.
- **SC-003**: El 100 % de estos casos falla antes de compilar en las pruebas: dependencia alterada,
  toolchain con otro sha256, tarball con un archivo que no está en el tag, firma con otra llave,
  procedencia de otro repositorio, artefacto sin atestación o de otro workflow.
- **SC-004**: Un paquete con arm64 excluido se publica solo en amd64 y, al quitar la exclusión, la
  ejecución siguiente publica arm64 sin cambiar ningún archivo de amd64.
- **SC-005**: Ningún job que compile código de upstream tiene acceso a secretos (comprobado por
  prueba).
- **SC-006**: Los paquetes de nivel 1 conservan sus sha256 y tepache-hello sigue pasando su E2E.
- **SC-007**: Un tercero sin credenciales verifica con `debfactory verify` cualquier versión publicada
  en menos de 15 minutos (nivel 1) o en el tiempo de una compilación (nivel 2), y un `.deb` alterado
  da "no verificado" el 100 % de las veces.
- **SC-008**: La infraestructura propia queda en un solo runner (`pub`) más el hosting; la carga de
  la PC por versión publicada no crece con el número de suites (solo verificación y prueba de humo).

## Assumptions

- **Constitución**: la 1.10.0 cubre la exclusión de arquitecturas y la compilación en runners de
  GitHub. Bajar toolchains y dependencias del lenguaje de upstream (no de los repositorios de la
  suite) requiere ampliar el principio III, que hoy solo admite las URLs de release declaradas; se
  propondrá la enmienda en el plan, con verificación obligatoria por hash o firma.
- El repositorio de paquetes (`opentepache/packages`) es público; si dejara de serlo aplica FR-012.
- El mantenedor hace público `opentepache/debfactory` (FR-022); la revisión previa del historial no
  encontró secretos ni datos de sesiones; quedan públicos su correo (ya público en los `.deb`), su
  usuario local y la descripción de la PC.
- Se mantiene el monorepo; separar paquetes en repositorios propios queda para cuando haya varios
  mantenedores con permisos por paquete (FR-026 lo deja preparado).
- Los runners estándar tienen 4 vCPU y 16 GB de RAM; ghostty usa ~12 GB al compilar (spike).
- Gestores de paquetes del lenguaje: en esta fase solo Zig; Go, Rust u otros quedan para fases
  posteriores con el mismo mecanismo.
- Zig 0.17.0 es la versión del toolchain de ghostty 1.4.0; 0.17.1 falla en el spike.
- Suites de ghostty: trixie y forky (clarify); quien use sid puede instalar el paquete de forky.
- El resumen del cambio entre versiones lo genera GitHub Models en un job sin secretos (principio X,
  clarify); el uso gratuito tiene límites, suficientes para unas pocas actualizaciones al día.
- Fuera de alcance: compilación cruzada, runners de pago o propios arm64, tests de upstream durante
  la compilación, antivirus, y que el detector de versiones (fase 005) maneje paquetes de nivel 2:
  sigue omitiéndolos, así que las versiones nuevas de ghostty se suben por PR manual (con `debfactory
  lock`); el resumen con IA y Scorecard aplican igual a esas PR.
