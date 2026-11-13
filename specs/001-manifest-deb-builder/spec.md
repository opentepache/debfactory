# Feature Specification: Constructor de .deb desde manifiestos declarativos

**Feature Branch**: `001-manifest-deb-builder`

**Created**: 2026-11-11

**Status**: Draft

**Input**: User description: "Quiero poder generar paquetes .deb instalables a partir de un
manifiesto declarativo por herramienta, sin escribir código por paquete. (…) Fuera de alcance en
esta fase: publicar el repositorio, CI, detección automática de versiones, sitio web, compilación
desde fuente (nivel 2)."

## User Scenarios & Testing *(mandatory)*

El único actor es **el mantenedor**: una sola persona que agrega, actualiza y construye
paquetes en su PC Debian. Todo el valor de esta fase se mide en cuánto trabajo le ahorra y en
que nunca produzca un paquete inseguro, sin licencia válida o roto.

### User Story 1 - Construir un paquete a partir de su manifiesto (Priority: P1)

El mantenedor tiene un manifiesto para una herramienta (por ejemplo `fnm`) y ejecuta un único
comando indicando el paquete y, opcionalmente, la arquitectura (la versión es la declarada en
el manifiesto). Obtiene un `.deb` instalable con versión
`<upstream>-0~tepache<N>`, metadatos completos y licencia en `copyright`, sin haber escrito código
para ese paquete.

**Why this priority**: es el núcleo de la fábrica; sin esto no hay nada que probar ni publicar.

**Independent Test**: con el manifiesto de un binario simple, ejecutar el comando de construcción
para `amd64` y comprobar que se genera un `.deb` cuyos metadatos (nombre, versión, mantenedor,
descripción, homepage, arquitectura) son los esperados y que pasa `lintian` sin errores.

**Acceptance Scenarios**:

1. **Given** un manifiesto válido de `fnm` en versión `1.39.0` con `N=1`, **When** el mantenedor
   construye para `amd64`, **Then** obtiene `fnm_1.39.0-0~tepache1_amd64.deb` con el binario
   instalado en la ruta declarada y la licencia upstream en `copyright`.
2. **Given** el mismo manifiesto, **When** el mantenedor construye "todas" las arquitecturas,
   **Then** obtiene un `.deb` por cada arquitectura soportada (`amd64`, `arm64`).
3. **Given** el mismo manifiesto y la misma versión, **When** se construye dos veces, **Then**
   ambos `.deb` son idénticos byte a byte.
4. **Given** cualquier `.deb` construido, **When** se analiza con `lintian`, **Then** no reporta
   errores.

---

### User Story 2 - Rechazar entradas inválidas o inseguras antes de construir (Priority: P1)

Si el manifiesto está mal escrito, si el artefacto descargado no coincide con su checksum o
firma, o si la licencia upstream no está permitida, el build se detiene antes de producir
ningún `.deb` y el mensaje dice exactamente qué falló y dónde.

**Why this priority**: es exigido por la constitución (III y IV, no relajables); un paquete
inseguro o con licencia no permitida es peor que no tener paquete.

**Independent Test**: preparar tres casos rotos (manifiesto con un campo obligatorio ausente,
checksum alterado, licencia fuera de la allowlist) y comprobar que cada uno falla sin generar
`.deb` y con un mensaje que nombra el paquete, la etapa y la causa.

**Acceptance Scenarios**:

1. **Given** un manifiesto sin el campo de licencia esperada, **When** se intenta construir,
   **Then** el build falla en la validación indicando el archivo, el campo y la regla violada.
2. **Given** un lockfile cuyo hash registrado no coincide con el artefacto descargado,
   **When** se construye, **Then** falla indicando el artefacto, el hash esperado y el obtenido.
