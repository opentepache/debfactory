# Feature Specification: Separar la herramienta y los paquetes en dos repositorios

**Feature Branch**: `004-split-repos`

**Created**: 2026-11-16

**Status**: Draft

**Input**: User description: "Separar debfactory en dos repositorios de GitHub dentro de la
organización opentepache: uno privado con la herramienta (debfactory) y uno público con la
configuración de los paquetes y el estado del repositorio APT (packages). (…) El repositorio
packages declara qué versión de debfactory necesita (fijada por commit). (…) Ningún job descarga
debfactory del repositorio privado ni tiene credenciales para leerlo. (…) Fuera de alcance: hacer
público debfactory; varios repositorios de paquetes publicando en el mismo repositorio APT;
empaquetar debfactory como .deb en su propio repositorio APT."

## Clarifications

### Session 2026-11-16

- Q: ¿Qué valida un cambio de la herramienta antes de que llegue a la imagen del runner? → A: la
  compilación de la imagen ejecuta todas las pruebas de la herramienta en la PC y se niega si
  alguna falla. No hay CI propio en el repositorio privado ni runners de la PC registrados en
  él. La segunda barrera es el check del PR que fija la versión en el repositorio de paquetes.
- Q: Al migrar, ¿qué identidad de autor queda en la historia pública del repositorio de
  paquetes? → A: la de siempre, con el correo personal del mantenedor: le sirve como referencia
  pública de su trabajo, y el correo ya es público en el campo `Maintainer` de cada paquete.

## User Scenarios & Testing *(mandatory)*

Hoy todo vive en un solo repositorio privado: la herramienta, las recetas de los paquetes, el
estado del repositorio APT y el pipeline de la fase 003. Proteger su rama principal exigiría un
plan de pago, y las recetas, que el mantenedor quiere que sean públicas, no se pueden abrir sin
abrir también la herramienta, que todavía no quiere publicar.

Esta fase reparte el contenido en dos repositorios:

- **La herramienta** (privado): el código, la imagen del runner, la instalación del host, las
  pruebas, las specs y la documentación de operación.
- **Los paquetes** (público): manifiestos y lockfiles, la configuración del repositorio APT, el
  estado publicado, el registro de auditoría, la guía del usuario final y la definición del
  pipeline. Es el repositorio cuyo merge publica, y su rama principal queda protegida sin coste.

Los actores son los de la fase 003, con un matiz:

- **El mantenedor**: escribe en los dos repositorios. Decide qué versión de la herramienta usa el
  pipeline, y es el único que la compila.
- **Un contribuyente externo**: ve y propone cambios solo en el repositorio de paquetes. Nunca
  ve el código de la herramienta.
- **El pipeline**: el de la fase 003, ahora disparado por el repositorio de paquetes y ejecutando
  la versión de la herramienta que ese repositorio declara.
- **El usuario final**: no nota nada. El repositorio APT, su URL, su llave y sus versiones no
  cambian.

### User Story 1 - El repositorio de paquetes publica con la herramienta que declara (Priority: P1)

El mantenedor integra en el repositorio de paquetes un cambio de manifiesto. El pipeline
construye, prueba y publica igual que en la fase 003, con la versión de la herramienta que el
repositorio de paquetes tiene fijada, y deja constancia de esa versión en la auditoría.

**Why this priority**: es el camino feliz de la fase 003 en la estructura nueva. Sin él no hay
publicación.

**Independent Test**: con los dos repositorios creados, subir la `revision` de un paquete en un
PR del repositorio de paquetes, integrarlo y comprobar que la versión nueva se instala con apt,
que la auditoría registra el commit de paquetes y la versión de la herramienta, y que `repo
verify --dest` pasa.

**Acceptance Scenarios**:

1. **Given** el repositorio de paquetes con la herramienta fijada en la versión H y el runner
   con H disponible, **When** se integra un cambio de manifiesto, **Then** el pipeline publica
   con H, y la entrada de auditoría registra el commit de paquetes y H.
