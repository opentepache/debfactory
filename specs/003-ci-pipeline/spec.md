# Feature Specification: Pipeline de construcción y publicación

**Feature Branch**: `003-ci-pipeline`

**Created**: 2026-11-13

**Status**: Draft

**Input**: User description: "Quiero que construir, probar y publicar ocurra automáticamente en
un pipeline, sin ejecutar comandos a mano. (…) Los builds corren en mi propia PC (runner
self-hosted), aislados del sistema principal, y nunca exponen la llave de firma a código de pull
requests. (…) Dos publicaciones simultáneas no corrompen el repositorio (se serializan)."

## Clarifications

### Session 2026-11-13

- Q: Cuando alguien abre un pull request desde un fork, ¿cuándo se ejecuta su código en la PC?
  → A: solo después de que el mantenedor apruebe cada ejecución. Hasta entonces solo corren
  validaciones que no ejecutan código del PR. Aun aprobado, corre aislado y sin secretos.
- Q: Ahora que hay pipeline, ¿los índices firmados deben caducar (`Valid-Until`)? → A: sí, a los
  30 días. Una ejecución programada semanal vuelve a firmarlos, sincroniza y verifica, y se avisa
  al mantenedor si a la generación servida le quedan menos de 7 días. Sustituye a FR-004a de la
  fase 002, que dejaba el riesgo aceptado hasta que hubiera automatización.
- Q: ¿Por qué canal se notifican los fallos? → A: un issue en la forja por paquete, versión y
  etapa, que se actualiza si el fallo se repite y se cierra solo al resolverse. La forja avisa al
  mantenedor por correo. Sin servicios ni credenciales nuevos.
- Q: ¿Cómo llega a la rama principal el estado y el registro de auditoría tras publicar? → A: el
  pipeline abre (o actualiza) un pull request con esos archivos y el mantenedor lo integra a
  mano. La publicación en apt no espera ese merge; mientras está pendiente, las siguientes
  ejecuciones parten del estado del PR, no del de la rama principal.

## User Scenarios & Testing *(mandatory)*

Hay cuatro actores:

- **El mantenedor**: el único con permiso de merge en la rama principal. Edita manifiestos y
  lockfiles, revisa pull requests y recibe las notificaciones de fallo.
- **Un contribuyente externo**: propone cambios con un pull request desde un fork. No es de
  confianza.
- **El pipeline**: ejecuta los gates 1–7 de la constitución en la PC del mantenedor, en un
  entorno aislado. Es el único que firma y publica.
- **El usuario final**: instala y actualiza con apt desde el repositorio público. Para él, lo
  único que cambia es que las versiones nuevas llegan sin que el mantenedor ejecute nada.

Esta fase cubre la parte "construir → probar → publicar" del principio VIII. La detección
automática de versiones nuevas queda fuera: el disparador es un cambio en un manifiesto o un
lockfile, hecho por una persona (o, más adelante, por una automatización que abra un pull
request).

Los paquetes de referencia son los cuatro publicados: `fnm`, `bob`, `buf` y `aqua`. `bob`
excluye `bookworm`.

### User Story 1 - Un merge publica solo (Priority: P1)

El mantenedor integra en la rama principal un cambio que sube la versión de un paquete (manifiesto
y lockfile). Sin que nadie ejecute nada más, el pipeline:

1. Detecta qué paquetes cambiaron.
2. Construye solo esos paquetes para `amd64` y `arm64`, con lintian.
3. Los prueba en cada suite no excluida y cada arquitectura.
4. Si todo pasa, los publica en el repositorio firmado.
5. Sincroniza el repositorio con el hosting público y verifica lo que sirve.
6. Abre o actualiza el pull request de estado con el estado nuevo y la entrada de auditoría,
   para que el mantenedor lo integre.

**Why this priority**: es el objetivo de la fase. Sin esto, cada versión sigue costando al
mantenedor una sesión de órdenes manuales, y el principio VIII no se cumple.

**Independent Test**: subir `fnm` a una versión nueva (o subir su `revision`) en un commit
integrado en la rama principal. Sin intervención, la versión nueva aparece en el repositorio
público y `apt-get install fnm` la instala en contenedores limpios de las 5 suites, en ambas
arquitecturas. Los otros tres paquetes no se reconstruyen ni cambian.

**Acceptance Scenarios**:

1. **Given** `fnm` publicado en `1.39.0-0~tepache1`, **When** se integra en la rama principal un
   commit que lo sube a una versión upstream nueva con su lockfile, **Then** al terminar el
   pipeline la versión nueva es la candidata de apt en las 5 suites y en ambas arquitecturas, y
   la anterior sigue disponible para `--allow-downgrades`.
2. **Given** el mismo cambio, **When** el pipeline termina, **Then** solo se construyó y probó
   `fnm`; `bob`, `buf` y `aqua` conservan los mismos `.deb` byte a byte.
3. **Given** un paquete nuevo con manifiesto y lockfile válidos, **When** se integra, **Then** se
   publica en todas sus suites no excluidas con `revision` 1.
4. **Given** que la prueba de instalación de un paquete falla en una sola suite, **When** el
   pipeline llega al gate 6, **Then** ese paquete no se publica en ninguna suite, el
   repositorio público no cambia para él y el mantenedor recibe una notificación.
5. **Given** que la publicación terminó, **When** el pipeline abre el pull request de estado,
   **Then** la versión ya está disponible vía apt sin esperar a ese merge.
6. **Given** el pull request de estado abierto, **When** el mantenedor lo integra, **Then** ese
   merge no dispara una publicación nueva.

---

### User Story 2 - Los pull requests se prueban, nunca se publican (Priority: P1)

Un pull request que toca manifiestos, lockfiles, configuración o código pasa por los mismos gates
que un cambio en la rama principal hasta el 6 (prueba de instalación). El resultado aparece en el
propio pull request, por paquete, suite y arquitectura. Nunca se firma, publica, sincroniza ni
modifica el estado.

**Why this priority**: es lo que permite integrar con confianza. Un merge con el pipeline del PR en
verde no debería fallar después en la rama principal.

**Independent Test**: abrir un PR que cambia el sha256 de un artefacto de `fnm` en su lockfile. El
pipeline falla en la etapa de verificación, nombra el artefacto y los dos hashes, y ni el
estado ni el repositorio local ni el público cambian.

**Acceptance Scenarios**:

1. **Given** un PR con un checksum incorrecto en un lockfile, **When** corre su pipeline,
   **Then** falla antes de construir, el PR queda marcado como fallido y no se publica nada.
2. **Given** un PR que sube la versión de un paquete correctamente, **When** corre su pipeline,
   **Then** construye y prueba ese paquete en todas sus suites y arquitecturas, y termina en
   verde sin publicar.
3. **Given** un PR que cambia código o configuración compartida y altera el `.deb` de una versión
   ya publicada sin subir su `revision`, **When** corre su pipeline, **Then** falla nombrando
   los paquetes afectados, porque una versión publicada no se puede reescribir.
4. **Given** un PR que cambia solo documentación, **When** corre su pipeline, **Then** no
   construye ni prueba paquetes.

---

### User Story 3 - Los secretos nunca llegan al código no confiable (Priority: P1)

La llave de firma, su frase de paso y las credenciales del hosting público solo están al alcance
de las ejecuciones que publican, y esas solo corren código que ya está en la rama principal. Las
ejecuciones de pull requests, sobre todo las de forks, corren aisladas del sistema principal de la
PC y sin ningún secreto.

**Why this priority**: el runner corre en la PC del mantenedor, donde viven la subllave y las
credenciales. Un fallo aquí compromete la firma del repositorio para todos los usuarios.

**Independent Test**: abrir desde un fork un PR que intenta leer variables de entorno, rutas
conocidas de la llave y las credenciales, y escribir fuera de su espacio de trabajo. Ninguna de
esas lecturas devuelve un secreto, ninguna escritura llega al sistema principal, y nada de eso
aparece en los logs.

**Acceptance Scenarios**:

1. **Given** un PR desde un fork, **When** su código se ejecuta, **Then** no tiene acceso a la
   llave, a la frase de paso, a las credenciales de publicación ni a los datos del repositorio
   APT.
2. **Given** un PR desde un fork recién abierto o con un commit nuevo, **When** el mantenedor
   todavía no lo aprobó, **Then** solo corren las validaciones que no ejecutan código del PR, y
   la construcción y las pruebas esperan la aprobación.
3. **Given** cualquier ejecución, **When** termina, **Then** su entorno de trabajo se descarta y
   la siguiente ejecución empieza limpia.

