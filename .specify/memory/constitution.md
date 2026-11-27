# debfactory Constitution

debfactory es una fábrica declarativa que reempaqueta herramientas de desarrollo modernas como
paquetes `.deb` y las publica en un repositorio APT firmado para Debian y Ubuntu. El repositorio
se publica bajo el proyecto **OpenTepache** (opentepache.org); debfactory es la herramienta que
lo construye. La mantiene una
sola persona en su tiempo libre: **la mantenibilidad manda sobre todo lo demás**. Ante cualquier
conflicto entre principios, gana la opción que menos carga operativa añade al mantenedor sin
violar los principios III (seguridad), IV (licencias) y X (límites de la IA), que nunca se
relajan.

## Core Principles

### I. Declarativo primero

- Cada paquete MUST definirse únicamente mediante un manifiesto de datos, un archivo por paquete.
- Agregar un paquete nuevo MUST NOT requerir escribir ni copiar código; basta con añadir su
  manifiesto.
- Si un paquete necesita lógica especial, primero MUST extenderse el esquema del manifiesto de
  forma genérica (reutilizable por otros paquetes).
- Los hooks de código por paquete son la última opción: solo se permiten si la extensión genérica
  del esquema no es viable, y el manifiesto MUST documentar la justificación.

*Rationale*: el coste de mantenimiento debe crecer con el esquema, no con el número de paquetes.

### II. Dos niveles de construcción

- **Nivel 1 — Reempaquetado** (por defecto): se usa el binario oficial publicado por upstream.
- **Nivel 2 — Compilación desde fuente** en un entorno limpio: solo se permite cuando no existe
  un binario upstream utilizable o el software depende de librerías del sistema. El manifiesto
  MUST declarar el nivel y, para Nivel 2, justificarlo.
- MUST NOT publicarse un paquete fuente que no corresponda exactamente al binario publicado.

*Rationale*: reempaquetar es barato y fiel a upstream; compilar es caro y solo se paga cuando
aporta algo que el binario upstream no puede dar.

### III. Seguridad de la cadena de suministro

- Todo artefacto descargado MUST verificarse contra los checksums o firmas publicados por
  upstream cuando existan.
- Si upstream no publica checksum, el hash SHA-256 MUST registrarse en el lockfile del paquete
  (archivo de datos versionado junto al manifiesto, generado por una orden explícita y nunca por
  el build); cualquier discrepancia posterior MUST hacer fallar el build.
- Solo se descargan artefactos (los archivos que se instalan) desde las URLs de release
  oficiales declaradas en el manifiesto; cualquier otro origen MUST hacer fallar el build.
- Los metadatos de verificación (archivos de checksums, firmas y el texto de licencia) MAY
  descargarse del mismo release o del repositorio upstream fijado al tag exacto de la versión.
  Su URL MUST derivarse del repositorio y del tag declarados en el manifiesto (nunca ser libre)
  y su hash MUST registrarse en el lockfile del paquete.
- El repositorio APT y sus índices MUST ir siempre firmados con GPG.
- La llave privada de firma MUST NOT residir en el repositorio de código ni aparecer en logs, y
  MUST tener un respaldo documentado fuera de la máquina de build.

*Rationale*: los usuarios confían en la firma del repositorio; esa confianza solo es legítima si
cada eslabón previo es verificable.

### IV. Gate de licencias (falla cerrado)

- Antes de construir, MUST validarse la licencia upstream de la versión exacta a empaquetar.
- Solo se permiten licencias libres incluidas en una lista explícita (allowlist) versionada en el
  repositorio. Contenido inicial: MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, MPL-2.0,
  GPL-2.0, GPL-3.0, LGPL-2.1, LGPL-3.0. Ampliarla requiere documentar el motivo y que la
  licencia sea libre (aprobada por OSI y compatible con las DFSG).
- Licencia desconocida, propietaria o no identificable MUST hacer fallar el build.
- Cada paquete MUST incluir su licencia en `debian/copyright`.

*Rationale*: ante la duda no se publica; un error de licencia es más caro que un paquete ausente.

### V. No duplicar

- MUST NOT empaquetarse software que ya tenga un repositorio APT oficial mantenido por su autor.
- MUST NOT empaquetarse software cuya versión en Debian estable esté al día con la última
  versión estable de upstream.
