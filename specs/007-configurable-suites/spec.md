# Feature Specification: Suites configurables y Debian forky

**Feature Branch**: `007-configurable-suites`

**Created**: 2026-11-26

**Status**: Draft

**Input**: User description: "Agregar soporte para Debian forky (Debian 14, testing) y, en general,
que agregar una suite sea solo configuración. El sitio debe tomar sus suites de la configuración:
distribución, nombre legible y versión, sin tabla fija en el código. `ci rebuild` de una versión ya
publicada debe probarla en todas las suites configuradas y, si el sha256 coincide, publicarla en
las suites que le falten. Así aqua, bob, buf, fnm y k9s llegan a forky sin esperar versiones
nuevas. El bloque "Detectar" de la portada elige forky cuando `os-release` dice forky; la pestaña
Debian sigue ofreciendo sid. Agregar forky a `config/debfactory.yaml` en `packages` y actualizar
`docs/usuario.md`. Criterios de aceptación: los comandos de la portada funcionan en un contenedor
limpio de forky (amd64 y arm64); tras el rebuild de cada paquete, `apt install` en forky instala la
versión publicada; agregar una suite futura no exige cambiar código."

## Contexto

La constitución v1.8.0 sacó la lista de suites de su texto: las suites las define la
configuración del repositorio de paquetes, y agregar una es un PR de configuración siempre que la
suite cumpla los criterios constitucionales (distribución basada en Debian o Ubuntu con `.deb`,
imagen de contenedor oficial, mismas pruebas, regla "falla en una, no se publica en ninguna"). Esta
fase hace que el código cumpla esa promesa y la estrena con Debian forky.

## Clarifications

### Session 2026-11-26

- Q: Si forky (testing) rompe la prueba de instalación por una causa ajena al paquete, ¿qué pasa
  con las demás suites? → A: se mantiene la regla "falla en una, no se publica en ninguna" para
  todas las suites configuradas (principio VII); el fallo se notifica y el mantenedor reintenta o
  excluye forky en el manifiesto de ese paquete con su motivo (FR-006, FR-012).
- Q: Cuando se agrega una suite, ¿quién lleva a ella las versiones ya publicadas? → A: el
  pipeline, automáticamente: la ejecución de `main` detecta las versiones publicadas a las que les
  falta una suite configurada no excluida y las amplía en esa misma ejecución; la reconstrucción
  manual de un paquete sigue disponible para reintentar (FR-004, FR-013).
- Q: Al ampliar a una suite nueva, ¿solo la versión actual o también las anteriores conservadas?
  → A: solo la versión actual (la más alta) de cada paquete; las anteriores no llegan a la suite
  nueva, y volver atrás allí será posible con las versiones que se publiquen desde ahora (FR-007).

## User Scenarios & Testing *(mandatory)*

Actores:

- **El mantenedor**: agrega o ajusta suites con un PR de configuración y lanza las ejecuciones
  manuales del pipeline.
- **El pipeline**: construye, prueba, publica y genera el sitio (fases 003 y 006).
- **El usuario de Debian forky** (testing): quiere instalar los paquetes del repositorio en su
  sistema.

### User Story 1 - Agregar una suite es solo configuración (Priority: P1)

El mantenedor agrega forky a la configuración con un PR: su imagen de contenedor, su
distribución, su nombre legible y su versión. Sin tocar el código de la herramienta, el pipeline
la incluye en todo: las pruebas de instalación de cada PR, la publicación, el repositorio firmado,
el sitio y las pruebas de punta a punta.

**Why this priority**: es lo que pidió la enmienda de la constitución; sin esto, cada suite nueva
sigue costando una versión nueva de la herramienta.

**Independent Test**: con la configuración de prueba más una suite ficticia adicional, sin ningún
otro cambio, el pipeline la prueba, la publica y el sitio la muestra con su nombre legible.

**Acceptance Scenarios**:

1. **Given** una suite nueva agregada solo en la configuración, **When** un PR cambia un paquete,
   **Then** la prueba de instalación incluye esa suite en las dos arquitecturas.
2. **Given** una suite nueva, **When** el pipeline publica una versión nueva de un paquete,
   **Then** la versión queda publicada en la suite nueva junto a las demás.
