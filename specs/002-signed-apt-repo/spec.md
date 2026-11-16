# Feature Specification: Repositorio APT firmado

**Feature Branch**: `002-signed-apt-repo`

**Created**: 2026-11-12

**Status**: Draft

**Input**: User description: "Quiero publicar los paquetes construidos en un repositorio APT
firmado que cualquier máquina Debian o Ubuntu pueda agregar con unos pocos comandos. (…) Fuera
de alcance: CI, detección automática de versiones, sitio web."

## Clarifications

### Session 2026-11-12

- Q: ¿Dónde vive cada parte de la llave de firma? → A: llave maestra fuera de línea (solo en
  el respaldo cifrado); subllave de firma en la PC, con frase de paso y caducidad de 2 años. Se
  renueva o se cambia la subllave sin cambiar el fingerprint publicado.
- Q: ¿Los índices firmados deben caducar (`Valid-Until`)? → A: no en esta fase. Es un riesgo
  aceptado y se revisa cuando exista automatización.
- Q: ¿Qué es la fuente de verdad sobre las versiones publicadas? → A: un archivo de estado
  versionado en git (versiones publicadas con su sha256, más los retiros). El árbol del
  repositorio se regenera a partir de él, y si se pierde la PC se reconstruye desde git.

## User Scenarios & Testing *(mandatory)*

Hay dos actores:

- **El mantenedor**: publica, retira y sincroniza desde su PC Debian, con los `.deb` que produjo
  la fase 001 (construidos, con lintian limpio y probados).
- **El usuario final**: tiene una máquina Debian o Ubuntu y agrega el repositorio para instalar
  y actualizar las herramientas con apt.

Los paquetes de referencia son los cuatro de la constitución v1.5.0: `fnm`, `bob`, `buf` y
`aqua`. La descripción original nombraba `pnpm` y `tenv`, pero la constitución los dejó fuera
del alcance inicial. `bob` excluye `bookworm`.

### User Story 1 - Publicar los paquetes en un repositorio firmado (Priority: P1)

El mantenedor ejecuta una orden para publicar uno o varios paquetes ya construidos y probados.
El sistema hace tres cosas:

1. Agrega cada `.deb` a todas las suites no excluidas en las que debe estar.
2. Regenera los índices.
3. Firma los índices con la llave dedicada del repositorio.

El resultado es un árbol de repositorio completo y coherente en disco.

**Why this priority**: sin un repositorio firmado no hay nada que agregar, sincronizar ni
documentar. Es el gate 7 del flujo de la constitución.

**Independent Test**: publicar los 8 `.deb` de la fase 001 en un repositorio vacío. Después,
comprobar desde la PC, sin red externa, cuatro cosas:

- Cada suite tiene índices firmados para `amd64` y `arm64`.
- La firma verifica con la llave pública publicada.
- Cada paquete aparece en las suites que le corresponden y en ninguna otra.
- `bob` no aparece en `bookworm`.

**Acceptance Scenarios**:

1. **Given** un repositorio vacío y los `.deb` de `fnm 1.39.0-0~tepache1` para `amd64` y `arm64`,
   **When** el mantenedor publica `fnm`, **Then** las 5 suites contienen `fnm 1.39.0-0~tepache1` en
   ambas arquitecturas. Además, los índices de cada suite están firmados y cada `.deb` existe una
   sola vez en el repositorio, aunque lo usen las 5 suites.
2. **Given** `bob`, que excluye `bookworm`, **When** se publica, **Then** aparece en `trixie`,
   `sid`, `noble` y `resolute`, y no en `bookworm`.
3. **Given** un `.deb` sin prueba de instalación aprobada, o cuyo contenido no coincide con el
   que se probó, **When** el mantenedor intenta publicarlo, **Then** la publicación se rechaza.
   El mensaje nombra el paquete, la versión, la arquitectura y el motivo, y el repositorio queda
   intacto.
4. **Given** una versión ya publicada, **When** se publica de nuevo el mismo `.deb` idéntico,
   **Then** no cambia nada y la orden termina con éxito. **When** se intenta publicar un `.deb`
   distinto con la misma versión, **Then** se rechaza, porque las versiones no se reescriben
   (constitución VI).

---

### User Story 2 - Agregar el repositorio e instalar siguiendo solo la documentación (Priority: P1)

