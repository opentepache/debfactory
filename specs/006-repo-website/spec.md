# Feature Specification: Sitio web del repositorio

**Feature Branch**: `006-repo-website`

**Created**: 2026-11-26

**Status**: Draft

**Input**: User description: "Quiero un sitio web estático que sirva como portada del repositorio
para que otras personas lo descubran y lo usen. (…) Página principal: qué es el repo, cómo
agregarlo en Debian y Ubuntu (copiar y pegar), fingerprint de la llave y lista de paquetes con su
versión actual. Una página por paquete: descripción, versión publicada, fecha, licencia, enlace a
upstream, comando de instalación, suites y arquitecturas disponibles, y versiones anteriores. Todo
se genera automáticamente a partir de los manifiestos y del estado real del repositorio; nunca se
edita a mano. Se regenera y publica en cada publicación de paquetes. En español e inglés. Ligero,
sin JavaScript obligatorio, accesible y legible en móvil. Criterios de aceptación: después de
publicar una versión nueva, el sitio muestra esa versión en menos de 10 minutos; los comandos de
instalación de la portada, copiados tal cual en un contenedor limpio, funcionan; agregar un
paquete nuevo no requiere tocar nada del sitio."

## Clarifications

### Session 2026-11-26

- Q: ¿En qué dirección web debe vivir el sitio? → A: en la raíz de `https://apt.opentepache.org`,
  en el mismo hosting y con la misma subida que el repositorio (FR-023).
- Q: ¿Qué idioma debe ver quien abre la raíz del sitio? → A: inglés en la raíz y español en
  `/es/` (FR-016).
- Q: Si los paquetes se publican bien pero falla el sitio, ¿qué debe pasar? → A: los paquetes
  quedan publicados, el sitio anterior sigue en línea, se notifica el fallo y la siguiente
  ejecución del pipeline (publicación o renovación) vuelve a intentar el sitio (FR-013).

## User Scenarios & Testing *(mandatory)*

Hay tres actores:

- **El visitante**: una persona que usa Debian o Ubuntu y llega al sitio buscando una de las
  herramientas, o el repositorio en sí. No conoce debfactory ni el repositorio de paquetes.
- **El mantenedor**: agrega paquetes y publica versiones; no quiere mantener el sitio.
- **El pipeline**: el mismo proceso determinista que publica los paquetes (fase 003). También
  genera y publica el sitio.

### User Story 1 - Agregar el repositorio desde la portada (Priority: P1)

Un visitante llega a la portada, entiende en pocas líneas qué es OpenTepache (herramientas de
desarrollo recientes como `.deb` firmados, solo versiones estables) y ve la lista de paquetes con
su versión actual. Copia el bloque de comandos de su distribución, lo pega en una terminal y el
repositorio queda agregado con la llave verificada. Puede comparar el fingerprint que muestra la
página con el de la llave que descargó.

**Why this priority**: es la razón de ser del sitio. Sin una forma fiable de agregar el
repositorio, nadie lo usa; con solo esta página el sitio ya aporta valor.

**Independent Test**: en un contenedor limpio de cada suite soportada (bookworm, trixie, sid,
noble, resolute; amd64 y arm64), pegar tal cual los comandos de la portada y comprobar que
`apt update` termina sin advertencias de firma y que se puede instalar un paquete de la lista.

**Acceptance Scenarios**:

1. **Given** un contenedor limpio de cualquier suite soportada, **When** el visitante ejecuta los
   comandos de la portada copiados sin modificar, **Then** `apt update` termina sin errores ni
   advertencias de firma y `apt install <paquete>` instala la versión que muestra el sitio.
2. **Given** la portada, **When** el visitante la lee, **Then** ve el fingerprint completo de la
   llave maestra del repositorio, idéntico al de la configuración del repositorio, y cómo
   comprobarlo contra la llave descargada.
3. **Given** la portada, **When** el visitante la lee, **Then** ve cada paquete publicado con su
   versión actual y un enlace a su página.
4. **Given** la portada, **When** el visitante busca cómo quitar el repositorio, **Then** encuentra
   los comandos para quitarlo, incluida la llave.

---

### User Story 2 - Conocer un paquete antes de instalarlo (Priority: P2)

Un visitante llega a la página de un paquete (desde la portada o desde un buscador). Ve qué hace,
la versión publicada y su fecha, la licencia, el enlace al proyecto upstream, el comando para
instalarlo, en qué suites y arquitecturas está disponible y qué versiones anteriores siguen en el
repositorio, con el comando para instalar una de ellas.