3. **Given** un upstream cuya licencia no puede identificarse, o cuya licencia detectada no
   coincide con la declarada en el manifiesto, o cuya licencia declarada no está en la
   allowlist, **When** se construye, **Then** falla indicando cuál de los tres casos ocurrió y
   las licencias implicadas (detectada y declarada).
4. **Given** un manifiesto que declara una URL de descarga fuera de las URLs de release
   oficiales del upstream declarado, **When** se construye, **Then** falla sin descargar nada.

---

### User Story 3 - Probar la instalación real en cada suite (Priority: P2)

Tras construir, el mantenedor ejecuta la prueba automática: el `.deb` se instala en un
contenedor limpio de cada suite soportada (bookworm, trixie, sid, noble, resolute) salvo las
excluidas en el manifiesto, se ejecuta la orden de comprobación declarada (por ejemplo
`--version`), se verifica que reporta la versión esperada y se desinstala limpiamente.

**Why this priority**: es el gate que impide publicar paquetes rotos (constitución VII), pero
depende de que P1 ya produzca `.deb`.

**Independent Test**: con un `.deb` ya construido, ejecutar la prueba y obtener un resultado por
suite y arquitectura (aprobado / fallido / excluido con motivo).

**Acceptance Scenarios**:

1. **Given** un `.deb` de `buf` para `amd64`, **When** se ejecuta la prueba, **Then** en cada
   suite no excluida se instala, la orden de comprobación reporta la versión upstream esperada,
   se desinstala con purga y no quedan archivos del paquete en el sistema.
2. **Given** un manifiesto que excluye `bookworm` con motivo documentado, **When** se ejecuta la
   prueba, **Then** el informe muestra `bookworm` como `excluded: <motivo>` y no la cuenta como
   fallo.
3. **Given** que la prueba falla en una sola suite no excluida, **When** termina, **Then** el
   resultado global del paquete es fallido y el informe indica la suite, la etapa
   (instalación / ejecución / versión / desinstalación) y la salida relevante.

---

### User Story 4 - Agregar o actualizar una herramienta editando solo datos (Priority: P2)

Para actualizar una herramienta, el mantenedor cambia la versión (y vuelve `N` a 1) en su
manifiesto y regenera el lockfile con una orden. Para agregar una herramienta nueva de un solo binario estático,
escribe un manifiesto nuevo de menos de ~25 líneas. En ningún caso escribe ni copia código.

**Why this priority**: es la razón de ser declarativa del proyecto (constitución I), pero se
apoya en P1–P3.

**Independent Test**: agregar un quinto paquete simple (un binario estático con licencia
permitida) solo con un manifiesto nuevo y comprobar que construye y pasa las pruebas.

**Acceptance Scenarios**:

1. **Given** el sistema con los 4 manifiestos iniciales, **When** se agrega un manifiesto de un
   binario estático de menos de ~25 líneas, **Then** el nuevo paquete se construye y pasa las
   pruebas sin cambios en ningún otro archivo salvo su lockfile generado.
2. **Given** un manifiesto en versión `X` con `N=2`, **When** se sube a versión upstream `Y`
   sin volver `N` a 1, **Then** la validación falla pidiendo `N=1`; con `N=1`, el paquete
   resultante tiene versión `Y-0~tepache1`.

---

### User Story 5 - Expresar paquetes no triviales con el mismo esquema (Priority: P3)

Los manifiestos pueden expresar varios ejecutables en un paquete (buf instala `buf`, `protoc-gen-buf-breaking` y
`protoc-gen-buf-lint`), binarios que se
renombran al instalar (ruta de destino con otro nombre), archivos comprimidos con rutas internas distintas por
arquitectura, completions de shell y man pages opcionales, y dependencias o conflictos con
otros paquetes.

**Why this priority**: necesario para los 4 paquetes iniciales, pero un binario simple ya aporta
valor sin estas capacidades.

**Independent Test**: construir los 4 paquetes iniciales para `amd64` y `arm64` y verificar que
cada uno instala exactamente los archivos declarados en sus rutas destino.