3. **Given** una suite nueva, **When** se genera el sitio, **Then** aparece con el nombre legible y
   la versión que dice la configuración, en la portada, en la disponibilidad de cada paquete y en
   el bloque de su distribución, sin cambios en el código del sitio.
4. **Given** una suite con datos incompletos en la configuración (sin distribución o sin nombre
   legible), **When** se valida la configuración, **Then** falla con un mensaje que nombra la
   suite y el campo.

---

### User Story 2 - Las versiones publicadas llegan a la suite nueva (Priority: P1)

El mantenedor integra el PR que agrega forky. En esa misma ejecución, sin ninguna acción más, el
pipeline detecta que las versiones publicadas (aqua, bob, buf, fnm, k9s) no están en forky, las
reconstruye y las prueba en todas las suites configuradas. Si cada paquete reconstruido es
idéntico al publicado y pasa todas las pruebas, lo publica en las suites donde aún no estaba,
forky entre ellas, sin esperar a que upstream saque una versión nueva. Si algo falla, el
mantenedor puede reintentar un paquete con la reconstrucción manual.

**Why this priority**: sin esto, la suite nueva queda vacía hasta la próxima versión de cada
paquete, que puede tardar meses.

**Independent Test**: en el entorno de integración, publicar un paquete, agregar una suite a la
configuración, ejecutar `main` y comprobar que la versión aparece en la suite nueva con el mismo
archivo y que en las demás no cambia nada; lo mismo con la reconstrucción manual.

**Acceptance Scenarios**:

1. **Given** una versión publicada en todas las suites salvo la nueva, **When** se lanza su
   reconstrucción y el paquete reconstruido es idéntico y pasa las pruebas en todas las suites,
   **Then** se publica en la suite nueva con el mismo archivo, queda registrado en la auditoría y
   el estado y el sitio lo reflejan.
2. **Given** la misma situación, **When** la prueba de instalación falla en la suite nueva,
   **Then** no se publica en ninguna suite nueva, se notifica el fallo con la suite y el motivo, y
   lo ya publicado no cambia.
3. **Given** un paquete cuyo manifiesto excluye la suite nueva, **When** se reconstruye, **Then**
   no se publica en ella y no se considera un fallo.
4. **Given** una versión ya publicada en todas las suites configuradas, **When** se reconstruye,
   **Then** el resultado es "nada que publicar", como hoy.
5. **Given** que el paquete reconstruido no es idéntico al publicado, **When** se reconstruye,
   **Then** falla sin publicar nada, como hoy.
6. **Given** un paquete con varias versiones conservadas, **When** el pipeline lo amplía a la suite
   nueva, **Then** solo la versión actual (la más alta) llega a ella; las anteriores siguen solo
   en sus suites de antes.

---

### User Story 3 - Agregar el repositorio en Debian forky (Priority: P2)

Un usuario de Debian forky abre la portada, copia el bloque "Detectar" y lo pega en su terminal.
El repositorio queda agregado para forky y puede instalar cualquier paquete publicado allí. La
pestaña Debian del sitio ofrece también sid de forma explícita.

**Why this priority**: es el beneficio visible para el usuario final, pero depende de las
historias 1 y 2.

**Independent Test**: en contenedores limpios de forky (amd64 y arm64), ejecutar tal cual los
bloques de la portada e instalar los paquetes publicados en forky.

**Acceptance Scenarios**:

1. **Given** un contenedor limpio de Debian forky, **When** se ejecuta el bloque "Detectar",
   **Then** el repositorio queda configurado para forky, `apt update` termina sin advertencias de
   firma y `apt install` instala las versiones publicadas.
2. **Given** un contenedor limpio de Debian sid, **When** se ejecuta el bloque "Detectar", **Then**
   el repositorio queda configurado para forky (en sid, el sistema declara el codename de testing)
   y los paquetes se instalan igual, porque son los mismos archivos.
3. **Given** la pestaña Debian, **When** el usuario la abre, **Then** ve las suites de Debian de la
   configuración, sid incluida, y el bloque elige la suite estable o de testing del sistema, o sid
   en cualquier otro caso.

---

### User Story 4 - Documentación al día (Priority: P3)