- MUST NOT empaquetarse software que ya se distribuya gratis, sin registro y al día con la última
  versión estable de upstream en un repositorio APT comunitario conocido y mantenido. Un
  repositorio que exige pago, suscripción o registro para instalar no cuenta. La lista de
  repositorios comunitarios conocidos se mantiene versionada en la configuración del repositorio
  de paquetes; ampliarla no requiere enmienda.
- Cada manifiesto MUST documentar por qué el paquete aporta valor frente a las alternativas
  existentes, confirmando las tres comprobaciones anteriores.

*Rationale*: cada paquete es mantenimiento perpetuo; solo se asume cuando nadie más lo cubre,
sea Debian, el propio autor u otra comunidad. Un repositorio de pago o desactualizado no cubre a
los usuarios, y ofrecer una alternativa a software libre redistribuible es legítimo si se
construye desde upstream, sin copiar el empaquetado de terceros ni usar su marca.

### VI. Versionado compatible con Debian

- **Nivel 1 (reempaquetado)**: formato `<upstream>-0~tepache<N>`, por ejemplo
  `1.40.0-0~tepache1`.
  - La revisión Debian MUST fijarse en `0`.
  - `N` cuenta los reempaquetados de la misma versión upstream: empieza en `1`, sube con cada
    corrección del empaquetado sin cambio upstream (`1.40.0-0~tepache2`) y se reinicia en `1` con
    cada versión upstream nueva.
  - Un mismo `.deb` de nivel 1 MUST publicarse en todas las suites soportadas (no excluidas).
- **Nivel 2 (compilación desde fuente)**: como cada suite produce un binario distinto, MUST
  agregarse un sufijo por suite para que nunca existan dos archivos distintos con el mismo nombre
  en el pool: `<upstream>-0~tepache<N>+<suite>`, por ejemplo `1.2.3-0~tepache1+deb13`.
  - Cada suite declara su sufijo en la configuración (`level2_suffix`), único entre las suites;
    por ejemplo `deb12` (bookworm), `deb13` (trixie), `debsid` (sid), `u2404` (noble). `N` sigue
    las mismas reglas que en nivel 1.
- Con la revisión en `0` y el sufijo `~`, cualquier versión oficial de Debian u Ubuntu del mismo
  upstream (por ejemplo `1.40.0-1`) siempre es mayor y gana, y una versión upstream más nueva de
  OpenTepache siempre supera a una oficial más vieja.
- Las versiones publicadas MUST NOT reescribirse ni reutilizarse.
- Las pruebas MUST validar el orden esperado con `dpkg --compare-versions`.

*Rationale*: convivir con Debian sin pisarlo, sin colisiones en el pool y sin romper la caché ni
la confianza de APT.

### VII. Reproducible y verificable

- El mismo manifiesto con la misma versión upstream MUST producir el mismo `.deb`: mismos
  contenidos y metadatos, con fechas fijadas (p. ej. `SOURCE_DATE_EPOCH`).
- Todo paquete MUST pasar `lintian` sin errores. Un error solo MAY silenciarse con un override
  declarado en el manifiesto, por etiqueta y con motivo, cuando no pueda corregirse en nivel 1
  (p. ej. `statically-linked-binary` en binarios upstream estáticos). Las etiquetas de
  seguridad, licencia o copyright MUST NOT silenciarse; su lista se mantiene versionada en la
  configuración compartida.
- Todo paquete MUST pasar una prueba de instalación real en un contenedor limpio de cada suite
  soportada no excluida y de cada arquitectura soportada no excluida antes de publicarse.
- Si la prueba falla en cualquier suite o arquitectura no excluida, el paquete MUST NOT
  publicarse en ninguna.

*Rationale*: un repositorio parcialmente roto es peor que una actualización retrasada.

### VIII. Automatización sin humanos en el camino feliz

- La cadena detectar versión nueva → construir → probar → publicar MUST ejecutarse sin
  intervención humana.
- Un humano solo interviene ante fallos.
- Todo fallo MUST notificarse al mantenedor indicando el paquete, la etapa y el motivo exacto.

*Rationale*: el tiempo del único mantenedor se reserva para lo que una máquina no puede decidir.

### IX. Simplicidad operativa