**Acceptance Scenarios**:

1. **Given** el manifiesto de `buf`, **When** se construye, **Then** el `.deb` contiene los tres
   ejecutables en sus rutas destino, las completions de bash, zsh y fish incluidas en el
   artefacto, y todas sus man pages (más de 100) declaradas con un único patrón, no una por una.
2. **Given** un manifiesto cuyo artefacto contiene un ejecutable `tool-linux-amd64` y declara
   `dst: /usr/bin/tool`, **When** se construye, **Then** el ejecutable queda instalado como
   `/usr/bin/tool` (renombrado).
3. **Given** un manifiesto que declara completions para bash, zsh y fish, **When** se construye,
   **Then** cada archivo queda en la ubicación estándar de Debian para ese shell.
4. **Given** un manifiesto que declara completions generadas por el propio binario, **When** se
   construye para una arquitectura distinta a la del host, **Then** el build no requiere
   ejecutar el binario de esa arquitectura en el host o, si lo requiere, lo declara y falla con
   un mensaje claro cuando no es posible.

---

### Edge Cases

- **Glibc insuficiente**: el binario upstream está compilado contra una glibc más nueva que la
  de alguna suite (por ejemplo, `bob` se compila en Ubuntu 24.04, glibc 2.39, y `bookworm` trae
  2.36). La prueba falla en esa suite; el mantenedor la excluye en el manifiesto con motivo, y
  la exclusión no bloquea las demás suites.
- **Upstream sin checksums publicados** (hoy `fnm` y `bob`): el hash queda registrado en
  el lockfile del paquete; si el artefacto cambia en upstream para la misma versión, el build falla.
- **Upstream con checksums publicados** (hoy `aqua`, con firma sigstore, y `buf`, con
  `sha256.txt` firmado con minisign): se verifica contra el checksum publicado, y el hash del
  archivo de checksums queda en el lockfile. Las firmas se verificarán en una fase futura.
- **Licencia no detectable automáticamente** (por ejemplo, el API de GitHub no identifica la
  licencia de `aqua` aunque el repositorio contiene `LICENSE`): el gate falla cerrado salvo que
  el texto de licencia de la versión exacta pueda obtenerse de una fuente declarada en el
  manifiesto e identificarse.
- **El artefacto solo trae el binario, sin texto de licencia**: el texto se obtiene de la fuente
  de licencia declarada en el manifiesto para esa versión exacta.
- **La versión upstream reporta un formato distinto** (por ejemplo `v1.39.0`, `fnm 1.39.0`): la
  verificación de versión acepta el patrón declarado en el manifiesto.
- **La orden de comprobación necesita red o HOME**: la prueba corre sin red y con un
  HOME temporal; si la herramienta no puede reportar su versión así, el manifiesto declara otra
  orden de comprobación.
- **Mismo nombre que un paquete oficial más viejo** (si Debian o Ubuntu llegan a empaquetar una
  de estas herramientas): el paquete de debfactory lo reemplaza mientras su versión upstream sea
  mayor; cuando la versión oficial alcance o supere al upstream, la oficial gana (constitución
  VI), y la herramienta se reevalúa frente a la constitución V. Si el
  paquete oficial divide archivos en otros paquetes, las relaciones declaradas en el manifiesto
  (`Conflicts`/`Replaces`/`Provides`) evitan choques de archivos.
- **Arquitectura sin binario upstream**: el paquete no cumple el alcance (toda arquitectura
  soportada es obligatoria) y no se incorpora hasta que upstream publique ese binario.
- **El binario upstream incrusta una biblioteca** (lintian `embedded-library`, p. ej. pnpm con
  libyaml): el error no se puede silenciar (constitución VII) y el paquete no se incorpora en
  nivel 1.
- **Descarga interrumpida o artefacto corrupto**: se trata igual que un checksum alterado; nada
  parcialmente descargado se usa ni se reutiliza.