La guía de usuario del repositorio de paquetes lista forky entre las suites, con su nombre legible,
y deja de decir que en sid `os-release` "dice forky pero la suite es sid" como si forky no
existiera.

**Why this priority**: necesaria para no contradecir al sitio, pero sin impacto funcional.

**Independent Test**: la guía de usuario lista las mismas suites que la configuración y sus
bloques probados siguen pasando.

**Acceptance Scenarios**:

1. **Given** la guía de usuario, **When** se compara con la configuración, **Then** lista las
   mismas suites, forky incluida.

---

### Edge Cases

- **Forky es testing**: cambia a diario y puede romper una prueba de instalación por causas ajenas
  al paquete. Un fallo en forky detiene la publicación en todas las suites (FR-012); el mantenedor
  reintenta o excluye forky en el manifiesto de ese paquete con su motivo.
- **Forky se vuelve estable** (Debian 14, previsto para 2027): basta con actualizar su nombre
  legible en la configuración; el bloque "Detectar" sigue funcionando porque el codename no cambia.
  La siguiente testing se agrega igual que forky.
- **Suite nueva sin imagen para una arquitectura**: la prueba de instalación falla en esa
  arquitectura y no se publica; la constitución exige imagen oficial para las suites configuradas.
- **Reconstrucción de una versión retirada**: no se publica en ninguna suite (las versiones
  retiradas no se reutilizan).
- **La ampliación falla y queda pendiente**: la versión sigue en sus suites de antes y no en la
  nueva; cada ejecución de `main` lo reintenta y el aviso sigue abierto (FR-014) hasta que se amplía
  o el manifiesto excluye la suite.
- **Dos reconstrucciones seguidas de la misma versión**: la segunda no tiene suites que agregar y
  termina en "nada que publicar".
- **Volver atrás en la suite nueva**: al principio solo tiene una versión de cada paquete; las
  anteriores llegan con las publicaciones siguientes, hasta el límite de la retención.
- **Suite quitada de la configuración**: fuera de alcance; la constitución exige anunciarlo antes
  en la documentación de usuario y su procedimiento llega en otra fase.
- **Sitio de una suite nueva sin paquetes todavía**: la suite aparece en los bloques de la portada
  y en la disponibilidad como "no disponible" hasta la reconstrucción.

## Requirements *(mandatory)*

### Functional Requirements

**Suites desde la configuración**

- **FR-001**: Cada suite de la configuración MUST declarar, además de su imagen de contenedor y su
  sufijo de nivel 2, su distribución (debian o ubuntu), su nombre legible (p. ej. "Debian 14") y
  su versión corta (p. ej. "14"). La validación de la configuración MUST rechazar una suite sin
  alguno de esos datos, nombrando la suite y el campo.
- **FR-002**: Ningún componente de la herramienta (construcción, pruebas, publicación, sitio,
  pruebas de punta a punta) MUST tener una lista fija de suites ni de sus nombres: todo sale de la
  configuración.
- **FR-003**: El sitio MUST mostrar cada suite con el nombre legible y la versión de la
  configuración, y agrupar las suites por la distribución que declaran.

**Llevar versiones publicadas a suites nuevas**

- **FR-014**: Una versión actual a la que le falta una suite configurada no excluida está en un
  estado transitorio que la constitución (principio VI) no admite como permanente. Mientras dure,
  cada ejecución de `main` MUST volver a intentar la ampliación (FR-013), y el fallo MUST seguir
  notificado (issue abierto) hasta que la versión se amplíe o su manifiesto excluya esa suite con su
  motivo; entonces el aviso se cierra solo.
- **FR-013**: Cada ejecución de `main` MUST detectar las versiones publicadas a las que les falta
  alguna suite configurada no excluida por su manifiesto y ampliarlas (FR-004) en esa misma
  ejecución, sin intervención humana (principio VIII). Las ampliaciones de varios paquetes MUST
  poder ir en la misma publicación que las versiones nuevas de esa ejecución.
- **FR-004**: La ampliación de una versión publicada, sea automática (FR-013) o por reconstrucción
  manual, MUST probarla en todas las suites configuradas no excluidas por su manifiesto. Si el paquete reconstruido es idéntico al publicado
  en cada arquitectura y pasa todas las pruebas, MUST publicarse en las suites configuradas donde
  aún no está, en una sola publicación atómica.