---

### User Story 4 - Notificación de fallos (Priority: P2)

Cuando una ejecución de la rama principal o una lanzada a mano falla, el mantenedor recibe una
notificación con el paquete, la versión, la etapa que falló, el motivo exacto y un enlace al log.

**Why this priority**: sin esto, un fallo pasa desapercibido y el usuario final se queda sin la
versión nueva sin que nadie lo sepa (principio VIII).

**Independent Test**: provocar un fallo de prueba de instalación en la rama principal. Llega una
notificación con los cinco datos. Repetir el fallo no crea otra notificación, y la siguiente
ejecución exitosa la cierra.

**Acceptance Scenarios**:

1. **Given** un fallo en cualquier etapa (validación, descarga, licencias, construcción, lintian,
   prueba, firma, sincronización o registro del estado), **When** ocurre en la rama principal o
   en una ejecución manual, **Then** el mantenedor recibe una notificación con paquete, versión,
   etapa, motivo y enlace al log.
2. **Given** una notificación abierta por un fallo, **When** el mismo paquete, versión y etapa
   vuelven a fallar, **Then** se actualiza la notificación existente en lugar de crear otra.
3. **Given** una notificación abierta, **When** una ejecución posterior publica esa versión,
   **Then** la notificación se cierra sola, con referencia a la ejecución que lo resolvió.
4. **Given** un fallo que no es de un paquete (falta espacio en disco, el runner no arranca su
   entorno), **When** ocurre, **Then** la notificación lo indica como fallo del pipeline, con la
   etapa y el enlace al log.

---

### User Story 5 - Registro auditable de cada publicación (Priority: P2)

Cada publicación y cada retiro dejan un registro permanente: qué paquete y versión, desde qué
commit, con qué sha256 de cada `.deb`, en qué generación del repositorio, cuándo y en qué
ejecución.

**Why this priority**: permite responder "¿de dónde salió este `.deb`?" y comprobar que lo que
sirve el repositorio público es lo que construyó el pipeline a partir de un commit concreto.

**Independent Test**: tomar cualquier `.deb` del repositorio público, buscar su sha256 en el
registro, ir al commit indicado y reconstruirlo. El resultado es idéntico byte a byte.

**Acceptance Scenarios**:

1. **Given** una publicación hecha por el pipeline, **When** se consulta el registro, **Then**
   aparece con paquete, versión, commit de origen, sha256 por arquitectura, generación, fecha y
   enlace a la ejecución.
2. **Given** el registro, **When** se compara con el repositorio público, **Then** cada `.deb`
   servido tiene una entrada con su mismo sha256.
3. **Given** entradas existentes, **When** hay publicaciones nuevas, **Then** solo se añaden
   entradas; las anteriores nunca se modifican ni se borran, ni siquiera al retirar una versión
   (el retiro es otra entrada).

---

### User Story 6 - Ejecuciones manuales: reconstruir y retirar (Priority: P3)

El mantenedor puede lanzar a mano, desde la forja y sin abrir una terminal:

- la reconstrucción de un paquete y una versión concretos (construir, probar y, si corresponde,
  publicar);
- el retiro de una versión concreta, con su motivo.

**Why this priority**: cubre lo que no dispara un commit: reintentar después de un fallo
transitorio (red, upstream caído), revalidar una versión publicada y retirar una versión
defectuosa sin salir del pipeline, que es el único que escribe en el repositorio.

**Independent Test**: lanzar la reconstrucción de `fnm 1.39.0-0~tepache1`, ya publicada. El
pipeline construye y prueba, comprueba que los `.deb` coinciden con los publicados, no publica
nada y termina en verde.

**Acceptance Scenarios**:

1. **Given** una versión ya publicada, **When** se lanza su reconstrucción, **Then** construye y
   prueba; si los `.deb` son idénticos a los publicados termina en verde sin cambios, y si
   difieren falla sin publicar.
2. **Given** una versión que falló en la rama principal por una causa transitoria, **When** se
   lanza su reconstrucción, **Then** recorre los gates y la publica si pasan.
3. **Given** una versión publicada, **When** el mantenedor lanza su retiro con un motivo,
   **Then** el pipeline la retira, sincroniza, verifica y deja el retiro en el registro.