## Requirements *(mandatory)*

### Functional Requirements

**Manifiesto y esquema**

- **FR-001**: Cada herramienta MUST describirse en exactamente un manifiesto de datos, sin
  código, que declare como mínimo: nombre del paquete, descripción corta y larga, homepage,
  repositorio upstream, licencia esperada (identificador SPDX), versión upstream, número de
  reempaquetado `N`, justificación de valor (constitución V: no está al día en Debian estable,
  su autor no publica repositorio APT y no está en repositorios comunitarios conocidos, como
  griffo), el nivel de construcción (`1` en esta fase) y, para `amd64` y `arm64`, la URL del
  artefacto oficial y su formato. Los hashes de integridad viven en el lockfile del paquete.
- **FR-002**: El esquema MUST soportar los formatos de artefacto: binario suelto, `.zip` y
  `.tar.gz`.
- **FR-003**: El esquema MUST permitir declarar una lista de archivos a instalar, cada uno con
  ruta de origen dentro del artefacto (puede variar por arquitectura), ruta de destino, nombre de
  destino (permite renombrar) y permisos. Una ruta de origen MAY ser un patrón que seleccione
  varios archivos (por ejemplo, todas las man pages de un directorio); un patrón que no
  seleccione ningún archivo MUST hacer fallar el build.
- **FR-004**: El esquema MUST permitir declarar, de forma opcional, completions de shell (bash,
  zsh, fish), incluidas en el artefacto o generadas por el propio binario, y man pages
  incluidas en el artefacto.
- **FR-005**: El esquema MUST permitir declarar relaciones con otros paquetes: dependencias,
  recomendaciones, conflictos, reemplazos y paquetes provistos.
- **FR-006**: El esquema MUST permitir excluir suites concretas, cada una con un motivo no
  vacío. Las arquitecturas no se pueden excluir (constitución, Alcance).
- **FR-007**: El esquema MUST permitir declarar la orden de comprobación de la prueba de
  instalación y una subcadena que la salida debe contener, en función de la versión upstream.
- **FR-008**: El esquema MUST permitir declarar la fuente del texto de licencia de la versión
  exacta: un archivo dentro del artefacto o una URL del repositorio upstream fijada a esa
  versión.
- **FR-009**: Los datos comunes a todos los paquetes (mantenedor, lista de suites y
  arquitecturas soportadas, allowlist de licencias, sufijos de suite, lista de repositorios
  APT comunitarios conocidos) MUST vivir en
  configuración compartida, no repetirse en cada manifiesto.
- **FR-010**: El sistema MUST validar cada manifiesto contra el esquema antes de cualquier
  descarga, y cada error MUST indicar archivo, campo y regla violada. Un manifiesto con campos
  desconocidos MUST rechazarse (protege contra errores de tipeo).

**Construcción**

- **FR-011**: Un comando MUST construir un paquete, en la versión declarada en su manifiesto,
  para una arquitectura dada o para todas las soportadas; y MUST poder construir todos los paquetes.
- **FR-012**: El sistema MUST descargar artefactos solo desde las URLs declaradas en el
  manifiesto y MUST rechazar URLs que no pertenezcan a los releases oficiales del repositorio
  upstream declarado.
- **FR-013**: El sistema MUST verificar cada artefacto contra el hash registrado en el lockfile
  del paquete (que el build nunca escribe) y, cuando upstream publique checksums, también contra
  ellos; el hash del archivo de checksums también vive en el lockfile. Cualquier discrepancia
  MUST hacer fallar el build indicando hash esperado y obtenido.
- **FR-014**: Antes de construir, el sistema MUST obtener el texto de licencia de la versión
  exacta desde la fuente declarada, identificarlo y fallar si: no es identificable, no está en
  la allowlist, o no coincide con la licencia esperada del manifiesto.