**Why this priority**: da la información para decidir si se confía en el paquete y cómo
instalarlo; es lo segundo que busca quien ya decidió agregar el repositorio o llega por un
paquete concreto.

**Independent Test**: generar el sitio con el estado actual y comprobar, para cada paquete, que
su página muestra exactamente los datos del manifiesto y del estado publicado (versión, fecha,
licencia, suites, arquitecturas y versiones conservadas).

**Acceptance Scenarios**:

1. **Given** un paquete publicado, **When** el visitante abre su página, **Then** ve descripción,
   versión publicada, fecha de publicación, licencia, enlace a upstream, comando de instalación,
   suites y arquitecturas disponibles.
2. **Given** un paquete con suites excluidas en su manifiesto (p. ej. `bob` sin `bookworm`),
   **When** el visitante abre su página, **Then** ve solo las suites en las que está publicado y,
   si el manifiesto lo documenta, el motivo de la exclusión.
3. **Given** un paquete con versiones anteriores conservadas en el repositorio, **When** el
   visitante abre su página, **Then** ve esas versiones con su fecha y el comando para instalar
   una versión concreta.
4. **Given** una versión retirada por defecto, **When** el visitante abre la página del paquete,
   **Then** esa versión no aparece como instalable.

---

### User Story 3 - El sitio se mantiene solo (Priority: P2)

El mantenedor publica una versión nueva, retira una defectuosa o agrega un paquete nuevo. Sin
tocar nada del sitio, en menos de 10 minutos el sitio refleja el estado real del repositorio. Si
generar o publicar el sitio falla, el mantenedor recibe una notificación y el sitio anterior sigue
en línea; los paquetes ya publicados no se ven afectados.

**Why this priority**: un sitio que se edita a mano se queda desactualizado y miente sobre lo
publicado; el principio VIII pide que el camino feliz no tenga humanos.

**Independent Test**: publicar una versión nueva de un paquete (o agregar un paquete nuevo) en el
repositorio de prueba y comprobar que, sin ninguna otra acción, el sitio de prueba la muestra en
menos de 10 minutos.

**Acceptance Scenarios**:

1. **Given** una publicación que termina con éxito, **When** pasan 10 minutos, **Then** el sitio
   muestra la versión nueva en la portada y en la página del paquete.
2. **Given** un paquete nuevo agregado como manifiesto y publicado, **When** termina la
   publicación, **Then** el sitio tiene su página y aparece en la portada sin ningún cambio en el
   código o las plantillas del sitio.
3. **Given** un retiro de versión, **When** termina, **Then** el sitio deja de mostrar esa versión
   como instalable.
4. **Given** que falla la generación o la subida del sitio, **When** el pipeline termina, **Then**
   el mantenedor recibe una notificación con la etapa y el motivo, cada página en línea sigue
   completa (de la versión anterior o de la nueva, nunca a medias ni con enlaces rotos), la
   publicación de paquetes cuenta como hecha y la siguiente ejecución deja el sitio al día.
5. **Given** una ejecución sin cambios en lo publicado (p. ej. la renovación periódica de firmas),
   **When** termina, **Then** el contenido visible del sitio no cambia; si el sitio en línea
   había quedado atrasado por un fallo anterior, esa ejecución lo pone al día.

---

### User Story 4 - Leer el sitio en español (Priority: P3)

El sitio se abre en inglés. Un visitante hispanohablante cambia a español (o llega directamente a
la versión en español, bajo `/es/`). Ve el mismo contenido, con los mismos datos y comandos,
traducido.

**Why this priority**: amplía la audiencia, pero el sitio en un idioma ya cumple el objetivo
principal.

**Independent Test**: comprobar que cada página existe en los dos idiomas, que cada una enlaza a
su equivalente en el otro idioma y que los datos y comandos son idénticos en ambas.

**Acceptance Scenarios**:

1. **Given** cualquier página, **When** el visitante elige el otro idioma, **Then** llega a la
   misma página en ese idioma, sin necesidad de JavaScript.
2. **Given** las dos versiones de una página, **When** se comparan, **Then** los datos (versiones,
   fechas, licencias, suites, fingerprint) y los comandos son idénticos.

---

### Edge Cases

- **Descripción con marcado o texto hostil**: la descripción y demás textos vienen de manifiestos
  que pueden incluir contenido de upstream. El sitio los muestra como texto, nunca como código o
  marcado ejecutable.
- **Descripción sin traducción**: si un manifiesto no tiene descripción en español, la página en
  español muestra la del paquete (en inglés), sin romper la generación.
- **Paquete con manifiesto pero sin publicar** (p. ej. un PR aún abierto, o un paquete que nunca
  pasó las pruebas): no aparece en el sitio. El sitio refleja lo publicado, no lo propuesto.