4. **Given** alguien sin permiso de mantenedor, **When** intenta lanzar una ejecución manual,
   **Then** la forja no se lo permite.

---

### Edge Cases

- **Dos merges seguidos**: las publicaciones se encolan en orden de llegada y corren una a la vez.
  La segunda parte del estado que dejó la primera. No se pierde ni se duplica ninguna.
- **Un merge cambia dos paquetes y uno falla**: el que pasa se publica; el que falla no, y se
  notifica. Los paquetes son independientes entre sí, y cada uno es todo o nada en sus suites
  (principio VII).
- **Falla la sincronización con el hosting público después de publicar en local**: el estado en
  el árbol local ya avanzó, pero el público sigue sirviendo la generación anterior completa. Se
  notifica, y la siguiente ejecución (o un reintento manual) completa la sincronización sin
  volver a construir.
- **Varias publicaciones antes de que el mantenedor integre el PR de estado**: cada una parte del
  estado del PR pendiente y lo actualiza. Nunca hay dos PR de estado abiertos a la vez.
- **El PR de estado se queda sin integrar más de 7 días**: se notifica (FR-020) como fallo de la
  etapa "estado", con enlace al PR.
- **El mantenedor cierra el PR de estado sin integrarlo, o alguien lo edita**: el pipeline no
  publica nada más y notifica hasta que el PR de estado vuelva a coincidir con lo publicado. El
  estado publicado nunca se descarta en silencio.
- **Se pierde la PC con el PR de estado pendiente**: la rama del PR vive en la forja, así que la
  reconstrucción desde git (fase 002) parte de ella y no pierde publicaciones.
- **Alguien integra en la rama principal un cambio que también toca el estado**: el PR de estado
  entra en conflicto; el pipeline lo rehace sobre la rama actualizada o, si no puede, notifica.
- **Se interrumpe una ejecución a mitad** (corte de luz, PC apagada, runner reiniciado): la
  siguiente ejecución deja el repositorio coherente y `repo verify` pasa. Si la PC está apagada,
  las ejecuciones esperan en cola hasta que vuelva.
- **Un merge sube la versión en el manifiesto pero no actualiza el lockfile**: falla en la
  validación con un mensaje que pide regenerar el lockfile. No se publica nada.
- **Un merge cambia solo código o configuración compartida**: se ejecutan todas las
  comprobaciones, pero no se publica nada, porque ninguna versión cambió.
- **Un PR desde un fork modifica la definición del pipeline** para pedir secretos o publicar: esa
  definición no recibe secretos ni permisos de publicación, sea cual sea su contenido.
- **El upstream retira el artefacto entre el PR y el merge**: la descarga falla en la rama
  principal, se notifica y el repositorio no cambia. El reintento manual sirve cuando el upstream
  vuelva a servirlo con el mismo hash.
- **Reconstrucción manual de una versión que ya no está en el manifiesto**: se usa el manifiesto
  y el lockfile del commit de la rama principal que declaró esa versión. Si ningún commit la
  declaró, la ejecución falla sin hacer nada.
- **La PC está apagada más de una semana**: el refirmado semanal no corre. A los 23 días sin
  refirmar llega el aviso de FR-032; a los 30, los clientes empiezan a rechazar los índices en
  `apt update` hasta que una ejecución vuelva a firmar. Ese es el precio aceptado de cerrar los
  *freeze attacks*.
- **Retiro manual de la única versión publicada de un paquete**: exige una confirmación explícita
  en los parámetros de la ejecución (FR-015 de la fase 002).

## Requirements *(mandatory)*

### Functional Requirements

**Disparadores y alcance**

- **FR-001**: Cada cambio integrado en la rama principal MUST disparar el pipeline sin
  intervención humana.
- **FR-002**: El pipeline MUST determinar los paquetes afectados como aquellos cuya versión del
  manifiesto aún no está publicada: versión nueva, `revision` nueva o paquete nuevo. Solo esos paquetes MUST construirse, probarse y publicarse.
- **FR-003**: Para cada paquete afectado, el pipeline MUST recorrer los gates 1–7 de la
  constitución, con la prueba de instalación en cada suite no excluida y cada arquitectura
  soportada, y MUST publicar solo si todos pasan.
- **FR-004**: Los paquetes afectados en un mismo cambio MUST tratarse de forma independiente: el
  fallo de uno no impide publicar los demás.