2. **Given** la estructura nueva recién creada, **When** corre la primera ejecución de la rama
   principal del repositorio de paquetes, **Then** no publica nada nuevo: el estado coincide con
   lo publicado en la fase 003 y la generación servida no cambia.
3. **Given** un `.deb` cualquiera servido por el repositorio público, **When** se busca su
   sha256 en la auditoría, **Then** la entrada dice qué commit de paquetes y qué versión de la
   herramienta lo produjeron.

---

### User Story 2 - Actualizar la herramienta es un cambio revisado en el repositorio de paquetes (Priority: P1)

El mantenedor quiere usar una versión nueva de la herramienta. La compila en su PC, la deja
disponible en el runner y abre un PR en el repositorio de paquetes que cambia la versión fijada.
El check reconstruye todos los paquetes publicados con la versión nueva y los compara con lo
publicado antes de que nada se publique.

**Why this priority**: es el punto de control que sustituye a la protección de la rama principal
de la herramienta. Un cambio en la herramienta solo llega a ejecutarse con la llave de firma a
través de este PR.

**Independent Test**: compilar una versión nueva de la herramienta que no cambia ningún `.deb`,
abrir el PR que la fija y comprobar que el check pasa, que muestra el enlace a las diferencias
entre las dos versiones y que tras el merge no se publica nada nuevo. Repetir con una versión que
sí cambia un `.deb` y comprobar que el check falla nombrando el paquete.

**Acceptance Scenarios**:

1. **Given** una versión nueva H2 disponible en el runner, **When** se abre un PR que fija H2,
   **Then** el check reconstruye los paquetes publicados con H2 y pasa si todos los `.deb` son
   idénticos a los publicados.
2. **Given** H2 cambia el `.deb` de una versión ya publicada, **When** se abre el PR que fija H2
   sin subir la `revision` de ese paquete, **Then** el check falla nombrando el paquete (como
   FR-010 de la fase 003).
3. **Given** un PR que fija una versión H3 que el runner no tiene, **When** corre el check,
   **Then** falla antes de construir con un mensaje que nombra H3, las versiones disponibles y la
   orden para añadirla.
4. **Given** el PR que fija H2, **When** el mantenedor lo revisa, **Then** su descripción enlaza
   las diferencias entre H y H2 en el repositorio de la herramienta.
5. **Given** el runner tiene H y H2 a la vez, **When** corre una ejecución de la rama principal
   antes del merge del PR, **Then** usa H y no falla; tras el merge, la siguiente usa H2.

---

### User Story 3 - El código de la herramienta nunca sale de la PC ni del repositorio privado (Priority: P1)

Un contribuyente externo hace fork del repositorio de paquetes y abre un PR. Su código corre,
tras la aprobación del mantenedor, en el runner no confiable, con la herramienta ya instalada en
la imagen. Ningún job tiene credenciales para leer el repositorio de la herramienta ni descarga
su código.

**Why this priority**: la herramienta es privada. Si un job pudiera leerla, un PR malicioso
podría exfiltrarla, y con ella cualquier credencial que le diera acceso.

**Independent Test**: desde una cuenta secundaria, abrir un PR que intente clonar el repositorio
de la herramienta, listar credenciales y buscar el código fuente en el sistema de archivos del
job. Comprobar que nada de eso funciona y que los logs no contienen credenciales.

**Acceptance Scenarios**:

1. **Given** un PR de un fork en el repositorio de paquetes, **When** se abre o recibe un commit,
   **Then** no se ejecuta nada en la PC hasta que el mantenedor lo aprueba (FR-013 de la fase
   003).
2. **Given** ese PR aprobado, **When** su código intenta clonar o descargar el repositorio de la
   herramienta, **Then** falla por falta de acceso: el job no tiene ninguna credencial con
   permiso sobre él.
3. **Given** ese PR aprobado, **When** busca el código fuente de la herramienta en el sistema de
   archivos del job, **Then** no lo encuentra; solo está el programa ya compilado.