- **Paquete del que se retiraron todas las versiones**: no aparece como instalable.
- **Repositorio vacío** (primera publicación): la portada se genera con la lista vacía y los
  comandos para agregar el repositorio.
- **Dos publicaciones seguidas**: el sitio final corresponde al último estado publicado, nunca a
  uno intermedio.
- **Rotación de la llave**: tras rotar la llave, el fingerprint del sitio cambia en la siguiente
  publicación, igual que el del repositorio.
- **Fallo del sitio en medio de una publicación**: los paquetes ya publicados no se revierten;
  cada página en línea sigue completa (FR-012), el fallo se notifica y la siguiente ejecución del
  pipeline pone el sitio al día.

## Requirements *(mandatory)*

### Functional Requirements

**Contenido**

- **FR-001**: La portada MUST explicar en pocas líneas qué es el repositorio (herramientas de
  desarrollo recientes, `.deb` firmados, solo versiones estables, Debian y Ubuntu, amd64 y arm64).
- **FR-002**: La portada MUST incluir los comandos para agregar el repositorio en Debian y en
  Ubuntu, listos para copiar y pegar, que funcionen sin modificar en todas las suites y
  arquitecturas soportadas, y los comandos para quitarlo.
- **FR-003**: La portada MUST mostrar el fingerprint completo de la llave maestra del repositorio
  y cómo comprobarlo contra la llave descargada.
- **FR-004**: La portada MUST listar cada paquete publicado con su versión actual y un enlace a su
  página.
- **FR-005**: Cada paquete publicado MUST tener una página con: descripción, versión publicada,
  fecha de publicación, licencia, enlace a upstream, comando de instalación, suites y
  arquitecturas en las que está publicado, y las versiones anteriores que sigan en el repositorio
  con su fecha y el comando para instalar una versión concreta.
- **FR-006**: El sitio MUST NOT mostrar como instalable ninguna versión retirada ni ninguna que no
  esté publicada en el repositorio.

**Origen de los datos**

- **FR-007**: Todo el contenido variable del sitio (paquetes, versiones, fechas, suites,
  arquitecturas, fingerprint, URL del repositorio, comandos) MUST generarse a partir de los
  manifiestos, la configuración y el estado publicado del repositorio. Nada se edita a mano.
- **FR-008**: Agregar, retirar o quitar un paquete MUST NOT requerir ningún cambio en el código,
  las plantillas o los textos del sitio.
- **FR-009**: Los datos del sitio MUST coincidir con los del repositorio publicado en el mismo
  momento: el sitio nunca anuncia una versión antes de que esté en el repositorio.
- **FR-010**: Los textos que vienen de manifiestos MUST mostrarse como texto escapado; el sitio
  MUST NOT interpretar marcado o código que contengan.

**Generación y publicación**

- **FR-011**: El sitio MUST regenerarse y publicarse automáticamente, como parte del mismo
  pipeline y sin intervención humana, en cada ejecución del job de publicación: publicación,
  retiro, renovación periódica y también cuando no hay paquetes que publicar. Así llegan solos los
  cambios que solo tocan el sitio (p. ej. una traducción) y se repara un sitio atrasado. Si el
  sitio no cambió, no se sube nada.
- **FR-012**: La actualización del sitio MUST ser atómica página a página para el visitante:
  cada página que recibe es completa y coherente consigo misma (de la versión anterior o de la
  nueva), nunca a medias, sin estilos ni con enlaces a páginas que no existen. Durante la subida,
  o tras un fallo a medias hasta la siguiente ejecución, dos páginas MAY mostrar versiones
  distintas; el hosting estático no permite reemplazar varios archivos a la vez.
- **FR-013**: Un fallo al generar o publicar el sitio MUST notificarse al mantenedor con la etapa
  y el motivo (principio VIII), MUST dejar en línea el sitio anterior y MUST NOT revertir ni
  bloquear la publicación de paquetes. La siguiente ejecución del pipeline que publique
  (publicación, retiro o renovación periódica de firmas) MUST volver a generar y subir el sitio
  si el que está en línea no corresponde al estado publicado.
- **FR-014**: La generación MUST ser determinista: con los mismos manifiestos, configuración y
  estado, el sitio generado es idéntico byte a byte.
- **FR-015**: El mantenedor MUST poder generar el sitio en local con un comando documentado, para
  revisarlo antes de publicar, y publicarlo en el hosting de prueba igual que el repositorio.
