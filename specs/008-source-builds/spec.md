# Feature Specification: Paquetes de nivel 2 (compilación desde el código fuente)

**Feature Branch**: `008-source-builds`

**Created**: 2026-11-26

**Status**: Draft

**Input**: User description: "Quiero soportar paquetes que deben compilarse desde el código fuente
porque upstream no publica binarios utilizables o porque dependen de librerías del sistema de cada
distribución. Qué debe hacer: el manifiesto puede declarar un paquete de nivel 2 con su carpeta
debian/ estándar (control, rules, changelog generado, copyright). Se construye en un entorno limpio
por suite y arquitectura, resolviendo dependencias de build desde los repos oficiales de esa
suite. Se publican el paquete fuente (.dsc, .orig.tar.*, .debian.tar.*) y los binarios, y ambos
corresponden exactamente entre sí. Debe convivir con el nivel 1 sin cambiar nada para los paquetes
existentes. Como cada suite produce un binario distinto, la versión lleva un sufijo por suite para
que nunca existan dos archivos distintos con el mismo nombre en el pool:
<upstream>-0~tepache<N>+<suite>, por ejemplo 1.2.3-0~tepache1+deb12, +deb13, +u2404, +u2604.
Criterios de aceptación: un paquete de prueba que enlaza con librerías del sistema se construye
por separado para bookworm y trixie y cada binario funciona en su suite. "apt-get source" +
rebuild local reproduce el mismo paquete binario."

## Contexto

La constitución (principio II) prevé dos niveles: el nivel 1 reempaqueta el binario oficial de
upstream y es el único que existe hoy; el nivel 2 compila desde el código fuente en un entorno
limpio, solo cuando no hay binario utilizable o el software depende de librerías del sistema, con
justificación en el manifiesto, y nunca publica un paquete fuente que no corresponda exactamente al
binario. El principio VI ya fija el sufijo por suite (`level2_suffix` de cada suite, fase 007).
Esta fase hace realidad el nivel 2.

## Clarifications

### Session 2026-11-26

- Q: ¿Qué garantiza el repositorio para que recompilar desde el fuente dé el mismo binario? → A: se
  publica el registro del entorno de compilación (versiones exactas de las dependencias) y el
  pipeline compila dos veces e idénticas antes de publicar; recompilar más tarde requiere conseguir
  esas versiones, y el repositorio no guarda copia de las dependencias (FR-008, FR-013).
- Q: ¿El paquete de prueba se publica en el repositorio público? → A: no; vive solo en las pruebas
  (E2E y repositorio de prueba). El primer paquete real de nivel 2 llega con un PR aparte cuando haya
  un candidato que cumpla el principio V (no duplicar) (SC-001).
- Q: Al agregar una suite, ¿los paquetes de nivel 2 publicados se compilan solos para ella? → A: sí;
  `main` compila, prueba y publica la versión actual solo para la suite nueva, con su sufijo, como
  amplía el nivel 1; si falla, queda pendiente, notificado y se reintenta (FR-012, FR-015).
- Q: ¿Un paquete de nivel 2 puede producir varios binarios desde un fuente o binarios
  `Architecture: all`? → A: no en esta fase; exactamente un paquete binario por fuente, con
  `Architecture: any`; la validación rechaza lo demás (FR-003).

## User Scenarios & Testing *(mandatory)*

Actores:

- **El mantenedor**: escribe el manifiesto de nivel 2 y su carpeta `debian/`, y revisa los PR.
- **El pipeline**: valida, descarga y verifica el código fuente, compila, prueba y publica (fases
  002–007).
- **El usuario**: instala el paquete con apt en su suite.
- **Quien verifica**: cualquier persona que descarga el paquete fuente con apt y lo recompila para
  comprobar que obtiene el mismo binario.

### User Story 1 - Declarar y construir un paquete de nivel 2 (Priority: P1)

El mantenedor agrega un manifiesto de nivel 2: el código fuente de upstream con su checksum, la
justificación de por qué no basta el nivel 1, y la carpeta `debian/` estándar (control, rules,
copyright; el changelog lo genera la herramienta). En un PR, el pipeline valida el manifiesto,
descarga y verifica el código fuente, pasa el gate de licencias, y por cada suite y arquitectura
configuradas compila en un entorno limpio con las dependencias de compilación de los repositorios
oficiales de esa suite, pasa lintian y prueba la instalación del binario resultante en esa misma
suite.

**Why this priority**: sin esto no hay nivel 2; todo lo demás se apoya en poder construir.

**Independent Test**: un paquete de prueba que enlaza con una librería del sistema se construye
para bookworm y trixie (amd64 y arm64), cada binario depende de la versión de la librería de su
suite y se instala y ejecuta en un contenedor limpio de esa suite.

**Acceptance Scenarios**:

1. **Given** un manifiesto de nivel 2 válido con su carpeta `debian/`, **When** se construye,
   **Then** hay un paquete fuente y un binario por suite y arquitectura no excluidas, cada uno con
   la versión `<upstream>-0~tepache<N>+<sufijo de la suite>`.
2. **Given** el binario de bookworm y el de trixie, **When** se inspeccionan, **Then** cada uno
   depende de las versiones de las librerías de su propia suite, y cada uno se instala y funciona en
   un contenedor limpio de su suite.
3. **Given** un manifiesto de nivel 2 sin justificación, **When** se valida, **Then** falla con un
   mensaje que lo pide (constitución II).
4. **Given** una suite donde la compilación falla, **When** el pipeline construye, **Then** no se
   publica la versión en ninguna suite (principio VII) y se notifica la suite, la arquitectura y el
   motivo, salvo que el manifiesto excluya esa suite.
5. **Given** que el código fuente descargado no coincide con su checksum, **When** se construye,
   **Then** falla antes de compilar (principio III).

---

### User Story 2 - Publicar fuente y binarios que se corresponden (Priority: P1)

Cuando la versión pasa todos los gates, el pipeline publica en cada suite su paquete fuente (la
descripción `.dsc`, el código original de upstream y el empaquetado) y sus binarios, firmados como
el resto del repositorio. El usuario instala con apt; quien verifica puede descargar el fuente con
apt.

**Why this priority**: la constitución exige publicar el fuente que corresponde exactamente al
binario; sin publicar no hay valor para el usuario.

**Independent Test**: en un contenedor limpio de cada suite, `apt-get install` instala la versión
de esa suite y `apt-get source` descarga un paquete fuente cuya versión es exactamente la del
binario instalado.

**Acceptance Scenarios**:

1. **Given** una versión de nivel 2 publicada, **When** un usuario de bookworm hace
   `apt-get install`, **Then** obtiene la versión `+deb12`; uno de trixie, la `+deb13`.
2. **Given** la misma versión, **When** se hace `apt-get source` en cada suite, **Then** se
   obtiene el paquete fuente de esa suite, con la firma del repositorio, y su versión es idéntica a
   la del binario.
3. **Given** el pool del repositorio, **When** se revisa, **Then** nunca hay dos archivos distintos
   con el mismo nombre; el código original de upstream, que es el mismo para todas las suites, es un
   único archivo compartido.
4. **Given** la auditoría, **When** se revisa la publicación, **Then** registra cada binario y cada
   paquete fuente con su suite, su arquitectura y su sha256.

---

### User Story 3 - Recompilar y obtener el mismo binario (Priority: P2)

Quien verifica descarga el paquete fuente con apt, lo recompila siguiendo una guía documentada, y
obtiene un binario idéntico byte a byte al publicado.

**Why this priority**: es la garantía de que el binario sale de ese fuente y no de otro; refuerza
la confianza, pero el paquete ya es útil antes de que alguien lo verifique.

**Independent Test**: tomar un binario publicado de cada suite, descargar su fuente con apt,
recompilarlo con el procedimiento documentado y comparar los sha256.

**Acceptance Scenarios**:

1. **Given** un binario publicado de nivel 2, **When** se descarga su fuente con `apt-get source`
   y se recompila con la guía en un entorno con las mismas versiones de dependencias de compilación
   que se usaron, **Then** el binario resultante es idéntico byte a byte.
2. **Given** el pipeline, **When** construye una versión de nivel 2, **Then** la construye dos veces
   en entornos limpios distintos y compara los resultados antes de publicar; si difieren, no publica
   y lo notifica.
3. **Given** cada publicación, **When** se revisa, **Then** incluye el registro del entorno de
   compilación (las versiones exactas de las dependencias de compilación usadas), necesario para
   reproducirla.

---

### User Story 4 - El nivel 1 no cambia (Priority: P1)

Los paquetes existentes (aqua, bob, buf, fnm, k9s) siguen construyéndose, probándose y publicándose
exactamente igual: un mismo `.deb` para todas las suites, sin sufijo, sin paquete fuente.

**Why this priority**: el repositorio en producción no puede romperse por la llegada del nivel 2.

**Independent Test**: con la herramienta nueva, la reconstrucción de los paquetes de nivel 1 da los
mismos sha256 que los publicados y el repositorio de prueba no cambia.

**Acceptance Scenarios**:

1. **Given** los paquetes de nivel 1 publicados, **When** el pipeline corre con la herramienta
   nueva, **Then** sus `.deb` son idénticos a los publicados y no hay nada que publicar.
2. **Given** un repositorio con paquetes de los dos niveles, **When** un usuario instala uno de cada
   uno, **Then** los dos funcionan y ningún paquete de nivel 1 cambia de nombre ni de versión.

---