- **FR-005**: Un cambio que no afecta a ningún manifiesto ni lockfile MUST ejecutar las
  comprobaciones que correspondan a lo que cambió (código, configuración), pero MUST NOT
  publicar.
- **FR-006**: La publicación MUST incluir la sincronización con el hosting público y su
  verificación remota. Una ejecución solo se da por exitosa cuando el hosting público sirve la
  generación nueva y la verificación pasa.
- **FR-007**: Tras publicar o retirar, el pipeline MUST abrir un pull request de
  estado con el estado del repositorio y las entradas de auditoría nuevas, o actualizar el que ya
  esté abierto. Ese PR solo MUST tocar esos archivos y lo integra el mantenedor. La
  disponibilidad vía apt MUST NOT esperar a ese merge, y el merge MUST NOT disparar otra
  publicación.
- **FR-007a**: Mientras haya un PR de estado pendiente, toda ejecución que escribe MUST partir
  del estado de ese PR y actualizarlo. MUST NOT existir más de un PR de estado abierto.
- **FR-007b**: El pipeline MUST verificar en el PR de estado que su contenido coincide con lo que
  sirve el repositorio. Si no coincide (PR cerrado sin integrar, editado a mano o en conflicto
  irresoluble), MUST NOT publicar nada más y MUST notificar hasta que vuelva a coincidir.
- **FR-007c**: Si el PR de estado lleva más de 7 días sin integrarse, el pipeline MUST notificar
  al mantenedor con enlace al PR.

**Pull requests**

- **FR-008**: Cada pull request MUST ejecutar los gates 1–6 para los paquetes afectados y las
  comprobaciones del código que cambie, y MUST mostrar el resultado en el pull request por
  paquete, suite y arquitectura.
- **FR-009**: Una ejecución de pull request MUST NOT firmar, publicar, sincronizar ni modificar
  el estado del repositorio, el árbol local ni la rama principal.
- **FR-010**: Si un pull request cambia código o configuración compartida, el pipeline MUST
  construir todos los paquetes publicados y compararlos con lo publicado. Si cambia el `.deb` de
  una versión ya publicada y el PR no sube su `revision`, MUST fallar nombrando los paquetes.
- **FR-011**: Un pull request que solo cambia documentación MUST NOT construir ni probar
  paquetes.

**Aislamiento y secretos**

- **FR-012**: Todas las ejecuciones MUST correr en la PC del mantenedor, en un entorno aislado del
  sistema principal. El entorno MUST NOT tener acceso al directorio personal del mantenedor, a su
  llavero, a sus credenciales ni a los datos del repositorio APT, salvo lo que FR-014 concede a
  las ejecuciones que publican. Cada ejecución MUST empezar con un entorno limpio y descartarlo al
  terminar.
- **FR-013**: El código de un pull request desde un fork MUST NOT ejecutarse en la PC hasta que
  el mantenedor apruebe explícitamente esa ejecución en ese PR. Cada commit nuevo del PR MUST
  requerir una aprobación nueva. Antes de la aprobación solo MAY correr validaciones que no
  ejecutan código del PR (esquema de los manifiestos, coherencia del lockfile). Aprobar una
  ejecución MUST NOT darle acceso a secretos (FR-014).
- **FR-014**: La subllave de firma, su frase de paso y las credenciales de publicación MUST
  estar disponibles solo para las ejecuciones que publican o retiran, y esas MUST ejecutar código
  que ya está en la rama principal: el disparo por merge y las ejecuciones manuales del
  mantenedor. Nunca MUST estar al alcance de una ejecución que corre código de un pull request,
  venga de un fork o de una rama del propio repositorio.
- **FR-015**: Los secretos MUST quedarse en la PC del mantenedor. La forja MUST NOT almacenarlos.
- **FR-016**: Los logs de todas las ejecuciones MUST NOT contener secretos, aunque el código
  ejecutado intente imprimirlos.
- **FR-017**: Las ejecuciones de pull requests MUST tener permisos mínimos en la forja: leer el
  código e informar su resultado, sin escribir en ramas, releases ni en la configuración del
  repositorio.
- **FR-017a**: Las ejecuciones que escriben MAY empujar solo la rama del PR de estado, abrir y
  actualizar ese PR y gestionar los issues de fallo. MUST NOT poder escribir en la rama
  principal: solo el mantenedor integra en ella.