- **FR-015**: El `.deb` MUST tener versión `<upstream>-0~tepache<N>` y nombre de archivo estándar de
  Debian (`<paquete>_<versión>_<arquitectura>.deb`); un mismo `.deb` de nivel 1 sirve para todas
  las suites.
- **FR-016**: El `.deb` MUST incluir metadatos completos: mantenedor, descripción, homepage,
  arquitectura, sección, prioridad, relaciones declaradas, y un `copyright` en formato legible
  por máquina con la licencia upstream y su texto.
- **FR-017**: Construir dos veces el mismo manifiesto con la misma versión MUST producir `.deb`
  idénticos byte a byte (fechas, propietarios, permisos y orden de archivos fijados).
- **FR-018**: Cada `.deb` construido MUST pasar `lintian` sin errores; un error de `lintian`
  MUST hacer fallar el build, salvo que esté cubierto por un override declarado en el manifiesto
  y permitido por la constitución VII.
- **FR-019**: Un fallo MUST detener el proceso para ese paquete y arquitectura sin dejar `.deb`
  parciales en la salida, y el mensaje MUST nombrar paquete, versión, arquitectura, etapa y
  causa exacta.
- **FR-020**: Ningún paso de construcción MUST requerir código específico de un paquete; los
  4 paquetes iniciales se expresan solo con el esquema genérico.

**Prueba de instalación**

- **FR-021**: Un comando MUST probar un `.deb` construido instalándolo en un contenedor limpio
  de cada suite soportada no excluida, para la arquitectura del `.deb`.
- **FR-022**: La prueba MUST: instalar el paquete con sus dependencias, ejecutar la orden de
  comprobación declarada sin acceso a red, verificar que la salida contiene la subcadena
  esperada, desinstalar con purga y verificar que no quedan archivos del paquete.
- **FR-023**: La prueba MUST producir un informe por suite y arquitectura con estado aprobado,
  fallido (con etapa y salida) o excluido (con motivo), y un resultado global que es fallido si
  falla cualquier suite no excluida.
- **FR-024**: La prueba MUST validar con `dpkg --compare-versions` que la versión generada es
  menor que la oficial hipotética `<upstream>-1` y mayor que `<upstream>-0~tepache<N-1>` cuando
  `N > 1` (constitución VI).

### Key Entities

- **Manifiesto**: descripción declarativa de una herramienta. Contiene identidad (nombre,
  descripciones, homepage, upstream), licencia esperada y su fuente, versión upstream y `N`,
  justificación de valor, nivel, artefactos por arquitectura, archivos a instalar, extras opcionales
  (completions, man pages), relaciones con otros paquetes, exclusiones y orden de comprobación.
- **Lockfile**: hashes verificados por versión y arquitectura (artefactos), del texto de
  licencia y del archivo de checksums upstream si existe, y la fecha del release upstream; lo genera una orden explícita, nunca el build, y se revisa en
  el control de versiones.
- **Artefacto**: archivo oficial publicado por upstream para una versión y arquitectura; tiene
  URL, formato e integridad (checksum upstream, firma upstream o hash registrado).
- **Configuración compartida**: mantenedor, suites y arquitecturas soportadas, allowlist de
  licencias, sufijos de suite y repositorios APT comunitarios conocidos.
- **Paquete construido (`.deb`)**: resultado para un manifiesto, versión y arquitectura; tiene
  versión `<upstream>-0~tepache<N>` y un resultado de `lintian`.
- **Informe de prueba**: resultado por suite y arquitectura (aprobado / fallido / excluido) y
  resultado global.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Los paquetes iniciales se construyen para `amd64` y `arm64` con cero archivos de código específicos de un paquete.
- **SC-002**: El 100 % de los paquetes construidos se instala, reporta la versión esperada y se
  desinstala limpiamente en cada una de las 5 suites soportadas, salvo exclusiones documentadas
  en su manifiesto.