- Se MUST preferir herramientas estándar, maduras y empaquetadas en Debian.
- Toda la operación MUST correr en una sola PC Debian (amd64, 16 núcleos, 31 GB RAM) más un
  hosting estático; no se introducen servicios que requieran infraestructura adicional. Única
  excepción, la compilación de nivel 2 (ver abajo).
- **Compilación de nivel 2 en runners de GitHub**: los paquetes de nivel 2 MAY compilarse en
  runners estándar hospedados por GitHub Actions (las etiquetas gratuitas para repositorios
  públicos, p. ej. `ubuntu-24.04` y `ubuntu-24.04-arm`; nunca runners "larger" ni de pago), en
  paralelo por suite, arquitectura y compilación, dentro del contenedor oficial de la suite. Reglas:
  - esos jobs MUST correr sin secretos y con permisos de solo lectura;
  - cada artefacto MUST llevar una atestación de procedencia de GitHub que el runner `pub`
    verifique (repositorio, workflow y commit) antes de usarlo;
  - las dos compilaciones de la doble compilación MUST correr en jobs distintos y dar el mismo
    resultado byte a byte;
  - las pruebas (lintian, instalación y ciclo de vida por suite y arquitectura), la firma y la
    publicación MUST seguir en la PC del mantenedor;
  - el repositorio de paquetes MUST ser público mientras se use esta vía; si deja de serlo, el
    nivel 2 vuelve a compilarse en la PC;
  - si un paquete no cabe en un runner estándar (memoria, disco o tiempo), se excluye esa
    arquitectura en su manifiesto con su motivo (Alcance, exclusiones).

  El nivel 1 sigue construyéndose en la PC. Esta vía no introduce servicios propios ni secretos
  fuera de la PC.
- El hosting público es almacenamiento estático S3-compatible (actualmente Cloudflare R2). El
  diseño MUST ser agnóstico del proveedor: solo se usan operaciones del API S3 estándar, y el
  proveedor, endpoint y bucket se configuran, nunca se codifican.
- Todo componente nuevo MUST poder explicarse en un párrafo y operarse con un comando documentado.

*Rationale*: lo que no se puede explicar ni operar en un comando no se mantendrá en tiempo libre,
y cambiar de proveedor de hosting no debe requerir cambiar código. Compilar desde el código fuente
en `arm64` emulado tarda horas en la PC; los runners gratuitos lo hacen en minutos y en paralelo, y
como no reciben secretos ni firman, la confianza en lo publicado sigue en la PC (atestación, doble
compilación idéntica, pruebas propias).

### X. La IA propone, el pipeline decide

- Los agentes de IA solo MAY actuar abriendo o comentando pull requests e issues.
- Los agentes de IA MUST NOT firmar, publicar ni hacer merge por sí mismos, MUST NOT modificar
  la allowlist de licencias y MUST NOT aceptar checksums nuevos sin que el pipeline determinista
  los verifique.
- Todo lo que proponga una IA MUST pasar por las mismas validaciones que un cambio humano.
- Los jobs con IA MUST correr sin acceso a secretos (llave GPG, credenciales de publicación) y
  con permisos mínimos, porque procesan texto controlado por terceros (release notes, READMEs)
  que puede contener inyección de prompts.
- Si la IA no está disponible, la fábrica MUST seguir funcionando igual: la IA es un acelerador,
  nunca una dependencia.

*Rationale*: el texto de upstream es entrada no confiable; la autoridad para firmar y publicar
reside solo en el pipeline determinista y en el mantenedor.

## Alcance y restricciones

- **Proyecto y repositorio**: el repositorio APT se publica como OpenTepache en
  `opentepache.org`. Sus campos `Origin` y `Label` MUST valer `OpenTepache`. debfactory es el
  nombre de la herramienta, no del repositorio.
- **Arquitecturas**: `amd64` y `arm64`.
- **Suites soportadas**: las define la configuración del repositorio de paquetes
  (`config/debfactory.yaml`, bloque `suites`). Agregar, quitar o renombrar una suite o una versión
  de sistema operativo es un cambio de configuración por PR, no una enmienda. Toda suite
  configurada MUST:
  - ser de Debian, de Ubuntu o de una distribución basada en ellas que use paquetes `.deb`;
  - tener una imagen de contenedor oficial de esa distribución para las pruebas de instalación;
  - pasar las mismas pruebas que las demás: lintian y la instalación en contenedor limpio por cada
    arquitectura soportada (gate 6);
  - contar para la regla "falla en una, no se publica en ninguna" del principio VII, salvo que un
    manifiesto la excluya.

  Retirar una suite MUST anunciarse en la documentación de usuario antes de dejar de publicar en
  ella.