### Edge Cases

- **El código de upstream intenta descargar cosas al compilar**: la compilación no tiene acceso a
  la red más allá de los repositorios oficiales de la suite para instalar dependencias; si el build
  necesita red, falla y se notifica (el mantenedor debe declarar esas fuentes o elegir otro
  paquete).
- **Compilar código de upstream es ejecutar código no confiable**: corre en un entorno aislado, sin
  la llave de firma ni credenciales, igual que las pruebas de instalación (principios III y X).
- **Una dependencia de compilación no existe en una suite** (p. ej. una librería nueva que bookworm
  no tiene): la suite se excluye en el manifiesto con su motivo; si no se excluye, falla y bloquea
  la versión (principio VII).
- **La misma versión upstream en una suite nueva** (fase 007): ampliar significa compilar para ella
  (FR-015); las demás suites no se recompilan ni cambian. Si la compilación o la prueba fallan, la
  suite nueva queda pendiente y notificada, y se reintenta en cada ejecución.
- **Corrección del empaquetado sin cambio upstream**: sube `N` en todas las suites a la vez
  (`1.2.3-0~tepache2+deb12`, `+deb13`…).
- **Versiones oficiales de Debian del mismo software**: con revisión `0`, `~` y el sufijo, la
  versión oficial (`1.2.3-1`) siempre gana (principio VI); el orden se prueba.
- **Las dependencias de compilación cambian con el tiempo en la suite**: una recompilación meses
  después puede obtener otras versiones; la reproducibilidad se garantiza con el registro del
  entorno (US3), no con el estado actual de la suite.
- **Retiro de una versión de nivel 2**: se retira de todas las suites, binarios y fuentes.

## Requirements *(mandatory)*

### Functional Requirements

**Manifiesto**

- **FR-001**: El manifiesto MUST poder declarar nivel 2. Un manifiesto de nivel 2 MUST incluir: el
  código fuente de upstream (URL con plantillas y su verificación por checksum o firma, como el
  nivel 1), la justificación del nivel 2 (constitución II), y una carpeta `debian/` junto al
  manifiesto con, al menos, `control`, `rules` y `copyright`, en el formato estándar de Debian.
- **FR-002**: El `changelog` del paquete fuente MUST generarlo la herramienta para cada suite, con la
  versión de esa suite y una entrada reproducible (fecha fija derivada de la publicación de
  upstream). La carpeta `debian/` del manifiesto MUST NOT traer `changelog`.
- **FR-003**: La validación MUST rechazar un manifiesto de nivel 2 sin justificación, sin código
  fuente verificable o sin los archivos obligatorios de `debian/`, y un `control` cuyo nombre de
  paquete fuente no coincida con el del manifiesto. En esta fase el `control` MUST declarar
  exactamente un paquete binario, con el mismo nombre que el fuente y `Architecture: any`; varios
  `Package:` o `Architecture: all` se rechazan con un mensaje claro.

**Construcción**

- **FR-004**: Por cada suite configurada no excluida y cada arquitectura, el pipeline MUST generar el
  paquete fuente con la versión `<upstream>-0~tepache<N>+<level2_suffix de la suite>` y compilar
  sus binarios a partir de ese mismo paquete fuente, en un entorno limpio de esa suite y
  arquitectura, con las dependencias de compilación de los repositorios oficiales de la suite.
- **FR-005**: La compilación MUST correr aislada: sin la llave de firma, sin credenciales y sin
  acceso a la red salvo para instalar dependencias desde los repositorios oficiales de la suite
  antes de compilar.
- **FR-006**: Cada binario MUST pasar los mismos gates que el nivel 1 en su suite: licencias (sobre
  el código fuente verificado, antes de compilar), lintian y prueba de instalación en un contenedor
  limpio de esa suite y arquitectura.
- **FR-007**: La regla "falla en una, no se publica en ninguna" (principio VII) MUST aplicarse a las
  suites y arquitecturas no excluidas de un paquete de nivel 2.
- **FR-008**: El pipeline MUST compilar cada versión dos veces en entornos limpios independientes y
  publicar solo si los binarios son idénticos byte a byte; MUST registrar, por suite y arquitectura,
  el entorno de compilación (versiones exactas de las dependencias de compilación) y publicarlo con
  el paquete.

**Publicación**

- **FR-009**: El pipeline MUST publicar en cada suite su paquete fuente (descripción firmada por el
  repositorio a través de los índices, código original de upstream y empaquetado) y sus binarios,
  en la misma publicación atómica, de modo que `apt-get source` y `apt-get install` funcionen en
  cada suite.
- **FR-010**: El código original de upstream MUST ser un único archivo en el pool compartido por
  todas las suites de la misma versión; ningún nombre de archivo del pool MUST corresponder a dos
  contenidos distintos.
