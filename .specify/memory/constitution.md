# debfactory Constitution

debfactory es una fábrica declarativa que reempaqueta herramientas de desarrollo modernas como
paquetes `.deb` y las publica en un repositorio APT firmado para Debian y Ubuntu. La mantiene una
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
- Si upstream no publica checksum, el hash SHA-256 MUST registrarse en el manifiesto; cualquier
  discrepancia posterior MUST hacer fallar el build.
- Solo se descargan artefactos desde las URLs de release oficiales declaradas en el manifiesto;
  cualquier otro origen MUST hacer fallar el build.
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
- MUST NOT empaquetarse software cuya versión en Debian estable esté al día con upstream.
- MUST NOT empaquetarse software que ya se distribuya en un repositorio APT comunitario conocido
  y mantenido (por ejemplo, griffo, `debian.griffo.io`). La lista de repositorios comunitarios
  conocidos se mantiene versionada en el repositorio de debfactory; ampliarla no requiere
  enmienda.
- Cada manifiesto MUST documentar por qué el paquete aporta valor frente a las alternativas
  existentes, confirmando las tres comprobaciones anteriores.

*Rationale*: cada paquete es mantenimiento perpetuo; solo se asume cuando nadie más lo cubre,
sea Debian, el propio autor u otra comunidad.

### VI. Versionado compatible con Debian

- **Nivel 1 (reempaquetado)**: formato `<upstream>-0~pav<N>`, por ejemplo `1.40.0-0~pav1`.
  - La revisión Debian MUST fijarse en `0`.
  - `N` cuenta los reempaquetados de la misma versión upstream: empieza en `1`, sube con cada
    corrección del empaquetado sin cambio upstream (`1.40.0-0~pav2`) y se reinicia en `1` con
    cada versión upstream nueva.
  - Un mismo `.deb` de nivel 1 MUST publicarse en todas las suites soportadas (no excluidas).
- **Nivel 2 (compilación desde fuente)**: como cada suite produce un binario distinto, MUST
  agregarse un sufijo por suite para que nunca existan dos archivos distintos con el mismo nombre
  en el pool: `<upstream>-0~pav<N>+<suite>`, por ejemplo `1.2.3-0~pav1+deb13`.
  - Sufijos de suite: `deb12` (bookworm), `deb13` (trixie), `debsid` (sid), `u2404` (noble),
    `u2604` (resolute). `N` sigue las mismas reglas que en nivel 1.
- Con la revisión en `0` y el sufijo `~`, cualquier versión oficial de Debian u Ubuntu del mismo
  upstream (por ejemplo `1.40.0-1`) siempre es mayor y gana, y una versión upstream más nueva de
  debfactory siempre supera a una oficial más vieja.
- Las versiones publicadas MUST NOT reescribirse ni reutilizarse.
- Las pruebas MUST validar el orden esperado con `dpkg --compare-versions`.

*Rationale*: convivir con Debian sin pisarlo, sin colisiones en el pool y sin romper la caché ni
la confianza de APT.

### VII. Reproducible y verificable

- El mismo manifiesto con la misma versión upstream MUST producir el mismo `.deb`: mismos
  contenidos y metadatos, con fechas fijadas (p. ej. `SOURCE_DATE_EPOCH`).
- Todo paquete MUST pasar `lintian` sin errores.
- Todo paquete MUST pasar una prueba de instalación real en un contenedor limpio de cada suite
  soportada (no excluida) y de cada arquitectura soportada antes de publicarse.
- Si la prueba falla en cualquier suite no excluida, el paquete MUST NOT publicarse en ninguna.

*Rationale*: un repositorio parcialmente roto es peor que una actualización retrasada.

### VIII. Automatización sin humanos en el camino feliz

- La cadena detectar versión nueva → construir → probar → publicar MUST ejecutarse sin
  intervención humana.
- Un humano solo interviene ante fallos.
- Todo fallo MUST notificarse al mantenedor indicando el paquete, la etapa y el motivo exacto.

*Rationale*: el tiempo del único mantenedor se reserva para lo que una máquina no puede decidir.

### IX. Simplicidad operativa

- Se MUST preferir herramientas estándar, maduras y empaquetadas en Debian.
- Toda la operación MUST correr en una sola PC Debian (64 GB RAM) más un hosting estático; no se
  introducen servicios que requieran infraestructura adicional.
- El hosting público es almacenamiento estático S3-compatible (actualmente Cloudflare R2). El
  diseño MUST ser agnóstico del proveedor: solo se usan operaciones del API S3 estándar, y el
  proveedor, endpoint y bucket se configuran, nunca se codifican.
- Todo componente nuevo MUST poder explicarse en un párrafo y operarse con un comando documentado.

*Rationale*: lo que no se puede explicar ni operar en un comando no se mantendrá en tiempo libre,
y cambiar de proveedor de hosting no debe requerir cambiar código.

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

- **Arquitecturas**: `amd64` y `arm64`.
- **Suites soportadas**: Debian `bookworm`, `trixie` y `sid`; Ubuntu `noble` (24.04 LTS) y
  `resolute` (26.04 LTS).
- **Exclusiones por paquete**: un manifiesto MAY excluir suites concretas (p. ej. si el binario
  upstream requiere una glibc más nueva que la de `bookworm`), documentando el motivo. Excluir una
  suite no bloquea la publicación en las demás; las suites excluidas no cuentan para la regla de
  "falla en una, no se publica en ninguna" del principio VII.
- **Hosting público**: almacenamiento estático S3-compatible (Cloudflare R2), con diseño
  agnóstico del proveedor (ver IX).
- **Paquetes iniciales**: `fnm`, `pnpm`, `bob`, `buf`, `aqua`. Cada uno MUST cumplir todos los
  principios (en particular V) antes de publicarse.
- Ampliar arquitecturas o suites es una enmienda a esta sección.

## Flujo de publicación y quality gates

Todo paquete atraviesa, en este orden, los siguientes gates; cualquier fallo detiene el flujo y
se notifica según el principio VIII:

1. **Validación del manifiesto** contra el esquema (I, II, V).
2. **Gate de licencias** sobre la versión exacta (IV).
3. **Descarga desde URL declarada y verificación** de checksum/firma (III).
4. **Construcción** reproducible en entorno limpio (II, VII).
5. **lintian** sin errores (VII).
6. **Prueba de instalación** en contenedor limpio por suite y arquitectura no excluidas (VII).
7. **Publicación atómica** en el repositorio con índices firmados con GPG (III, VI).

## Governance

- Esta constitución prevalece sobre cualquier otra práctica, documento o convención del proyecto.
- **Enmiendas**: todo cambio MUST documentar su motivo y actualizar en el mismo cambio las
  plantillas y documentos afectados (`.specify/templates/*`, guías de operación).
- **Versionado de la constitución** (SemVer):
  - MAJOR: eliminación o redefinición incompatible de un principio o de la gobernanza.
  - MINOR: principio o sección nuevos, o ampliación material de una regla (incluye ampliar
    suites, arquitecturas o la allowlist de licencias).
  - PATCH: aclaraciones y correcciones de redacción sin cambio semántico.
- **Cumplimiento**: cada plan (`plan.md`) MUST incluir una verificación explícita contra cada uno
  de los principios I–X en su sección "Constitution Check". Toda desviación MUST justificarse en
  la tabla de seguimiento de complejidad del plan o el plan no se aprueba.

**Version**: 1.3.0 | **Ratified**: 2026-11-11 | **Last Amended**: 2026-11-11