- **Exclusiones por paquete**: un manifiesto MAY excluir suites concretas (p. ej. si el binario
  upstream requiere una glibc más nueva que la de `bookworm`), documentando el motivo. Excluir una
  suite no bloquea la publicación en las demás; las suites excluidas no cuentan para la regla de
  "falla en una, no se publica en ninguna" del principio VII.
- **Exclusión de arquitecturas por paquete**: un manifiesto MAY excluir una arquitectura
  soportada concreta, documentando el motivo, cuando compilarla o probarla no sea viable con los
  recursos del principio IX (p. ej. un paquete de nivel 2 que tarda más de una hora por
  compilación en `arm64` emulado). Reglas:
  - excluir una arquitectura no bloquea la publicación en las demás; las arquitecturas excluidas
    no cuentan para la regla "falla en una, no se publica en ninguna" (VII) ni para el gate 6;
  - un paquete MUST conservar al menos una arquitectura soportada;
  - quitar la exclusión amplía la versión vigente a esa arquitectura con el mismo mecanismo que
    una suite nueva, sin reescribir lo ya publicado;
  - el sitio y la documentación de usuario MUST mostrar en qué arquitecturas está cada paquete.

  Las arquitecturas soportadas del repositorio siguen fijas (ver arriba); excluir una en un
  paquete no es ampliarlas ni reducirlas.
- **Hosting público**: almacenamiento estático S3-compatible (Cloudflare R2), con diseño
  agnóstico del proveedor (ver IX).
- **Solo versiones estables**: debfactory MUST empaquetar únicamente versiones estables y de
  producción de upstream. MUST NOT empaquetar versiones preliminares (alpha, beta, rc, preview,
  nightly, tip, canary, dev) ni releases marcadas como pre-release por upstream. El repositorio
  es para uso productivo y su promesa al usuario es estabilidad.
- **Paquetes iniciales**: `fnm`, `bob`, `buf`, `aqua`. Cada uno MUST cumplir todos los
  principios (en particular V) antes de publicarse.
- Ampliar las arquitecturas es una enmienda a esta sección; las suites, no (ver arriba).

## Flujo de publicación y quality gates

Todo paquete atraviesa, en este orden, los siguientes gates; cualquier fallo detiene el flujo y
se notifica según el principio VIII:

1. **Validación del manifiesto** contra el esquema (I, II, V).
2. **Descarga desde URL declarada y verificación** de checksum/firma (III).
3. **Gate de licencias** sobre la versión exacta, con el texto ya verificado, antes de
   cualquier paso de construcción (IV).
4. **Construcción** reproducible en entorno limpio (II, VII).
5. **lintian** sin errores (VII).
6. **Prueba de instalación** en contenedor limpio por cada suite no excluida y cada
   arquitectura soportada no excluida (VII).
7. **Publicación atómica** en el repositorio con índices firmados con GPG (III, VI).

## Governance

- Esta constitución prevalece sobre cualquier otra práctica, documento o convención del proyecto.
- **Enmiendas**: todo cambio MUST documentar su motivo y actualizar en el mismo cambio las
  plantillas y documentos afectados (`.specify/templates/*`, guías de operación).
- **Versionado de la constitución** (SemVer):
  - MAJOR: eliminación o redefinición incompatible de un principio o de la gobernanza.
  - MINOR: principio o sección nuevos, o ampliación material de una regla (incluye ampliar
    arquitecturas o la allowlist de licencias; las suites son configuración).
  - PATCH: aclaraciones y correcciones de redacción sin cambio semántico.
- **Cumplimiento**: cada plan (`plan.md`) MUST incluir una verificación explícita contra cada uno
  de los principios I–X en su sección "Constitution Check". Toda desviación MUST justificarse en
  la tabla de seguimiento de complejidad del plan o el plan no se aprueba.

**Version**: 1.10.0 | **Ratified**: 2026-11-11 | **Last Amended**: 2026-11-27