- **SC-003**: Agregar un paquete de un solo binario estático requiere crear un único archivo de
  menos de ~25 líneas, más su lockfile generado, y ningún otro cambio.
- **SC-004**: Actualizar un paquete a una nueva versión upstream requiere cambiar como máximo la
  versión y `N` en su manifiesto, más regenerar su lockfile con una orden.
- **SC-005**: El 100 % de los casos de prueba negativos (manifiesto inválido, hash del lockfile alterado,
  checksum upstream que no coincide, licencia no permitida, licencia no identificable, licencia distinta de la declarada, URL no
  oficial) falla antes de producir un
  `.deb`, con un mensaje que nombra paquete, etapa y causa.
- **SC-006**: Dos construcciones consecutivas de la misma versión producen `.deb` con hash
  idéntico en el 100 % de los paquetes.
- **SC-007**: El 100 % de los `.deb` construidos pasa `lintian` sin errores.

## Assumptions

- **Suites y arquitecturas**: las de la constitución (bookworm, trixie, sid, noble, resolute;
  amd64, arm64). La prueba de `arm64` puede ejecutarse por emulación en la PC `amd64`.
- **Datos de upstream a la fecha (2026-11-11)**, que motivan el esquema:
  - `fnm` v1.39.0 (GPL-3.0): `.zip` por arquitectura, sin checksums publicados.
  - `bob` v4.3.0 (MIT): `.zip`; el asset `arm` es aarch64 compilado en Ubuntu 24.04 (riesgo de
    glibc en bookworm); sin checksums.
  - `buf` v1.75.0 (Apache-2.0): `.tar.gz` por arquitectura con `sha256.txt` firmado con
    minisign; contiene 3 ejecutables en `buf/bin/`, completions de bash/zsh/fish, más de 100 man
    pages y `LICENSE`. No está en Debian ni en Ubuntu ni en griffo; upstream solo ofrece
    Homebrew, sin repositorio APT.
  - `aqua` v2.66.1 (MIT según su `LICENSE`): `.tar.gz` con checksums y firma sigstore.
- **Paquetes iniciales**: `fnm`, `bob`, `buf`, `aqua`. Ninguno está en griffo
  (verificado en bookworm, trixie, sid, noble y resolute). Descartados:
  - `tenv`: su autor publica un repositorio APT oficial (Cloudsmith); viola la constitución V.
  - `uv`: ya está empaquetado en griffo (y 0.9.17 en Debian testing/sid).
  - `pnpm`: su binario oficial (v12.18.0) da el error de lintian `embedded-library` (libyaml),
    que la constitución VII no permite silenciar.
- **Repositorios comunitarios conocidos**: por ahora, griffo (`debian.griffo.io`), en la
  configuración compartida (constitución V, v1.3.0). La lista se
  amplía cuando se conozcan otros; la comprobación es manual en esta fase.
- **Licencias compuestas**: una expresión SPDX como `MIT OR Apache-2.0` se acepta si al menos
  una alternativa está en la allowlist; con `AND`, todas deben estarlo.
- **N es manual**: el mantenedor fija `N` en el manifiesto; el sistema no lo calcula.
- **Mantenedor**: un único mantenedor global en la configuración compartida.
- **Completions generadas**: si una completion se genera ejecutando el binario, se acepta
  ejecutar el binario de la arquitectura nativa o por emulación durante el build; si no es
  posible, el build falla con mensaje claro.
- **Man pages generadas**: fuera de esta fase; ningún paquete inicial las necesita.
- **Overrides de lintian**: se permiten solo declarados en el manifiesto, por etiqueta y con
  justificación; nunca para etiquetas de seguridad, licencia o copyright (constitución VII).
- **Firma del repositorio**: no aplica en esta fase (no se publica); los `.deb` no se firman
  individualmente.
- **Fuera de alcance**: publicar el repositorio, CI, detección automática de versiones, sitio
  web, compilación desde fuente (nivel 2), agentes de IA.