**Serialización**

- **FR-018**: Las ejecuciones que escriben (publicar, retirar, sincronizar, registrar el estado)
  MUST serializarse: como máximo una a la vez, en orden de llegada, sin descartar ninguna de la
  cola. Cada una MUST partir del estado que dejó la anterior.
- **FR-019**: Las ejecuciones que no escriben MAY correr en paralelo con otras, dentro de la
  capacidad de la PC.

**Notificaciones**

- **FR-020**: Todo fallo en una ejecución de la rama principal, manual o programada MUST
  notificar al mantenedor abriendo un issue en la forja con: paquete (o "pipeline" si el fallo
  no es de un paquete), versión, etapa, motivo exacto y enlace al log. El aviso al mantenedor
  llega por el correo de la forja. MUST NOT requerir servicios ni credenciales adicionales.
- **FR-021**: Los issues de fallo MUST deduplicarse por paquete, versión y etapa: un fallo
  repetido comenta el issue abierto en lugar de abrir otro. Una ejecución posterior que publica esa versión
  MUST cerrarla.
- **FR-022**: Los fallos de pull requests MUST verse en el propio pull request, con los mismos
  datos, y MUST NOT generar notificaciones aparte.

**Auditoría**

- **FR-023**: Cada publicación y cada retiro MUST añadir una entrada al registro de auditoría con:
  paquete, versión, operación, commit de origen, sha256 de cada `.deb` por arquitectura,
  generación del repositorio, fecha y enlace a la ejecución. El retiro MUST incluir el motivo.
- **FR-024**: El registro MUST versionarse en git (en el PR de estado mientras está pendiente y
  en la rama principal al integrarse) y ser de solo añadir: ninguna entrada se modifica ni se
  borra.
- **FR-025**: Todo `.deb` servido por el repositorio público MUST tener una entrada con su mismo
  sha256 en el registro de la rama principal o en el del PR de estado pendiente.

**Ejecuciones manuales**

- **FR-026**: El mantenedor MUST poder lanzar desde la forja la reconstrucción de un paquete y
  una versión concretos. La ejecución usa el manifiesto y el lockfile del commit de la rama
  principal que declaró esa versión, y recorre los gates 1–7. Si la versión ya está publicada,
  MUST comparar los `.deb` con lo publicado: si coinciden termina sin cambios, y si no, falla sin
  publicar.
- **FR-027**: El mantenedor MUST poder lanzar desde la forja el retiro de una versión con su
  motivo. El retiro MUST sincronizarse, verificarse y registrarse igual que una publicación.
- **FR-028**: Solo el mantenedor MUST poder lanzar ejecuciones manuales.

**Caducidad y refirmado**

- **FR-030**: Los índices de suite MUST declarar una caducidad (`Valid-Until`) de 30 días desde
  su firma. Esto sustituye a FR-004a de la fase 002.
- **FR-031**: Una ejecución programada MUST volver a firmar los índices al menos una vez por
  semana aunque no haya cambios de paquetes, y MUST sincronizar y verificar como una
  publicación. No cambia ningún `.deb`, no añade entradas de auditoría de paquetes y MUST NOT
  requerir un PR de estado: el estado versionado en git no cambia por refirmar. Se serializa
  con las demás escrituras (FR-018).
- **FR-032**: Si a la generación que sirve el hosting público le quedan menos de 7 días de
  validez, el pipeline MUST notificar al mantenedor (FR-020) con la fecha de caducidad.

**Recuperación**

- **FR-029**: Repetir una ejecución que falló o se interrumpió MUST llevar el repositorio a un
  estado coherente sin publicar dos veces lo mismo ni perder una publicación. Si la publicación
  local ya ocurrió, el reintento MUST limitarse a completar lo que faltó (sincronizar, verificar,
  registrar).

### Key Entities

- **Ejecución**: una corrida del pipeline. Tiene disparador (merge, pull request, manual), commit,
  paquetes afectados, resultado por paquete, etapa, suite y arquitectura, y enlace al log.
- **Paquete afectado**: un paquete cuya versión del manifiesto aún no está publicada, con la
  versión completa que se va a construir.
- **Entrada de auditoría**: el registro permanente de una publicación o un retiro (FR-023).
  Complementa el estado del repositorio de la fase 002, que dice qué está publicado hoy; el
  registro dice cómo se llegó ahí.