4. **Given** un job que pide el runner confiable desde cualquier workflow que no sea el de
   publicación de la rama principal del repositorio de paquetes, **When** arranca, **Then** el
   runner lo rechaza sin ejecutar ningún paso (como en la fase 003, ahora atado también al
   repositorio).

---

### User Story 4 - Migrar sin que el usuario final note nada (Priority: P2)

El mantenedor pasa de un repositorio a dos. El estado publicado, las generaciones en el hosting,
la llave y la URL no cambian. Cada repositorio conserva la historia de git de lo que contiene.

**Why this priority**: se hace una sola vez, pero un error deja el repositorio APT incoherente o
pierde la trazabilidad de la fase 002.

**Independent Test**: tras la migración, `repo verify --dest` pasa sin una generación nueva,
`apt update` en un contenedor limpio no da avisos, y `git log` de un manifiesto en el repositorio
de paquetes muestra sus commits desde la fase 001.

**Acceptance Scenarios**:

1. **Given** el repositorio actual, **When** se migra, **Then** el repositorio de paquetes
   contiene los manifiestos, lockfiles, configuración, estado, auditoría y guía de usuario con su
   historia de git, y el de la herramienta contiene el resto con la suya.
2. **Given** la migración terminada, **When** un usuario final hace `apt update && apt upgrade`,
   **Then** no ve cambios ni avisos.
3. **Given** la migración terminada, **When** el mantenedor busca en el repositorio de la
   herramienta, **Then** ya no hay manifiestos, lockfiles ni estado del repositorio APT: una
   sola copia de cada cosa.

---

### User Story 5 - La herramienta se prueba sin el repositorio de paquetes (Priority: P2)

El mantenedor desarrolla la herramienta en su repositorio privado. Sus pruebas usan datos de
prueba propios y no necesitan el repositorio de paquetes.

**Why this priority**: sin esto, cada cambio en la herramienta exigiría tener los dos
repositorios sincronizados para poder probarla.

**Independent Test**: en un clon limpio del repositorio de la herramienta, sin el de paquetes,
ejecutar todas las pruebas y comprobar que pasan.

**Acceptance Scenarios**:

1. **Given** un clon limpio del repositorio de la herramienta, **When** se ejecutan todas sus
   pruebas, **Then** pasan sin acceder al repositorio de paquetes.
2. **Given** un cambio en la herramienta, **When** el mantenedor quiere llevarlo al runner,
   **Then** la compilación de la imagen del runner ejecuta antes todas las pruebas de la
   herramienta en la PC y se niega a compilar si alguna falla.
3. **Given** una imagen compilada con una versión nueva, **When** se abre el PR que la fija en
   el repositorio de paquetes, **Then** ese check es la segunda barrera: reconstruye y compara
   todo lo publicado (US2).

---

### Edge Cases

- **Versión de la herramienta fijada que no existe en su repositorio**: el check falla nombrando
  la versión; no se intenta construir.
- **Herramienta compilada desde un árbol con cambios sin commit**: la compilación de la imagen se
  niega, porque la versión no identificaría el código.
- **El runner tiene una versión que ningún commit de la rama principal de paquetes fija**: no
  se usa; se puede retirar de la imagen cuando no la fije ni la rama principal ni un PR abierto.
- **Reconstruir una versión publicada con una herramienta distinta de la que la construyó**: se
  usa la versión que fija hoy la rama principal. Si el `.deb` resultante difiere, falla sin
  publicar (FR-026 de la fase 003), y la auditoría dice con qué versión se construyó el original.
- **Un PR de paquetes cambia a la vez la versión de la herramienta y un manifiesto**: se permite.
  El check compara todo lo publicado con la versión nueva y además construye el manifiesto
  cambiado.
- **PR de estado abierto durante la migración**: la migración espera a que se integre o lo
  recrea en el repositorio de paquetes desde la copia del pipeline (`restore-state-pr`).
- **Issues de fallo y PR de estado en un repositorio público**: su contenido (paquete, versión,
  etapa, motivo, enlace al log) pasa a ser público. Ya no puede incluir nada que dependa de que
  el repositorio sea privado.