- **FR-011**: El estado y la auditoría MUST registrar, para una versión de nivel 2, cada binario por
  suite y arquitectura y cada paquete fuente por suite, con su sha256.
- **FR-012**: La retención, el retiro, la ampliación a suites nuevas (fase 007) y el sitio (fase
  006) MUST funcionar con versiones de nivel 2: el sitio muestra la versión de cada suite y enlaza
  al paquete fuente.

- **FR-015**: Cuando a la versión actual de un paquete de nivel 2 le falte una suite configurada no
  excluida, cada ejecución de `main` MUST generar su paquete fuente con el sufijo de esa suite,
  compilarlo (dos veces, FR-008), probarlo y publicarlo solo en esa suite, sin recompilar ni cambiar
  las demás; un fallo queda pendiente y notificado hasta lograrlo o hasta que el manifiesto excluya
  la suite (como FR-014 de la fase 007).

**Reproducibilidad**

- **FR-013**: MUST existir una guía documentada para recompilar un binario publicado a partir de
  `apt-get source` y del registro del entorno de compilación, y una prueba automática que la siga y
  obtenga el mismo sha256. El repositorio MUST NOT guardar copia de las dependencias de compilación:
  conseguir sus versiones exactas tiempo después (del archivo histórico de la distribución) queda
  del lado de quien verifica.

**Convivencia**

- **FR-014**: Los manifiestos de nivel 1 MUST seguir siendo válidos sin cambios, y sus paquetes MUST
  construirse y publicarse exactamente igual que antes (mismos sha256, sin sufijo, sin paquete
  fuente).

### Key Entities

- **Manifiesto de nivel 2**: nivel, justificación, código fuente de upstream y su verificación,
  licencia, carpeta `debian/` (sin changelog), suites excluidas con motivo.
- **Paquete fuente por suite**: descripción `.dsc`, código original de upstream (compartido),
  empaquetado de esa suite; versión con sufijo.
- **Binario por suite y arquitectura**: versión con sufijo; construido desde el paquete fuente de su
  suite.
- **Registro del entorno de compilación**: por suite y arquitectura, las versiones exactas de las
  dependencias de compilación usadas.
- **Versión publicada de nivel 2**: el conjunto de paquetes fuente y binarios de una versión upstream
  y revisión `N`, por suite.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: En las pruebas de punta a punta, un paquete de prueba que enlaza con una librería del sistema se construye para bookworm
  y trixie en amd64 y arm64 (4 de 4), y cada binario se instala y ejecuta en un contenedor limpio de
  su suite y depende de la librería de esa suite.
- **SC-002**: En cada suite donde está publicado, `apt-get source` descarga un paquete fuente cuya
  versión coincide con la del binario (100 % de las suites y arquitecturas publicadas).
- **SC-003**: Recompilar desde `apt-get source` siguiendo la guía, con el entorno registrado, da el
  mismo sha256 que el binario publicado en todas las combinaciones de la prueba (bookworm, trixie y
  noble × amd64 y arm64).
- **SC-004**: Tras la fase, los 5 paquetes de nivel 1 publicados se reconstruyen con los mismos
  sha256 y no cambia ningún archivo publicado de nivel 1.
- **SC-005**: Ningún nombre de archivo del pool corresponde a dos contenidos distintos (comprobado
  en la verificación del repositorio).

## Assumptions

- El paquete de prueba es un software pequeño de código abierto con licencia aceptada que enlaza con
  una librería del sistema cuya versión cambia entre bookworm y trixie (se elige en el plan). Vive
  solo en las pruebas y en el repositorio de prueba; nunca se publica en producción.
- Las dependencias de compilación vienen solo de los repositorios oficiales de cada suite; no se
  usan repositorios de terceros ni el propio repositorio de OpenTepache.
- "Reproducir el mismo binario" significa con las mismas versiones de dependencias de compilación
  que registra el entorno (como en las compilaciones reproducibles de Debian); obtener esas versiones
  años después puede requerir archivos históricos de la distribución, fuera del alcance de esta
  fase.
- Los sufijos salen del `level2_suffix` de cada suite en la configuración (fase 007).
- El detector de versiones (fase 005) no propone versiones nuevas de paquetes de nivel 2 en esta
  fase; el mantenedor las sube a mano con un PR.
- **Fuera de alcance**: compilación cruzada (cada arquitectura compila en su propio entorno,
  emulado si hace falta), paquetes fuente con varios binarios o con binarios `Architecture: all`
  (FR-003), parches de Debian (quilt) más allá de lo que soporte el formato estándar,
  y el detector para nivel 2.
- **Dependencias**: constitución v1.8.0 (II, III, VI, VII); fases 001 (gates), 002 (repositorio y
  pool), 003 (pipeline y auditoría), 006 (sitio) y 007 (suites configurables).