El usuario final lee la documentación y, con unos pocos comandos copiados de ella, hace tres
cosas: descarga la llave pública a un keyring dedicado, crea un archivo de fuente en formato
deb822 que limita esa llave a este repositorio (`Signed-By`) y ejecuta `apt update`. Luego
instala las herramientas con `apt install`. La documentación también explica cómo verificar el
fingerprint de la llave y cómo quitar el repositorio.

**Why this priority**: es el valor visible del proyecto. Un repositorio que nadie consigue
agregar sin advertencias no sirve.

**Independent Test**: levantar un contenedor limpio de cada suite y de cada arquitectura (arm64
por emulación) apuntando al repositorio servido en local. Seguir al pie de la letra los comandos
de la documentación y comprobar tres cosas:

- `apt update` termina sin advertencias ni errores de firma.
- `apt install` instala las versiones publicadas.
- Los pasos de "quitar el repositorio" dejan el sistema sin rastro de la fuente ni de la llave.

**Acceptance Scenarios**:

1. **Given** un contenedor limpio de `trixie`, **When** el usuario ejecuta los comandos de la
   documentación y luego `apt install fnm bob buf aqua`, **Then** se instalan las versiones
   publicadas y `apt update` no muestra advertencias.
2. **Given** un contenedor limpio de `bookworm`, **When** el usuario sigue la documentación,
   **Then** `apt install fnm buf aqua` funciona. La documentación indica que `bob` no está
   disponible en esa suite y por qué.
3. **Given** el repositorio agregado, **When** el usuario sigue la sección "quitar el
   repositorio", **Then** desaparecen el archivo de fuente y la llave y `apt update` deja de
   consultarlo. Los paquetes instalados se conservan salvo que el usuario elija desinstalarlos,
   también documentado.
4. **Given** la documentación, **When** el usuario compara el fingerprint de la llave descargada
   con el publicado, **Then** dispone de un comando documentado que muestra el fingerprint para
   compararlo.

---

### User Story 3 - Publicar todo o nada (Priority: P1)

Una publicación se aplica completa o no se aplica: todas las suites y arquitecturas que le
corresponden cambian a la vez, o ninguna cambia. Esto vale tanto para el árbol local como para
lo que ven los clientes en el hosting.