- **Un contribuyente externo cambia un manifiesto sin poder generar el lockfile** (no tiene la
  herramienta, que es privada): el check falla en la coherencia del lockfile; el mantenedor lo
  genera y lo sube a la rama del PR. Lo explica la guía para contribuyentes del repositorio de
  paquetes.
- **Entradas de auditoría anteriores a esta fase**: no tienen versión de la herramienta. Se
  conservan tal cual (el registro es de solo añadir) y se entiende que las produjo el commit del
  repositorio único que ya registran.

## Requirements *(mandatory)*

### Functional Requirements

**Reparto**

- **FR-001**: El repositorio de paquetes MUST contener los manifiestos y lockfiles, la
  configuración del repositorio APT, el estado publicado, el registro de auditoría, la guía del
  usuario final y la definición del pipeline. MUST NOT contener código de la herramienta.
- **FR-002**: El repositorio de la herramienta MUST contener el código, la definición de la
  imagen del runner, la instalación del host, las pruebas, las specs y la documentación de
  operación. MUST NOT contener manifiestos, lockfiles ni estado del repositorio APT reales; sus
  pruebas usan datos de prueba propios.
- **FR-003**: El repositorio de paquetes MUST ser público y su rama principal MUST estar
  protegida (PR obligatorio, sin force push, sin borrado, sin excepciones) con el plan gratuito
  de la forja. El de la herramienta MUST ser privado.

**Versión fijada de la herramienta**

- **FR-004**: El repositorio de paquetes MUST declarar en un archivo versionado qué versión de la
  herramienta usa, identificada por su commit.
- **FR-005**: Toda ejecución del pipeline MUST comprobar, antes de cualquier otro paso, que la
  herramienta que va a usar es exactamente la versión declarada por el commit de paquetes que
  ejecuta, y fallar si no está disponible, con un mensaje que nombre la versión pedida, las
  disponibles y la orden para añadirla.
- **FR-006**: El runner MUST poder tener disponibles varias versiones de la herramienta a la vez,
  para que actualizarla no haga fallar las ejecuciones que todavía fijan la anterior.
- **FR-007**: La herramienta MUST conocer su propia versión, y la compilación de la imagen MUST
  negarse si el código tiene cambios sin commit.
- **FR-007a**: La compilación de la imagen MUST ejecutar antes todas las pruebas de la
  herramienta en la PC y negarse si alguna falla. El repositorio de la herramienta no tiene CI
  propio ni runners registrados: el runner de la PC solo atiende al repositorio de paquetes.
- **FR-008**: Un cambio de la versión fijada MUST tratarse como un cambio de código compartido
  (FR-010 de la fase 003): el check del PR reconstruye todos los paquetes publicados con la
  versión nueva, los compara con lo publicado y falla si cambia el `.deb` de una versión ya
  publicada sin subir su `revision`. Lo mismo en la ejecución de la rama principal antes de
  publicar.
- **FR-009**: El PR que cambia la versión fijada MUST enlazar, en su descripción o en el resumen
  del check, las diferencias entre la versión anterior y la nueva en el repositorio de la
  herramienta.

**Aislamiento**

- **FR-010**: Ningún job MUST tener credenciales con acceso al repositorio de la herramienta, ni
  descargar su código. La herramienta llega a los jobs ya compilada desde la PC del mantenedor,
  y ningún job puede modificarla.
- **FR-011**: El código fuente de la herramienta MUST NOT estar en el entorno del runner ni en el
  sistema de archivos de los jobs. El programa compilado sí está, porque es lo que se ejecuta.
- **FR-012**: El runner confiable MUST aceptar solo el workflow de publicación de la rama
  principal del repositorio de paquetes, y rechazar cualquier job de otro repositorio, de otra
  rama o de otro workflow.
- **FR-013**: Los PRs de forks en el repositorio de paquetes MUST requerir la aprobación del
  mantenedor en cada ejecución (FR-013 de la fase 003), con la configuración gratuita de la forja.

**Auditoría**

