# Feature Specification: Detección de versiones upstream y actualización automática

**Feature Branch**: `005-upstream-release-watch`

**Created**: 2026-11-19

**Status**: Draft

**Input**: User description: "Quiero enterarme automáticamente de cuándo sale una versión nueva de
cada herramienta empaquetada y que el sistema prepare la actualización solo. (…) Revisar
periódicamente (por defecto cada 24 horas) los releases upstream; ignorar prereleases, betas,
nightlies y RCs salvo que el manifiesto indique lo contrario; abrir un pull request que actualiza
el manifiesto (versión y checksums) para que el pipeline lo valide; auto-merge opcional por
paquete; sin PRs duplicados; notificar cambios de licencia o de formato de los assets en lugar de
reintentar indefinidamente; respetar los límites de la API de GitHub."

## Clarifications

### Session 2026-11-19

- Q: La petición dice "ignorar prereleases… salvo que el manifiesto indique lo contrario", pero la
  constitución prohíbe empaquetar versiones preliminares: ¿qué puede indicar el manifiesto? → A:
  solo afinar cómo se reconoce una versión estable (patrón de etiquetas propio, o ignorar una
  marca de pre-release que upstream pone por error en releases estables). Nunca permite publicar
  una versión preliminar.
- Q: ¿Cuánto debe esperar el detector desde que upstream publica un release estable hasta
  proponerlo? → A: 48 horas para todos los paquetes antes de abrir siquiera el PR (valor global
  del repositorio de paquetes, no por paquete). Da margen a que upstream retire o corrija un
  release roto o comprometido antes de que llegue a un PR, con o sin auto-merge.
- Q: Con auto-merge activo, ¿un salto de versión mayor (p. ej. 4.x → 5.0) también se integra
  solo? → A: no. Un cambio de versión mayor siempre espera al mantenedor; las versiones menores y
  los parches sí se integran solos.

## User Scenarios & Testing *(mandatory)*

Hay dos actores:

- **El mantenedor**: el único con permiso de merge en la rama principal del repositorio de
  paquetes. Decide qué paquetes se integran solos (auto-merge) y recibe las notificaciones.
- **El detector**: el proceso automático que revisa los releases upstream y propone las
  actualizaciones. No es una IA: es determinista. No firma, no publica y no integra nada por sí
  mismo; solo abre o actualiza pull requests y notificaciones. La publicación sigue en manos del
  pipeline de la fase 003, con todas sus pruebas.

### User Story 1 - Una versión nueva estable llega como pull request (Priority: P1)

Upstream publica una versión estable nueva de un paquete empaquetado. En el primer ciclo de
revisión después de que el release cumpla 48 horas, sin que nadie lo pida, aparece un pull request que lleva el manifiesto a esa versión
(con su revisión de empaquetado reiniciada) y su lockfile con los hashes verificados de los
artefactos, la licencia y la fecha del release. El pipeline de PR lo construye, lo prueba y deja
el resultado en el propio PR.