**Why this priority**: lo exige la constitución (VII: "si falla en una, no se publica en
ninguna"; flujo, gate 7: publicación atómica). Un repositorio a medias rompe `apt update` a los
usuarios.

**Independent Test**: provocar tres fallos: falta el `.deb` de una arquitectura, falla la firma
y se corta la sincronización a mitad. En cada caso, comprobar que el repositorio local y el
remoto siguen sirviendo exactamente el estado anterior y que `apt update` sigue funcionando.

**Acceptance Scenarios**:

1. **Given** que falta el `.deb` `arm64` de un paquete, **When** el mantenedor lo publica,
   **Then** la publicación se aborta antes de tocar el repositorio. El mensaje nombra el
   paquete, la versión y la arquitectura que faltan.
2. **Given** que la firma falla (llave no disponible, frase de paso incorrecta o llave
   caducada), **When** se publica, **Then** la publicación se aborta y el repositorio conserva
   sus índices firmados anteriores, sin índices sin firmar ni mezclados.
3. **Given** que la sincronización al hosting se interrumpe a mitad, **When** un cliente hace
   `apt update` en ese momento, **Then** ve el estado anterior completo o el nuevo completo.
   Nunca ve un índice que apunte a un archivo inexistente. Repetir la sincronización la
   completa.
4. **Given** una publicación de varios paquetes en la que uno no cumple los requisitos, **When**
   se ejecuta, **Then** no se publica ninguno y el informe lista todos los motivos.

---

### User Story 4 - Actualizar, conservar versiones anteriores y retirar una defectuosa (Priority: P2)

El mantenedor publica una versión nueva y los usuarios la reciben con `apt upgrade`. El
repositorio conserva las últimas versiones de cada paquete (3 por defecto, configurable), así
que un usuario puede volver a una anterior con apt. Si una versión resulta defectuosa, el
mantenedor la retira de todas las suites con una orden. El retiro queda registrado para que
esa versión no vuelva a publicarse por error.

**Why this priority**: es el ciclo de vida normal tras la primera publicación. Sin retiro, un
paquete roto quedaría servido indefinidamente.

**Independent Test**: en un contenedor con el repositorio agregado:

1. Instalar `fnm` en su versión publicada.
2. Publicar una versión nueva y hacer `apt upgrade`.
3. Retirar la versión nueva.
4. Hacer `apt update` y luego `apt install fnm=<versión anterior>` (con permiso de downgrade
   explícito, tal como lo indique la documentación).
5. Comprobar en cada paso la versión instalada.

**Acceptance Scenarios**:

1. **Given** `fnm 1.39.0-0~tepache1` instalado, **When** se publica `fnm 1.40.0-0~tepache1` y el
   usuario ejecuta `apt update && apt upgrade`, **Then** queda instalada `1.40.0-0~tepache1`.
2. **Given** 4 versiones publicadas sucesivamente con la retención por defecto (3), **When** se
   publica la cuarta, **Then** el repositorio ofrece solo las 3 más recientes y el archivo de la
   más antigua deja de estar en el repositorio.
3. **Given** `1.40.0-0~tepache1` publicada y defectuosa, **When** el mantenedor la retira, **Then**
   desaparece de las 5 suites en una sola operación atómica, y la versión vigente pasa a ser la
   más reciente que queda (`1.39.0-0~tepache1`).
4. **Given** la versión retirada, **When** el usuario que la tiene instalada hace
   `apt update && apt upgrade`, **Then** apt no la cambia automáticamente. **When** ejecuta el
   downgrade explícito documentado, **Then** queda instalada `1.39.0-0~tepache1`.
5. **Given** una versión retirada, **When** el mantenedor intenta publicarla otra vez con el
   mismo número, **Then** se rechaza e indica que debe incrementar `revision` o publicar otra versión
   upstream (constitución VI).
6. **Given** que se pide retirar la única versión publicada de un paquete, **When** se ejecuta,
   **Then** se exige una confirmación explícita (un indicador en la orden), porque el paquete
   desaparecería del repositorio.

---

### User Story 5 - Sincronizar con el hosting local y el público (Priority: P2)

El mantenedor sincroniza el repositorio generado con un destino. Puede ser un directorio servido
en local, para pruebas con contenedores, o el hosting estático público (S3-compatible, hoy
Cloudflare R2). El destino se elige por nombre en la configuración. Las credenciales del destino
público nunca están en el repositorio git ni aparecen en la salida.

**Why this priority**: sin sincronizar, el repositorio solo existe en la PC del mantenedor. Va
detrás de P1 porque el destino local basta para validar todo lo anterior.

**Independent Test**:

1. Sincronizar con el destino local.
2. Servirlo por HTTP a los contenedores y repetir las pruebas de US2.
3. Sincronizar con un destino S3-compatible de pruebas y comprobar que el contenido remoto
   coincide byte a byte con el local.
4. Comprobar que los archivos que ya no se usan se borran solo después de publicar los índices
   nuevos.

**Acceptance Scenarios**:

1. **Given** un repositorio local publicado, **When** el mantenedor sincroniza al destino
   "local", **Then** el directorio de destino contiene una copia exacta, servible por HTTP
   estático sin configuración especial.
2. **Given** el destino público configurado y sus credenciales en el entorno del mantenedor,
   **When** sincroniza, **Then** se suben primero los archivos de paquete nuevos, después los
   índices y por último los índices firmados de entrada de cada suite. Solo al final se borran
   los archivos que ya nadie referencia.
3. **Given** una sincronización sin cambios pendientes, **When** se ejecuta, **Then** no sube
   nada y lo informa.
4. **Given** credenciales ausentes o inválidas, **When** se sincroniza al destino público,
   **Then** la orden falla con un mensaje que nombra el destino y la causa, sin imprimir la
   credencial.

---

### User Story 6 - Custodiar, respaldar y rotar la llave de firma (Priority: P3)

El mantenedor genera una llave GPG dedicada al repositorio siguiendo un procedimiento
documentado. La llave tiene dos partes:

- **Llave maestra**: identifica al repositorio. Su fingerprint es el que se publica. Vive solo
  en un respaldo cifrado fuera de línea y nunca en la PC de build.
- **Subllave de firma**: firma los índices. Vive en la PC, protegida con frase de paso y con
  caducidad de 2 años.

El procedimiento también cubre cuatro tareas:

- Respaldar la llave maestra y la subllave fuera de la máquina.
- Recuperarlas desde el respaldo.
- Renovar o rotar la subllave sin que cambie el fingerprint publicado.
- Revocar la subllave si la PC se compromete, y rotar la llave maestra en el caso raro de que
  se pierda o se comprometa, con un periodo en el que ambas son válidas.

La llave pública y el fingerprint de la llave maestra se publican en el repositorio y en la
documentación.

**Why this priority**: la llave se crea una sola vez y la rotación es poco frecuente, pero sin
procedimiento escrito una pérdida o un compromiso de la llave no tiene salida. La constitución
III exige el respaldo documentado.

**Independent Test**: seguir el procedimiento en un entorno desechable:

1. Crear la llave maestra y la subllave de firma.
2. Respaldarlas y quitar la llave maestra de la PC.
3. Borrar la subllave del llavero y restaurarla.
4. Publicar con la subllave restaurada.
5. Rotar a una subllave nueva.
6. Revocar la subllave anterior.

En cada paso, comprobar que un cliente que solo siguió la documentación sigue haciendo
`apt update` sin advertencias.

**Acceptance Scenarios**:

1. **Given** la llave creada según el procedimiento, **When** se inspecciona el repositorio git,
   el historial y la salida de todas las órdenes, **Then** no aparece material de la llave
   privada ni su frase de paso.
2. **Given** el respaldo documentado, **When** la subllave se borra de la PC y se restaura desde
   el respaldo, **Then** la siguiente publicación firma con la misma subllave y el
   fingerprint de la llave maestra no cambia.
3. **Given** la PC de build, **When** se inspecciona su llavero, **Then** contiene la subllave
   de firma y no la parte secreta de la llave maestra.
4. **Given** una subllave renovada o rotada, **When** un cliente ejecuta `apt update` tras
   refrescar el keyring publicado como indica la documentación, **Then** no ve errores y no
   tiene que cambiar el archivo de fuente ni el fingerprint que verificó.
5. **Given** una rotación de la llave maestra (solo por pérdida o compromiso), **When** un
   cliente con el keyring anterior ejecuta `apt update` durante el periodo de transición,
   **Then** no ve errores. El keyring publicado contiene ambas llaves y la documentación
   explica cómo refrescarlo.
6. **Given** la subllave cerca de su fecha de caducidad, **When** el mantenedor publica,
   **Then** recibe un aviso con los días restantes, antes de que los clientes empiecen a ver
   errores.

---

### Edge Cases

- **Suite excluida**: un paquete que excluye una suite nunca aparece en ella, ni siquiera en sus
  versiones anteriores. Si una versión nueva deja de excluir una suite, entra en ella y las
  versiones anteriores siguen fuera.
- **Exclusión nueva**: si una versión nueva excluye una suite que antes no excluía, las
  versiones anteriores siguen en esa suite y el informe lo advierte. Los usuarios de esa suite
  se quedan en la anterior.
- **Paquete sin versiones**: retirar la última versión con confirmación deja el paquete fuera de
  todas las suites. Los índices siguen siendo válidos aunque una suite quede sin paquetes.
- **Retención mayor que el historial**: con menos versiones que el máximo, no se borra nada.
- **Varias versiones de la misma versión upstream** (`-0~tepache1` y `-0~tepache2`) cuentan como
  versiones distintas a efectos de retención.
- **Archivo de paquete compartido**: el mismo `.deb` usado en varias suites se guarda una sola
  vez y solo se borra cuando ninguna suite lo referencia.
- **Árbol local desincronizado** del archivo de estado (borrado, editado o de otra rama): la
  siguiente operación de escritura aborta y sugiere regenerar el árbol (FR-037).
- **Disco lleno o error de escritura** durante la generación local: la publicación se aborta y
  el árbol servido sigue siendo el anterior.
- **Llave caducada o revocada**: firmar falla y se aborta. La publicación nunca deja índices sin
  firmar.
- **Órdenes concurrentes**: dos publicaciones o sincronizaciones simultáneas sobre el mismo
  repositorio están prohibidas. La segunda falla de inmediato con un mensaje claro y no espera
  ni corrompe nada.
- **Hosting con caché o consistencia eventual**: un cliente que descarga índices nuevos cuyos
  archivos aún no son visibles no debe recibir 404. Por eso el orden de subida es paquetes,
  índices y firma de entrada, y el borrado va al final.
- **Reloj del cliente desfasado**: los índices no caducan en esta fase, así que un reloj
  atrasado o adelantado no produce errores de firma. Es una decisión registrada en las
  suposiciones.
- **Cliente con apt antiguo** (`bookworm`): el formato deb822 con `Signed-By` funciona en todas
  las suites soportadas. La documentación no ofrece la variante de una línea.

## Requirements *(mandatory)*

### Functional Requirements

#### Estructura del repositorio

- **FR-001**: El repositorio MUST ofrecer las suites `bookworm`, `trixie`, `sid`, `noble` y
  `resolute`, el componente `main` y las arquitecturas `amd64` y `arm64`, tomadas de la
  configuración compartida (la misma que usa la fase 001).
- **FR-002**: Un mismo `.deb` de nivel 1 MUST publicarse en todas las suites no excluidas por su
  manifiesto y guardarse una sola vez en el repositorio (constitución VI).
- **FR-003**: Las suites excluidas en el manifiesto de la versión publicada MUST NOT recibir esa
  versión.
- **FR-004**: Cada suite MUST tener índices de paquetes por arquitectura y un índice de suite
  firmado, en las dos formas que apt acepta: firma integrada y firma separada.
- **FR-004a**: Los índices de suite MUST NOT declarar fecha de caducidad (`Valid-Until`) en
  esta fase. *Sustituido por FR-030 de la fase 003 (`Valid-Until` de 30 días con refirmado
  semanal).*
- **FR-005**: El árbol del repositorio MUST poder servirse con cualquier servidor de archivos
  estáticos, sin lógica en el servidor.

#### Publicación

- **FR-006**: El mantenedor MUST poder publicar uno o varios paquetes, o todos, con una sola
  orden. La orden toma los `.deb` del directorio de salida de la fase 001.
- **FR-007**: Antes de modificar nada, la publicación MUST verificar, para cada paquete, cada
  arquitectura soportada y cada suite no excluida:
  - que existe el `.deb`;
  - que su informe de build declara lintian aprobado;
  - que su informe de prueba de instalación declara aprobado el resultado global;
  - que el hash del `.deb` coincide con el de ambos informes.
  Si falla cualquiera de estas comprobaciones, la publicación MUST abortarse por completo.
- **FR-008**: La publicación MUST ser atómica en el árbol local. El estado servido cambia de
  golpe del anterior al nuevo completo, o no cambia. Ningún fallo (validación, firma, disco o
  interrupción) MUST dejar índices sin firmar, mezclados o que apunten a archivos inexistentes.
- **FR-009**: Una versión ya publicada MUST NOT reescribirse. Publicar otra vez el mismo `.deb`,
  idéntico byte a byte, MUST ser una operación sin cambios que termina con éxito. Publicar un
  `.deb` distinto con el mismo nombre, versión y arquitectura MUST fallar.
- **FR-010**: La publicación MUST informar qué cambia por paquete, suite y arquitectura: qué
  versiones se agregan, cuáles se eliminan por retención y cuáles se mantienen.
- **FR-011**: Las órdenes de publicar, retirar y sincronizar MUST ofrecer un modo de simulación
  que muestre los cambios sin aplicarlos.
- **FR-012**: Solo MUST poder ejecutarse una operación de escritura a la vez sobre el mismo
  repositorio. Una segunda operación concurrente MUST fallar de inmediato con un mensaje claro.

#### Retención y retiro

- **FR-013**: El repositorio MUST conservar las últimas `K` versiones de cada paquete por suite
  (todas las arquitecturas de una versión entran y salen juntas), ordenadas según
  `dpkg --compare-versions`. `K` se configura con valor por
  defecto 3 y mínimo 1. Los archivos de paquete que ninguna suite referencia MUST eliminarse.
- **FR-014**: El mantenedor MUST poder retirar una versión concreta de un paquete con una orden.
  Retirar MUST eliminarla de todas las suites en una sola operación atómica (mismas garantías
  que FR-008).
- **FR-015**: Retirar la única versión publicada de un paquete MUST requerir confirmación
  explícita en la orden.
- **FR-016**: Las versiones retiradas MUST quedar registradas en el estado del repositorio
  (FR-035) con el motivo y la fecha. Volver a publicar una versión retirada MUST fallar
  (constitución VI).
- **FR-017**: Tras un retiro, la versión vigente de cada suite MUST ser la más alta de las que
  quedan. La documentación de usuario MUST explicar cómo hacer el downgrade explícito a ella.

#### Estado del repositorio

- **FR-035**: La fuente de verdad del repositorio MUST ser un archivo de estado versionado en
  git. Por cada versión publicada lista el paquete, la versión, la arquitectura, el sha256 del
  `.deb` y las suites en las que está. Además registra las versiones retiradas con su motivo y
  fecha. Publicar y retirar MUST actualizar este archivo en la misma operación atómica que el
  árbol; el diff del archivo se revisa en git, igual que los lockfiles.
- **FR-036**: El árbol del repositorio MUST poder regenerarse por completo a partir del archivo
  de estado y de los `.deb` correspondientes. Si un `.deb` no está en `dist/`, se obtiene
  reconstruyéndolo desde el manifiesto y el lockfile de esa versión, que estén en el historial
  de git. Su sha256 MUST coincidir con el del estado; si no coincide, la regeneración MUST
  fallar.
- **FR-037**: Antes de cada operación de escritura, el sistema MUST comprobar que el árbol local
  coincide con el archivo de estado. Si difieren, MUST abortar e indicar cómo regenerar el
  árbol.
- **FR-038**: Borrar versiones antiguas del lockfile de un paquete (orden de la fase 001) MUST
  fallar mientras alguna de esas versiones siga publicada.

#### Firma y llave

- **FR-018**: Los índices MUST firmarse con una subllave de firma de una llave GPG dedicada
  exclusivamente al repositorio. El fingerprint de la llave maestra se declara en la
  configuración; firmar con una subllave que no pertenezca a esa llave maestra MUST fallar.
- **FR-018a**: La parte secreta de la llave maestra MUST NOT estar en la PC de build; vive solo
  en el respaldo cifrado fuera de línea. La subllave de firma MUST tener caducidad (2 años) y
  frase de paso propia, distinta de la de la llave maestra. Esa frase de paso se guarda en un
  archivo con permisos `600`, fuera del repositorio git, para firmar sin interacción.
- **FR-019**: La llave y sus algoritmos MUST ser aceptados sin advertencias por el apt de las 5
  suites soportadas.
- **FR-020**: La llave privada y su frase de paso MUST NOT guardarse en el repositorio git ni
  aparecer en la salida, los informes ni los mensajes de error de ninguna orden (constitución
  III).
- **FR-021**: Si la firma falla por cualquier motivo, la publicación MUST abortarse con un
  mensaje que nombre la etapa (firma) y la causa, sin revelar material secreto. El repositorio
  servido MUST seguir siendo el anterior.
- **FR-022**: La llave pública MUST publicarse en el propio repositorio, en una ruta fija y
  documentada, como keyring listo para usarse con `Signed-By`. Debe incluir la llave maestra
  pública con sus subllaves vigentes. Durante una rotación de la llave maestra, el keyring MUST
  contener la llave vigente y la siguiente.
- **FR-023**: Cada publicación MUST avisar cuando la subllave de firma caduque en menos de 60
  días, indicando la fecha.
- **FR-024**: MUST existir un procedimiento documentado, en la guía de operación, para estas
  tareas:
  - generar la llave maestra y la subllave;
  - respaldarlas fuera de la máquina y quitar la maestra de la PC;
  - restaurarlas;
  - renovar la caducidad de la subllave o rotarla;
  - revocar una subllave comprometida;
  - rotar la llave maestra sin cortar el servicio a los clientes.

#### Sincronización

- **FR-025**: El mantenedor MUST poder sincronizar el repositorio con un destino con nombre,
  definido en la configuración. MUST haber al menos dos tipos de destino: un directorio local y
  un almacenamiento estático S3-compatible. El proveedor, el endpoint y el bucket se configuran
  y nunca se codifican (constitución IX).
- **FR-026**: La sincronización MUST seguir este orden:
  1. Subir los archivos de paquete nuevos.
  2. Subir los índices por arquitectura, incluidas sus copias direccionadas por hash.
  3. Subir los índices firmados de cada suite.
  4. Borrar los archivos que no referencian ni el estado nuevo ni el anterior ya sincronizado.
  Un cliente de una suite MUST ver siempre un estado completo, el anterior o el nuevo. Por eso
  los índices se piden por hash y lo del estado anterior se conserva una sincronización más.
- **FR-027**: Una sincronización interrumpida MUST poder repetirse hasta completarse sin pasos
  manuales. Tras completarse, el destino MUST ser idéntico al árbol local.
- **FR-028**: Las credenciales del destino público MUST leerse del entorno del mantenedor o de
  un archivo fuera del repositorio git, y MUST NOT aparecer en la salida.
- **FR-029**: Sincronizar MUST NOT subir nada que no esté publicado y firmado en el árbol local.

#### Documentación para el usuario final

- **FR-030**: MUST existir una guía de usuario en el repositorio git con:
  - la URL del repositorio y el fingerprint completo de la llave;
  - los comandos para descargar la llave a `/etc/apt/keyrings/` y verificar su fingerprint;
  - el archivo de fuente en formato deb822 (`.sources`) con `Signed-By`, para cada suite;
  - los comandos `apt update` y `apt install`;
  - cómo hacer el downgrade explícito a una versión anterior;
  - cómo quitar el repositorio (fuente y llave) y, opcionalmente, los paquetes;
  - qué paquetes no están disponibles en qué suites y por qué.
- **FR-031**: La guía MUST permitir agregar el repositorio con 4 comandos como máximo (sin
  contar `apt install`), copiables tal cual salvo `<suite>`, cuyo valor da la tabla de suites de
  la guía.
- **FR-032**: Las pruebas de aceptación de esta fase MUST ejecutar literalmente los comandos de
  la guía, extraídos del propio documento, para que la guía no pueda quedar desactualizada sin
  que falle una prueba.

#### Errores y salida

- **FR-033**: Cada fallo MUST indicar el paquete, la versión, la arquitectura, la suite (si
  aplica), la etapa (validación, firma, retención, retiro, sincronización) y la causa, con el
  mismo formato de errores que la fase 001.
- **FR-034**: Todas las órdenes MUST ser no interactivas, con códigos de salida estables y una
  salida estructurada opcional, como en la fase 001, para preparar la automatización futura
  (constitución VIII). Firmar tampoco pide nada: la frase de paso de la subllave se lee del
  archivo de FR-018a.

### Key Entities

- **Repositorio**: árbol de archivos estático con un pool de paquetes y, por suite, índices
  firmados. Tiene una URL pública y una llave de firma.
- **Suite**: una distribución destino (`bookworm`, …, `resolute`), con componente `main` y
  arquitecturas `amd64` y `arm64`. Contiene un conjunto de versiones de paquetes.
- **Versión publicada**: paquete, versión y arquitectura, con el hash del `.deb` y las suites en
  las que está. Su origen son los informes de build y de prueba de la fase 001.
- **Publicación**: operación que pasa del estado del repositorio N al N+1. Incluye agregar
  versiones, aplicar la retención y retirar versiones. Es atómica.
- **Estado del repositorio**: archivo versionado en git. Es la fuente de verdad: lista las
  versiones publicadas con su sha256 y sus suites, y las versiones retiradas con fecha y
  motivo. A partir de él se regenera el árbol y se impide republicar una versión retirada.
- **Llave de firma**: llave GPG dedicada con dos partes:
  - una llave maestra, cuyo fingerprint se publica y que vive solo en el respaldo fuera de
    línea;
  - una o más subllaves de firma, cada una con caducidad y estado (vigente, caducada o
    revocada).
  Solo la parte pública se publica.
- **Destino de sincronización**: nombre, tipo (local o S3-compatible) y parámetros. Las
  credenciales se guardan fuera del repositorio git.
- **Guía de usuario**: documento que el usuario final sigue para agregar, usar y quitar el
  repositorio. Es también la fuente de los comandos que ejecutan las pruebas.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: En contenedores limpios de las 5 suites × 2 arquitecturas (10 combinaciones),
  siguiendo solo la guía, `apt update` termina sin advertencias ni errores de firma, y
  `apt install` instala la versión publicada de cada paquete disponible en esa suite: `fnm`,
  `bob`, `buf` y `aqua`, salvo `bob` en `bookworm`. Pasa en 10 de 10.
- **SC-002**: Un usuario nuevo agrega el repositorio con 4 comandos como máximo, copiados de la
  guía, en menos de 2 minutos sin contar descargas.
- **SC-003**: En un contenedor de cada suite, se recorre el ciclo publicar → `apt upgrade` →
  retirar → downgrade explícito y la versión instalada es la esperada en cada paso. Pasa en las
  5 suites.
- **SC-004**: Hay 4 fallos provocados: falta un `.deb` de una arquitectura, el `.deb` no tiene
  prueba aprobada, la firma falla y la sincronización se interrumpe. En los 4, el repositorio
  local y el remoto siguen sirviendo el estado anterior completo y un cliente hace
  `apt update` sin errores.
- **SC-005**: Una búsqueda de material de llave secreta (bloques `PRIVATE KEY` y paquetes
  OpenPGP secretos) y del contenido de la frase de paso da 0 resultados en tres sitios: el
  repositorio git (incluido su historial), los logs y la salida de todas las órdenes en las
  pruebas. Los fingerprints son públicos y no cuentan.
- **SC-006**: El procedimiento de respaldo, restauración, rotación de subllave y revocación se
  ejecuta de principio a fin en un entorno desechable. El fingerprint publicado no cambia en
  ningún paso, y un cliente que refresca el keyring como indica la guía no ve errores.
- **SC-007**: Publicar una versión nueva de un paquete ya construido y probado, incluida la
  sincronización al destino público, toma una sola orden de publicación y una de sincronización,
  y menos de 5 minutos.
- **SC-008**: Con la retención por defecto y 5 versiones publicadas sucesivamente de un paquete,
  el repositorio ofrece exactamente 3. El árbol servido no guarda archivos de las 2 más
  antiguas, y los destinos tampoco tras dos sincronizaciones. El almacén local de `.deb`
  publicados queda aparte, porque lo necesita la reconstrucción.
- **SC-009**: Partiendo de un clon limpio del repositorio git, sin `dist/` ni caché, y de la
  subllave restaurada desde el respaldo, el árbol regenerado contiene los mismos `.deb`, con
  el mismo sha256, y los mismos índices de paquetes que el publicado. Solo cambian las firmas.

## Assumptions

- Los `.deb` de entrada son los que produce la fase 001 en `dist/`, junto a sus informes de build
  y de prueba. Esta fase no construye ni prueba paquetes; solo exige que esos informes existan y
  digan "aprobado".
- La subllave de firma vive en un llavero GPG dedicado en la PC, fuera del repositorio git, con
  su frase de paso en un archivo `600` junto a él. Firmar no requiere interacción. Ese
  archivo protege tanto como no tener frase de paso; se acepta porque la llave maestra está
  fuera de línea y una subllave comprometida se revoca y se reemplaza sin cambiar el
  fingerprint publicado. La llave maestra solo sale del respaldo fuera de línea para crear,
  renovar o revocar subllaves.
- La URL pública definitiva (dominio) se fija en la configuración. Hasta tenerla, las pruebas
  usan el destino local servido por HTTP y un destino S3-compatible de pruebas.
- **Riesgo aceptado**: los índices no caducan en esta fase (FR-004a). Así no hay que volver a
  firmar periódicamente, a cambio de no proteger contra un atacante que sirva índices antiguos
  pero válidos. Se revisa cuando exista automatización (constitución VIII).
- La retención y el retiro se aplican a nivel de versión completa: todas las arquitecturas de esa
  versión entran y salen juntas.
- Si se pierde la PC, el repositorio se reconstruye desde git: el archivo de estado, más los
  manifiestos y lockfiles de su historial, más builds reproducibles. Los `.deb` y el árbol
  generado no se versionan en git. Basta con restaurar la subllave de firma desde el respaldo.
- Solo se publican paquetes de nivel 1. El sufijo de suite del nivel 2 queda fuera de alcance,
  aunque el diseño no debe impedirlo.
- Fuera de alcance: CI, detección automática de versiones, sitio web, notificaciones
  automáticas, espejos múltiples y firmas por paquete.