- **FR-014**: Cada entrada nueva de auditoría (publicación, reconstrucción y retiro) MUST
  registrar, además de lo que exige la fase 003, la versión de la herramienta que la produjo.
- **FR-015**: Las entradas anteriores a esta fase MUST conservarse sin cambios.

**Migración**

- **FR-016**: La migración MUST conservar en cada repositorio la historia de git de los archivos
  que contiene.
- **FR-017**: La migración MUST NOT crear una generación nueva del repositorio APT ni cambiar su
  estado, su llave, su URL ni lo que sirve el hosting.
- **FR-018**: Tras la migración, el pipeline MUST quedar registrado solo en el repositorio de
  paquetes, y la comprobación de configuración de la forja (`ci doctor`) MUST pasar contra él sin
  plan de pago.

**Pruebas de la herramienta**

- **FR-019**: Todas las pruebas de la herramienta MUST pasar en un clon limpio de su repositorio,
  sin el de paquetes.

### Key Entities

- **Versión fijada de la herramienta**: el commit del repositorio de la herramienta que declara
  el repositorio de paquetes. Una por commit de paquetes. Cambia solo por PR.
- **Versión disponible en el runner**: una herramienta compilada por el mantenedor desde un
  commit sin cambios pendientes e incluida en la imagen del runner. Puede haber varias.
- **Entrada de auditoría**: la de la fase 003 más la versión de la herramienta.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Tras la migración, el repositorio público sirve la misma generación que antes
  (mismo serial), `repo verify --dest` pasa y un `apt update` en contenedores limpios de las 5
  suites no da avisos.
- **SC-002**: La primera ejecución de la rama principal del repositorio de paquetes no publica
  nada nuevo, y una reconstrucción de cualquier versión publicada produce los mismos sha256.
- **SC-003**: Un PR de un fork diseñado para obtener el código de la herramienta (clonar,
  descargar, buscar en disco, leer credenciales) no lo consigue en ninguno de los intentos, y
  los logs no contienen credenciales.
- **SC-004**: Un PR que fija una versión de la herramienta que el runner no tiene falla en menos
  de 1 minuto, antes de construir, con un mensaje que nombra la versión.
- **SC-005**: Durante una actualización de la herramienta, ninguna ejecución falla por la
  transición: 0 ejecuciones fallidas entre que la versión nueva está disponible y el merge del PR
  que la fija.
- **SC-006**: El 100 % de las entradas de auditoría creadas tras la migración incluyen la
  versión de la herramienta.
- **SC-007**: La comprobación de configuración de la forja pasa en verde contra el repositorio
  de paquetes con el plan gratuito.
- **SC-008**: Las pruebas de la herramienta pasan en un clon limpio sin el repositorio de
  paquetes.

## Assumptions

- **Nombres**: el repositorio de la herramienta es el actual `opentepache/debfactory` (privado).
  El de paquetes es uno nuevo y público en la misma organización; su nombre se fija en el plan.
- **Plan de la forja**: la organización sigue en el plan gratuito. Protección de ramas y
  aprobación de forks solo se exigen en el repositorio de paquetes, que es público.
- **Seguridad de la rama principal de la herramienta**: sin protección (el plan gratuito no la
  da en privados). Se acepta porque solo el mantenedor escribe en él y porque nada de esa rama
  se ejecuta con la llave hasta que un PR revisado en el repositorio de paquetes lo fija
  (FR-008, FR-009).
- **Reproducibilidad para terceros**: mientras la herramienta sea privada, solo el mantenedor
  puede reproducir un `.deb` publicado. La auditoría ya registra todo lo necesario para que
  cualquiera pueda hacerlo cuando la herramienta se publique.
- **Tareas pendientes de la fase 003**: las que dependen de la forja (T020, T021, T033, T039,
  T045–T047, T053, T058, T064, T068–T070) se ejecutan contra el repositorio de paquetes una vez
  hecha esta fase.
- **Fuera de alcance**: hacer pública la herramienta, varios repositorios de paquetes
  publicando en el mismo repositorio APT, y distribuir la herramienta como `.deb` en su propio
  repositorio APT.