**Why this priority**: es el corazón de la funcionalidad. Sin ella el mantenedor sigue vigilando
releases a mano, que es justo lo que el principio VIII ("detectar versión nueva → … → publicar sin
intervención humana") pide eliminar.

**Independent Test**: con un upstream simulado que publicó hace más de 48 horas una versión
estable nueva de `bob`, ejecutar un ciclo del detector y comprobar que existe un único PR para `bob` con la versión
nueva, la revisión `1`, el lockfile regenerado y el check del pipeline en verde.

**Acceptance Scenarios**:

1. **Given** `bob` empaquetado en la versión 4.3.0 y upstream publicó 4.4.0 estable hace más de
   48 horas, **When** corre el ciclo, **Then** existe un PR para `bob` que cambia la versión a 4.4.0, la
   revisión a 1 y el lockfile con los hashes de 4.4.0, y el pipeline de PR lo valida.
2. **Given** upstream publica 4.4.0 y 4.5.0 entre dos ciclos, **When** corre el ciclo, **Then** el
   PR propone directamente 4.5.0 (la más reciente), no una por una.
3. **Given** ninguna versión nueva para ningún paquete, **When** corre el ciclo, **Then** no se
   abre, modifica ni notifica nada.
4. **Given** upstream publicó 4.4.0 estable hace 10 horas, **When** corre el ciclo, **Then** no se
   abre PR todavía (ni con un ciclo manual); el resumen del ciclo indica cuándo será elegible.
5. **Given** upstream publicó 4.4.0 hace 60 horas y 4.4.1 hace 5 horas, **When** corre el ciclo,
   **Then** el PR propone 4.4.0; en el primer ciclo en que 4.4.1 cumpla 48 horas, el mismo PR (o
   uno nuevo si el anterior ya se integró) pasa a 4.4.1.
4. **Given** el PR de una versión nueva, **When** el mantenedor lo revisa, **Then** el PR muestra
   la versión anterior y la nueva, el enlace a las notas del release upstream y si la licencia
   cambió.

---

### User Story 2 - Las versiones preliminares nunca se proponen (Priority: P1)

Upstream publica una beta, una RC, una nightly o un release marcado como pre-release. El detector
lo ignora: no abre PR, no actualiza uno existente y no notifica.

**Why this priority**: la constitución solo permite empaquetar versiones estables y de producción.
Un PR con una beta, sobre todo con auto-merge activo, publicaría en apt algo que el repositorio
promete no publicar.

**Independent Test**: con un upstream simulado cuyo único release nuevo está marcado como
pre-release (y otro cuyo tag es `v4.4.0-rc1` sin la marca), ejecutar un ciclo y comprobar que no
hay PR ni notificación.

**Acceptance Scenarios**:

1. **Given** upstream publica `4.4.0` marcado como pre-release, **When** corre el ciclo, **Then**
   no se abre PR.
2. **Given** upstream publica `v4.4.0-rc1` (o `beta`, `alpha`, `nightly`, `preview`, `canary`,
   `dev`, `tip`) sin marcarlo como pre-release, **When** corre el ciclo, **Then** tampoco se abre
   PR.
3. **Given** un PR abierto para 4.4.0 y upstream publica 4.5.0-beta, **When** corre el ciclo,
   **Then** el PR sigue en 4.4.0.
4. **Given** un paquete cuyo manifiesto declara un patrón propio de etiquetas estables (p. ej.
   `release-1.2.3`) o que upstream marca por error como pre-release sus versiones estables,
   **When** upstream publica un release que cumple esa regla, **Then** el detector lo trata como
   estable; **And** si el release además lleva un marcador preliminar en su versión (p. ej.
   `release-1.3.0-rc1`), **Then** lo sigue descartando: ninguna regla del manifiesto permite
   proponer una versión preliminar.

---

### User Story 3 - El camino feliz se publica solo con auto-merge (Priority: P2)

Para los paquetes que el mantenedor marque con auto-merge, el PR de versión nueva se integra solo
cuando todas las pruebas del pipeline pasan, y la ejecución de la rama principal lo publica en
apt. El mantenedor no interviene.

**Why this priority**: cierra la cadena del principio VIII, pero depende de US1 y conviene
activarlo paquete por paquete una vez que el detector ha demostrado ser fiable.

**Independent Test**: con auto-merge activo para `bob` y un release nuevo simulado publicado hace
más de 48 horas, ejecutar un
ciclo y comprobar, sin acciones humanas, que el PR se integra tras su check en verde y que
`apt install bob` en un contenedor limpio instala la versión nueva.

**Acceptance Scenarios**:

1. **Given** `bob` con auto-merge activo y una versión nueva estable, **When** el check del PR
   pasa, **Then** el PR se integra solo y la versión termina disponible en apt en las 5 suites sin
   intervención humana.
2. **Given** `bob` con auto-merge activo, **When** el check del PR falla, **Then** el PR queda
   abierto sin integrar y el mantenedor recibe la notificación de fallo del pipeline.
3. **Given** un paquete sin auto-merge (el valor por defecto), **When** su PR pasa el check,
   **Then** el PR espera el merge del mantenedor.
4. **Given** auto-merge activo, **When** la versión nueva trae un cambio de licencia (aunque la
   nueva esté permitida), **Then** el PR no se integra solo: espera al mantenedor.
5. **Given** `bob` con auto-merge activo en 4.3.0, **When** la candidata es 5.0.0 y su check pasa,
   **Then** el PR no se integra solo y su descripción indica que es un cambio de versión mayor;
   **And** si la candidata es 4.4.0 o 4.3.1, se integra sola.

---

### User Story 4 - Licencias y formatos que cambian se notifican, no se reintentan (Priority: P2)

Si la versión nueva cambia a una licencia no permitida, o upstream cambia el nombre o el formato
de sus assets de modo que el manifiesto ya no los encuentra, el detector no abre un PR que
fallaría: notifica al mantenedor una sola vez con el paquete, la versión y el motivo exacto, y no
vuelve a intentarlo en cada ciclo mientras nada cambie.

**Why this priority**: sin esto, cada ciclo produciría un PR roto o un error repetido; el
mantenedor perdería la señal entre el ruido.

**Independent Test**: con un upstream simulado cuya versión nueva cambia la licencia a una fuera
de la allowlist, ejecutar dos ciclos y comprobar una única notificación, ningún PR y nada nuevo en
apt.

**Acceptance Scenarios**:

1. **Given** `bob` pasa de una licencia permitida a una no permitida en 4.4.0, **When** corre el
   ciclo, **Then** se abre una notificación con paquete, versión, licencia anterior y nueva, no se
   abre PR y no se publica nada.
2. **Given** upstream renombra sus assets en 4.4.0 y la plantilla del manifiesto ya no resuelve,
   **When** corre el ciclo, **Then** se notifica "formato de assets cambiado" con los nombres
   esperados y los encontrados, y no se abre PR.
3. **Given** esa notificación abierta, **When** corren más ciclos sin cambios, **Then** no se crea
   otra notificación ni se reintenta la preparación del PR.
4. **Given** esa notificación abierta, **When** el mantenedor corrige el manifiesto o upstream
   publica una versión que sí encaja, **Then** el detector vuelve a proponer la actualización y la
   notificación se cierra sola con referencia al PR.

---

### User Story 5 - Ciclos baratos y dentro de los límites de la API (Priority: P3)

Cada ciclo consulta upstream con el mínimo de peticiones, evita volver a descargar lo que no
cambió y, si se acerca al límite de peticiones de la forja, se detiene ordenadamente y continúa en
el ciclo siguiente sin perder ni duplicar trabajo.

**Why this priority**: con pocos paquetes el límite está lejos, pero el catálogo crecerá y un
detector que agota el límite también bloquea al pipeline, que usa la misma forja.

**Independent Test**: con un límite simulado casi agotado, ejecutar un ciclo y comprobar que se
detiene antes de agotarlo, lo registra y el ciclo siguiente revisa los paquetes que faltaron.

**Acceptance Scenarios**:

1. **Given** el límite de peticiones a punto de agotarse, **When** corre el ciclo, **Then** se
   detiene antes de agotarlo, registra qué paquetes quedaron sin revisar y no falla.
2. **Given** un ciclo sin novedades, **When** se repite, **Then** consume como máximo una consulta
   ligera por paquete.

---

### Edge Cases

- **Release retirado upstream durante la espera de 48 horas**: nunca llega a proponerse.
- **Release retirado o editado upstream** después de abrir el PR: el ciclo siguiente lleva el PR a
  la versión estable más reciente que siga existiendo; si ya no hay ninguna más nueva que la
  publicada, cierra el PR con un comentario.
- **Release publicado sin assets todavía** (upstream sube los binarios minutos después): la
  espera de 48 horas (FR-004a) lo absorbe; si tras la espera siguen faltando, es un cambio de
  formato y se notifica en ese mismo ciclo.
- **Versión upstream menor que la publicada** (p. ej. un parche de una rama antigua): se ignora.
- **Hashes de un release ya propuesto que cambian** (upstream resube un asset con el mismo tag):
  no se actualiza el PR en silencio; se notifica como posible manipulación y se deja de proponer
  esa versión hasta que el mantenedor decida.
- **El mantenedor editó a mano la rama del PR** del detector: el detector no sobrescribe sus
  commits; comenta en el PR que hay una versión más nueva disponible.
- **PR de versión nueva abierto y el mantenedor sube la revisión del mismo paquete** por otro PR:
  al integrarse uno, el detector rehace el otro sobre la rama principal actualizada.
- **Upstream fuera de la forja** (sin releases consultables): el paquete se omite del detector y
  se informa en el resumen del ciclo; no es un fallo.
- **Ciclo que se solapa con el anterior** (máquina lenta o apagada): nunca corren dos ciclos a la
  vez; uno que llega con otro en marcha espera a que termine y entonces, si no ha vencido el
  intervalo, termina sin hacer nada. Un ciclo manual forzado corre completo, pero siempre después
  del anterior.
- **PC apagada** a la hora del ciclo: el ciclo corre al volver a encenderse, una sola vez.
- **Etiquetas con formatos distintos** (`v1.2.3`, `1.2.3`, `release-1.2.3`): la versión se obtiene
  según la convención de etiquetas que ya declara cada manifiesto.

## Requirements *(mandatory)*

### Functional Requirements

**Detección**

- **FR-001**: El sistema MUST revisar los releases upstream de cada paquete del repositorio de
  paquetes de forma periódica, con un intervalo configurable en el repositorio de paquetes cuyo
  valor por defecto es 24 horas.
- **FR-002**: El mantenedor MUST poder lanzar un ciclo a mano, para todos los paquetes o para uno.
- **FR-003**: El sistema MUST considerar solo versiones estables: MUST descartar los releases
  marcados como pre-release por upstream y los que tengan en su versión o etiqueta un marcador de
  versión preliminar (alpha, beta, rc, preview, nightly, tip, canary, dev).
- **FR-003a**: El manifiesto de un paquete MAY afinar cómo se reconoce una versión estable de su
  upstream: declarar el patrón de sus etiquetas estables y/o indicar que la marca de pre-release
  de upstream no es fiable para ese paquete. Esa declaración MUST NOT permitir proponer una
  versión con un marcador preliminar en su versión: el filtro de FR-003 sobre la versión se
  aplica siempre (constitución, Alcance y restricciones).
- **FR-004**: El sistema MUST proponer la versión estable más reciente que sea mayor que la
  versión del manifiesto en la rama principal, comparando como versiones (no como texto), entre
  las que lleven publicadas upstream al menos el tiempo de espera.
- **FR-004a**: El tiempo de espera MUST ser de 48 horas por defecto, configurable solo de forma
  global en el repositorio de paquetes; MUST aplicarse a todos los paquetes, con o sin auto-merge,
  y también a los ciclos lanzados a mano. Se cuenta desde la fecha de publicación del release
  upstream.
- **FR-005**: Dos ciclos MUST NOT correr a la vez; un ciclo que encuentra otro en marcha MUST
  terminar sin efectos.

**Propuesta**

- **FR-006**: Ante una versión nueva, el sistema MUST abrir un pull request contra la rama
  principal que cambie solo la versión del manifiesto, reinicie su revisión de empaquetado a `1` y
  regenere el lockfile del paquete con la orden explícita de lockfile ya existente.
- **FR-007**: Antes de abrir el PR, el sistema MUST verificar los artefactos de la versión nueva
  contra los checksums que publique upstream (principio III) y MUST comprobar que la licencia de
  esa versión exacta está en la allowlist (principio IV).
- **FR-008**: MUST existir como máximo un PR abierto del detector por paquete. Si ya existe uno y
  aparece una versión más nueva, el sistema MUST actualizar ese PR a la nueva versión en lugar de
  abrir otro.
- **FR-009**: El sistema MUST NOT sobrescribir commits que no sean suyos en la rama de un PR del
  detector; en ese caso MUST comentar en el PR la versión más nueva disponible.
- **FR-010**: Cada PR MUST indicar la versión anterior y la nueva, el enlace a las notas del
  release upstream, si la licencia cambió y si el paquete tiene auto-merge.
- **FR-011**: Los PR del detector MUST pasar por el mismo pipeline de PR (fase 003) y las mismas
  reglas de la rama principal que un cambio humano; el detector MUST NOT saltarse ninguna
  validación ni integrar sin que el check obligatorio esté en verde.
- **FR-012**: El detector MUST NOT firmar, publicar ni tener acceso a la llave de firma ni a las
  credenciales de publicación.

**Auto-merge**

- **FR-013**: Cada manifiesto MUST poder declarar si sus PR de versión nueva se integran solos;
  el valor por defecto MUST ser que no.
- **FR-013a**: Cada manifiesto MAY excluir su paquete del detector; un paquete excluido, o sin
  upstream consultable, MUST aparecer como omitido en el resumen del ciclo y no es un fallo.
- **FR-014**: Con auto-merge activo, el PR MUST integrarse solo cuando todas las validaciones
  obligatorias pasen, y MUST NOT integrarse si alguna falla o está pendiente.
- **FR-015**: Con auto-merge activo, un PR MUST esperar al mantenedor si su versión nueva cambia
  la licencia (aunque la nueva esté permitida), si cambia algo del manifiesto además de versión y
  revisión, o si es un cambio de versión mayor.
- **FR-015a**: Un cambio de versión mayor es un cambio del primer componente numérico de la
  versión upstream (4.x → 5.0); si ese componente es `0`, también lo es un cambio del segundo
  (0.46 → 0.47), porque en las versiones `0.x` el segundo componente marca las rupturas. Las
  versiones que no siguen ese esquema de componentes numéricos se tratan siempre como mayores.
  El PR MUST indicar si es un cambio de versión mayor.

**Notificaciones y reintentos**

- **FR-016**: El sistema MUST notificar al mantenedor, por el mismo canal de notificaciones del
  pipeline (fase 003), cuando la versión nueva: (a) tiene una licencia fuera de la allowlist o no
  identificable; (b) no tiene los assets que espera el manifiesto (formato o nombre cambiado);
  (c) no supera la verificación de checksums upstream; (d) tiene hashes distintos de los ya
  propuestos para el mismo tag.
- **FR-017**: Cada notificación MUST indicar el paquete, la versión upstream, el motivo exacto y
  los datos para corregirlo (licencia anterior y nueva, assets esperados y encontrados, hashes).
- **FR-018**: El sistema MUST NOT reintentar ni volver a notificar una misma combinación de
  paquete, versión y motivo mientras su notificación siga abierta; MUST volver a intentarlo cuando
  cambie el manifiesto del paquete o aparezca otra versión upstream.
- **FR-019**: Una notificación del detector MUST cerrarse sola cuando se abra un PR válido para
  ese paquete o la versión deje de ser la más reciente, con referencia a lo que la resolvió.
- **FR-020**: La falta de assets en un release solo MUST evaluarse cuando el release ya es
  elegible (FR-004a); la espera cubre los assets que upstream sube con retraso, y la falta tras la
  espera MUST notificarse en ese mismo ciclo (FR-016 b).

**Límites y operación**

- **FR-021**: El sistema MUST respetar los límites de peticiones de la forja: MUST reutilizar
  respuestas no modificadas, MUST detener el ciclo antes de agotar el límite y MUST retomar los
  paquetes pendientes en el ciclo siguiente.
- **FR-022**: Un ciclo sin novedades MUST NOT crear commits, ramas, PRs ni notificaciones.
- **FR-023**: Cada ciclo MUST dejar un resumen consultable: paquetes revisados, versión
  publicada y upstream de cada uno, decisión tomada (sin cambios, PR abierto o actualizado,
  descartada por preliminar, notificada, omitida por límite) y peticiones consumidas.
- **FR-024**: El detector MUST funcionar sin depender de servicios de IA (principio X); si en el
  futuro una IA comenta los PR, el detector MUST funcionar igual sin ella.

### Key Entities

- **Release upstream**: versión publicada por el autor del software; tiene etiqueta, versión,
  marca de pre-release, fecha, notas, assets y licencia en esa etiqueta.
- **Candidata**: la versión estable más reciente mayor que la del manifiesto en la rama
  principal y con al menos 48 horas desde su publicación upstream; es lo que el detector propone.
- **PR de actualización**: el único PR abierto del detector para un paquete; contiene manifiesto y
  lockfile de la candidata y su estado de auto-merge.
- **Notificación del detector**: aviso por paquete, versión y motivo (licencia, assets, checksums,
  hashes cambiados); bloquea los reintentos de esa combinación mientras está abierta.
- **Ciclo**: una pasada del detector por todos los paquetes, con su resumen y su consumo de
  peticiones.
- **Política de auto-merge**: declaración por paquete en su manifiesto, desactivada por defecto;
  aun activa, nunca cubre cambios de versión mayor, de licencia ni de otros campos del manifiesto.
- **Exclusión del detector**: declaración opcional por paquete en su manifiesto (FR-013a).
- **Regla de versión estable**: declaración opcional por paquete en su manifiesto (patrón de
  etiquetas estables, fiabilidad de la marca de pre-release de upstream); nunca admite marcadores
  preliminares en la versión.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Tras un release estable nuevo simulado de `bob`, existe un PR correcto antes de que
  termine el primer ciclo posterior a las 48 horas de espera (≤ 72 h desde el release con el
  intervalo por defecto, o en minutos con un ciclo manual sobre un release ya elegible), con
  versión, revisión y lockfile correctos y su check en verde; antes de las 48 horas no existe
  ningún PR.
- **SC-002**: Con auto-merge activo para `bob`, la versión nueva queda instalable con `apt install`
  en las 5 suites y las 2 arquitecturas sin ninguna acción humana desde el release upstream, a
  más tardar unas horas después del primer ciclo posterior a la espera de 48 horas.
- **SC-003**: Un release marcado como pre-release o con un marcador preliminar en su versión
  produce 0 PRs y 0 notificaciones.
- **SC-004**: Un cambio de licencia a una no permitida produce exactamente 1 notificación a lo
  largo de cualquier número de ciclos sin cambios, 0 PRs y 0 publicaciones.
- **SC-005**: En ningún momento hay más de 1 PR abierto del detector por paquete.
- **SC-006**: Un ciclo sin novedades sobre el catálogo actual consume como máximo una consulta
  por paquete a la forja y no deja ningún cambio en el repositorio.
- **SC-007**: Un ciclo nunca agota el límite de peticiones de la forja, aunque el catálogo crezca
  a 100 paquetes.
- **SC-008**: El mantenedor dedica 0 minutos al camino feliz de los paquetes con auto-merge, y
  solo revisa PRs o notificaciones cuando algo requiere una decisión.

## Assumptions

- Todos los paquetes actuales tienen su upstream en GitHub (`upstream: owner/repo` en el
  manifiesto) y publican releases allí; otros orígenes quedan fuera de esta fase.
- El ciclo periódico lo dispara la propia PC (como el refirmado semanal de la fase 003), porque
  las ejecuciones programadas de la forja se desactivan tras 60 días sin actividad; "cada 24 horas"
  significa una vez al día mientras la PC esté encendida, con recuperación al encenderla.
- El detector solo actualiza versión, revisión y lockfile. Si una versión nueva necesita otros
  cambios del manifiesto (assets renombrados, suites excluidas, overrides de lintian), se
  notifica y lo resuelve el mantenedor con un PR normal.
- Las comprobaciones de "no duplicar" (principio V) no se repiten en cada versión nueva: si
  Debian, el autor o una comunidad empiezan a cubrir el paquete, se detecta por otra vía; queda
  fuera de esta fase.
- Las notificaciones usan el canal ya existente del pipeline (issues de la forja con la etiqueta
  de fallos), sin servicios ni credenciales nuevos.
- El PR de versión nueva sigue el mismo camino que cualquier PR: check obligatorio `verify` en la
  rama principal, publicación por la ejecución de la rama principal y PR de estado posterior.
- La forma en que un PR abierto por automatización obtiene su check obligatorio y puede
  integrarse solo (la fase 004 mostró que la forja trata de forma especial los PR que abre su
  propio token) es una decisión del plan, no de esta especificación.
- Compilar desde fuente (nivel 2) sigue fuera de alcance; el detector cubre los paquetes de
  nivel 1.