- **FR-023**: El sitio MUST servirse en la raíz de la URL pública del repositorio (hoy
  `https://apt.opentepache.org`), en el mismo hosting, y MUST subirse en el mismo paso que el
  repositorio. Sus archivos MUST NOT interferir con las rutas del repositorio APT (`dists/`,
  `pool/`, la llave pública).

**Idiomas**

- **FR-016**: Cada página MUST existir en inglés y en español, con un enlace visible a su
  equivalente en el otro idioma, y con el idioma declarado en la propia página. El inglés MUST
  estar en la raíz del sitio y el español bajo `/es/`, con la misma estructura de rutas. No hay
  redirección automática por idioma del navegador.
- **FR-017**: Los datos y comandos MUST ser idénticos en los dos idiomas; solo cambian los textos.

**Calidad**

- **FR-018**: Toda la funcionalidad (leer, navegar, cambiar de idioma, copiar comandos) MUST
  funcionar sin JavaScript. Cualquier JavaScript que se agregue es una mejora opcional.
- **FR-019**: El sitio MUST cumplir WCAG 2.1 nivel AA (contraste, navegación por teclado,
  estructura de encabezados, textos alternativos, idioma declarado).
- **FR-020**: El sitio MUST leerse en pantallas de 320 px de ancho sin desplazamiento horizontal
  de la página; los bloques de comandos MAY desplazarse dentro de su propio recuadro.
- **FR-021**: El sitio MUST NOT cargar recursos de terceros (fuentes, scripts, analítica,
  rastreadores, CDN): todo se sirve desde el mismo hosting.
- **FR-022**: Cada página MUST pesar menos de 100 KB transferidos, sumando todos sus recursos.

### Key Entities

- **Portada**: presentación del repositorio, comandos para agregar y quitar, fingerprint, lista
  de paquetes. Una por idioma.
- **Página de paquete**: datos de un paquete publicado. Una por paquete y por idioma.
- **Paquete publicado**: nombre, descripción, licencia, upstream y suites excluidas (con motivo)
  del manifiesto; versiones conservadas con su fecha, suites y arquitecturas del estado publicado.
- **Configuración del repositorio**: URL pública, suites, arquitecturas, componente, fingerprint
  de la llave maestra.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Después de una publicación correcta, el sitio público muestra la versión nueva en
  menos de 10 minutos, sin ninguna acción humana.
- **SC-002**: Los comandos de la portada, copiados sin modificar en un contenedor limpio de cada
  una de las 5 suites y 2 arquitecturas, agregan el repositorio y permiten instalar cualquier
  paquete listado, con `apt update` sin advertencias de firma (10 de 10 combinaciones).
- **SC-003**: Agregar un paquete nuevo hasta verlo en el sitio requiere cero cambios en el sitio:
  solo el manifiesto y la publicación habitual.
- **SC-004**: En el 100 % de las páginas, versiones, fechas, suites, arquitecturas y fingerprint
  coinciden con el repositorio publicado.
- **SC-005**: Cada página pesa menos de 100 KB, funciona con JavaScript desactivado y se lee a
  320 px de ancho sin desplazamiento horizontal.
- **SC-006**: Una auditoría automática de accesibilidad no reporta errores de nivel A ni AA en
  ninguna página.
- **SC-007**: Ante un fallo del sitio, el mantenedor recibe una notificación, ninguna página en
  línea queda a medias y la siguiente ejecución del pipeline deja el sitio al día.

## Assumptions

- **Hosting**: decidido en Clarifications (FR-023). No hace falta infraestructura nueva
  (principio IX). Un dominio aparte (p. ej. `opentepache.org`) queda fuera de esta fase.
- **Idioma por defecto**: decidido en Clarifications (FR-016). Sin redirección automática: el
  hosting estático no la permite sin JavaScript o servicios adicionales.
- **Descripciones**: la descripción del paquete sale del manifiesto (la del `.deb`, en inglés). El
  manifiesto MAY tener una descripción en español; si no la tiene, la página en español usa la
  disponible.
- **Versiones anteriores**: son las que el repositorio conserva (por defecto las 3 últimas). Las
  ya eliminadas por la política de retención no aparecen.
- **Comandos**: son los mismos que documenta la guía de usuario del repositorio de paquetes
  (deb822 `.sources` con `Signed-By`). El sitio y la guía no deben contradecirse; el sitio pasa a
  ser la referencia pública.
- **Fuera de alcance**: búsqueda, estadísticas de descargas, analítica, comentarios, registro de
  cambios de upstream, noticias o blog, y dominio propio para el sitio.
- **Dependencias**: el pipeline de publicación de la fase 003 y el estado publicado del
  repositorio (fases 002 y 004).