- **FR-005**: Esa publicación MUST reutilizar los mismos archivos ya publicados: no crea una
  versión nueva ni reescribe ninguna.
- **FR-006**: Un fallo en la prueba de cualquier suite MUST detener la publicación en todas las
  suites nuevas, notificarse con la suite, la arquitectura y el motivo, y dejar intacto lo ya
  publicado.
- **FR-007**: Solo la versión actual (la más alta) de cada paquete MUST ampliarse a una suite
  nueva; las versiones anteriores conservadas no. Una versión que ya no es la actual no se amplía,
  tampoco por reconstrucción manual. La retención sigue aplicándose en la suite nueva como en las
  demás.
- **FR-008**: Cada publicación en suites nuevas MUST quedar en la auditoría con el paquete, la
  versión, las suites agregadas, el commit y la ejecución.

- **FR-012**: Las suites de testing (forky) MUST contar para la regla "falla en una, no se publica
  en ninguna" igual que las demás: no existe una suite "no bloqueante". Un fallo allí se notifica
  con la suite y el motivo; la salida es reintentar o excluir la suite en el manifiesto del paquete,
  con su motivo, por PR.

**Portada**

- **FR-009**: El bloque "Detectar" MUST elegir cualquier suite de la configuración cuyo codename
  coincida con el que declara el sistema (forky incluida). Solo si no coincide ninguna y el sistema
  es Debian, elige sid; en otro caso no agrega el repositorio y lo dice (como en la fase 006).
- **FR-010**: La pestaña Debian MUST ofrecer las suites de Debian de la configuración y elegir la
  del sistema si está configurada, o sid en cualquier otro caso.

**Documentación**

- **FR-011**: La guía de usuario del repositorio de paquetes MUST listar las mismas suites que la
  configuración, forky incluida, y explicar que en sid el sistema declara el codename de testing.

### Key Entities

- **Suite**: nombre (codename), imagen de contenedor, sufijo de nivel 2, distribución, nombre
  legible y versión corta. Vive en la configuración.
- **Versión publicada**: paquete, versión, archivos por arquitectura y suites donde está. Una
  reconstrucción puede ampliar sus suites sin cambiar sus archivos.
- **Entrada de auditoría de ampliación**: paquete, versión, suites agregadas, commit y ejecución.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Agregar una suite requiere cambiar cero archivos de código: solo la configuración
  (verificado con una suite ficticia en las pruebas).
- **SC-002**: Los bloques de la portada, copiados tal cual, agregan el repositorio e instalan los
  paquetes en contenedores limpios de forky en amd64 y arm64 (2 de 2), y siguen funcionando en las
  10 combinaciones actuales (12 de 12 en total).
- **SC-003**: Tras integrar el PR que agrega forky, sin ninguna otra acción, `apt install` en forky
  instala la versión publicada de cada uno de los 5 paquetes (5 de 5, salvo exclusiones de su
  manifiesto).
- **SC-004**: Reconstruir una versión ya presente en todas las suites no cambia nada publicado (0
  archivos subidos al repositorio).
- **SC-005**: Un fallo de la prueba en la suite nueva deja lo publicado exactamente igual y llega
  una notificación con la suite y el motivo.

## Assumptions

- La imagen oficial `debian:forky` existe para amd64 y arm64 (comprobado el 2026-11-26) y cumple
  el criterio de la constitución.
- Forky se presenta como "Debian 14" con la indicación de testing en el nombre legible, que el
  mantenedor actualiza cuando sea estable.
- Los paquetes de nivel 1 son el mismo archivo en todas las suites; por eso ampliar las suites de
  una versión no exige reconstruir nada distinto, solo comprobar y probar.
- Las ampliaciones las hace el pipeline solo (FR-013); la reconstrucción manual queda para
  reintentar un paquete concreto.
- Ubuntu no cambia en esta fase; una suite de Ubuntu futura se agrega igual que forky.
- **Fuera de alcance**: quitar o renombrar suites publicadas, derivadas (Linux Mint, Pop!_OS…) y
  sus páginas, y suites de otras arquitecturas.
- **Dependencias**: constitución v1.8.0; pipeline de la fase 003 (`ci rebuild`, auditoría, estado);
  sitio de la fase 006.