- **PR de estado**: el único pull request que abre el pipeline. Lleva el estado del repositorio
  y las entradas de auditoría de las publicaciones y retiros aún no integrados. Mientras está
  abierto, es la referencia de la que parten las ejecuciones que escriben.
- **Notificación de fallo**: un issue de la forja identificado por paquete, versión y etapa. Está
  abierto mientras el fallo persiste y se cierra con la ejecución que lo resuelve.
- **Secretos de publicación**: la subllave de firma, su frase de paso y las credenciales del
  hosting público. Viven en la PC y solo los ven las ejecuciones que publican.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Desde el merge de una versión nueva de `fnm` hasta que `apt-get install fnm` la
  instala desde el repositorio público en las 5 suites y ambas arquitecturas pasan menos de
  30 minutos, con cero órdenes manuales del mantenedor.
- **SC-002**: El 100 % de los pull requests con un checksum incorrecto fallan antes de construir,
  y la generación del repositorio local y la del público quedan iguales que antes.
- **SC-003**: Un PR desde un fork diseñado para buscar secretos (variables de entorno, rutas de la
  llave y de las credenciales, escritura fuera de su espacio) no obtiene ningún secreto, no
  escribe nada en el sistema principal y ningún secreto aparece en sus logs.
- **SC-004**: Dos merges con versiones nuevas de dos paquetes, con menos de 1 minuto de
  diferencia, terminan con ambas versiones publicadas, dos entradas de auditoría y `repo verify`
  en verde, sin intervención.
- **SC-005**: Cada uno de estos fallos provocados genera una notificación con los cinco datos en
  menos de 5 minutos: checksum, lintian, prueba de instalación, firma y sincronización. Repetir
  el fallo no genera una segunda notificación.
- **SC-006**: El 100 % de los `.deb` del repositorio público tienen una entrada de auditoría, y
  reconstruir uno desde el commit de su entrada da el mismo sha256.
- **SC-007**: Tras matar una ejecución a mitad de la publicación, la siguiente ejecución deja el
  repositorio coherente y `repo verify` pasa, sin otra acción del mantenedor que relanzarla.
- **SC-008**: Una reconstrucción manual o un retiro se lanzan con una sola acción en la forja,
  sin abrir una terminal.
- **SC-009**: Durante 5 semanas sin cambios de paquetes, la generación servida nunca tiene menos
  de 23 días de validez, y un cliente que hace `apt update` no recibe errores ni advertencias.

## Assumptions

- **Forja**: el código se aloja en una forja pública que ofrece pull requests desde forks,
  runners propios, ejecuciones manuales con parámetros y protección de la rama principal. Se
  eligió GitHub en `/speckit-plan` (research D1); los requisitos no dependen de ella.
- **Lockfiles**: los genera una persona con `lock` y van en el mismo cambio que el manifiesto. El
  pipeline los verifica, pero nunca los escribe. Generarlos solo sería parte de la detección
  automática de versiones, que queda para otra fase.
- **Un único escritor**: a partir de esta fase, el pipeline es el único que publica, retira y
  sincroniza el repositorio público. Las órdenes locales siguen sirviendo al mantenedor para
  desarrollar y probar contra datos desechables o el destino de prueba.
- **Secretos en la PC**: la subllave, la frase de paso y las credenciales de R2 viven en la PC,
  donde ya están, al alcance solo del entorno que publica. Subirlos a la forja aumentaría la
  exposición y contradice III y X.
- **Un solo runner**: hay una sola PC. Si está apagada, las ejecuciones esperan en cola. Avisar
  de que el runner está caído queda fuera de alcance.
- **Merge con PR en verde**: el mantenedor integra por pull request con el pipeline en verde; la
  protección de la rama principal no admite push directo. Aun así, si un PR se integra sin el
  check en verde, la ejecución de la rama principal repite todos los gates y las comprobaciones
  de código antes de publicar, así que ningún camino publica sin pasar los gates.
- **Medición pendiente de la fase 002**: la primera publicación real del pipeline sirve también
  para medir SC-007 de la fase 002 (T062, paso 5).
- **Fuera de alcance**: detección automática de versiones, agentes de IA (cuando lleguen, sus PR
  pasan por este mismo pipeline como cualquier contribuyente), sitio web, publicación de
  paquetes de nivel 2.
