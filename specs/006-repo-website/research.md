# Research: Sitio web del repositorio

Decisiones de la fase 006. Cada una dice qué se eligió, por qué y qué se descartó. Las
referencias `FR-`/`SC-` son de [spec.md](spec.md).

## D1. Generador: `debfactory site` con `html/template` de Go

**Decisión**: el sitio lo genera la propia herramienta, con un grupo de órdenes nuevo
`debfactory site` (contracts/cli.md). `site data` escribe `site.json`, el modelo completo del
sitio (data-model.md §1). `site build` lee ese modelo y renderiza las páginas con `html/template`
de la biblioteca estándar. Las plantillas, el CSS y las traducciones van embebidos en el binario
(`embed`).

**Motivo**:

- El binario ya lee manifiestos, configuración y estado; no hay otro proceso que mantener.
- `html/template` escapa por contexto (texto, atributo, URL) sin configuración: cubre FR-010 para
  textos de upstream que vienen en los manifiestos.
- Sin dependencias nuevas, sin `node_modules` y sin una segunda cadena de suministro que fijar y
  auditar (principios III y IX).
- Una sola versión de herramienta, la fijada en `debfactory.version`, determina byte a byte el
  sitio de un estado dado (FR-014, principio VII).

**Separar `data` de `build`**: el JSON es el contrato entre "qué está publicado" y "cómo se ve".
Se puede inspeccionar y probar por separado, y se publica en `/site.json` para quien quiera
consumirlo sin raspar HTML.

**Alternativas**:

- *Astro*, como propone el stack. Un sitio de unas pocas páginas por paquete, sin interactividad,
  no justifica un toolchain de Node con cientos de dependencias transitivas. Habría que fijarlo,
  auditarlo, actualizarlo y correrlo en el runner de publicación, que es el único con
  credenciales. Se descarta (III, IX).
- *Hugo* u otro generador empaquetado en Debian: tendría que leer el mismo JSON y agrega una
  herramienta y un lenguaje de plantillas más, sin ganar nada sobre `html/template`.

## D2. De dónde salen los datos: estado publicado, manifiestos y configuración, verificados contra los índices

**Decisión**: `site data` construye el modelo con tres fuentes:

| Fuente | Aporta |
|---|---|
| estado de referencia de la ejecución (data-model §1) | paquetes, versiones conservadas, fecha de publicación, suites y arquitecturas de cada versión |
| manifiestos del commit que se publica (`packages/<p>.yaml`) | resumen, descripción, licencia SPDX, `homepage`, suites excluidas y su motivo, traducciones (D7) |
| `config/debfactory.yaml` | URL pública, suites, arquitecturas, componente, fingerprints de las llaves maestras, nombre del keyring |

Antes de escribir el JSON, `site data` comprueba cada versión y suite contra los índices
`Packages` que aptly generó en la generación vigente (`dists/<suite>/main/binary-<arch>/`). Si el
estado dice que algo está publicado y no aparece en los índices, o al revés, falla con la etapa
`site` (FR-009). Una versión retirada no está en el estado, así que nunca aparece (FR-006).

**Qué estado**: el que describe la generación vigente, comprobado por su SHA-256 contra
`GenInfo.StateSHA256`. En CI es el estado que la ejecución ya determinó como referencia
(`internal/ci/run.go`), que puede venir de un PR de estado todavía sin integrar; no es
necesariamente el `repo/state.yaml` del checkout de `main`. Fuera de CI, `site data --state FILE`
(por defecto `repo/state.yaml`). Si no coincide con la generación, `site data` falla.

**Motivo**: el stack pide "manifiestos + índices de aptly". El estado es la fuente que ya produce
esos índices (`repo.Generate`) y trae la fecha de publicación, que los índices no tienen. Usar los
índices como verificación da la garantía de FR-009 sin depender de la fecha que aptly no guarda.

**Paquete con manifiesto pero sin publicar**: no está en el estado, así que no aparece (edge
case). **Paquete publicado cuyo manifiesto ya no existe** (p. ej. se borró antes de retirarlo):
aparece con los datos del estado y sin descripción, y `site data` avisa en el log.

**Alternativas**: *solo los índices de aptly*: no tienen la fecha de publicación ni el motivo de
exclusión, y la descripción del `.deb` es la del manifiesto de todos modos. *Solo los
manifiestos*: muestran lo propuesto, no lo publicado (FR-009).

## D3. Rutas: el repositorio sigue en la raíz, el inglés en la raíz, el español en `/es/`

**Decisión**:

```text
https://apt.opentepache.org/
├── index.html                    portada en inglés
├── packages/<p>/index.html       página de paquete en inglés
├── es/index.html                 portada en español
├── es/packages/<p>/index.html    página de paquete en español
├── assets/site.<hash>.css        estilos (nombre con hash del contenido: inmutable)
├── site.json                     modelo del sitio (D1)
├── debfactory.gpg                llave pública (fase 002, sin cambios)
├── dists/  pool/                 repositorio APT (fase 002, sin cambios)
```

`site build` MUST fallar si alguna ruta generada empieza por `dists/`, `pool/` o coincide con un
archivo de la raíz del repositorio (FR-023). Un paquete no puede llamarse `assets` ni `es`
(validación del manifiesto).

**Motivo**:

- **Repositorio en la raíz, no en `/apt`**, como proponía el stack. Moverlo cambia la URL que ya
  tiene cada usuario en su `/etc/apt/sources.list.d/*.sources` (`URIs:
  https://apt.opentepache.org`) y la del keyring. Todo `apt update` existente fallaría con 404
  hasta que cada usuario edite su archivo. Las rutas del sitio no chocan con `dists/`, `pool/`
  ni la llave, así que no hace falta moverlo.
- **Inglés en la raíz, español en `/es/`**, como decidió el clarify (FR-016) y no `/en/` y `/es/`
  como decía el stack. Así la URL del repositorio, la que sale en los comandos y en los
  buscadores, lleva directo al contenido.

**Alternativas**: *`/en/` y `/es/` con una raíz que elige*: sin JavaScript ni redirecciones del
servidor, la raíz sería una página de dos enlaces; un clic más para todos (se descartó en el
clarify).

## D4. `index.html` en R2: una regla de reescritura de Cloudflare

**Hecho**: un bucket de R2 público por dominio propio no sirve un documento índice. Hoy
`https://apt.opentepache.org/` responde 404, igual que cualquier ruta que termine en `/`.

**Decisión**: una regla de reescritura de URL (*Transform Rules → URL Rewrite*) en la zona de
Cloudflare. Con la condición `ends_with(http.request.uri.path, "/")`, reescribe la ruta a
`concat(http.request.uri.path, "index.html")`. Es configuración del proveedor, de una vez, y se
documenta en `docs/operacion-ci.md`. Los enlaces internos usan rutas de directorio (`/packages/bob/`).
`ci doctor` comprueba que `<public_url>/` responde 200 con `text/html`. La prueba E2E sirve el
árbol con Caddy, que resuelve el índice sin configuración.

**Motivo**: URLs limpias, que son las que la gente comparte. El diseño sigue siendo agnóstico del
proveedor (principio IX): cualquier hosting estático (S3 website, Caddy, nginx, Pages) resuelve el
índice por sí mismo o con una regla equivalente. El código no sabe nada de Cloudflare.

**Alternativas**: *enlaces explícitos a `…/index.html`*: funcionarían sin la regla, pero la raíz
`https://apt.opentepache.org/`, la URL que importa, seguiría dando 404. *Un Worker*: es código y
servicio aparte, y viola IX.

## D5. Publicación: una fase `site` después de la sincronización del repositorio

**Decisión**: `ci main`, `ci retire` y la renovación periódica (`ci refresh`) terminan con un paso
nuevo después de sincronizar el repositorio y de abrir el PR de estado. Corre siempre, también
cuando `ci main` no tiene nada que publicar: así se suben los cambios que solo tocan el sitio
(una traducción, el nombre) y se repara un sitio que quedó atrasado. Si el sitio no cambió, el
registro (punto 3) hace que no se suba nada.

1. `site data` y `site build` generan el sitio desde la generación vigente en un directorio
   temporal, que se renombra a `<data>/site/<digest>/` (digest del árbol, data-model §3). Un
   sitio que cambia sin cambiar el serial (una traducción) no choca con el anterior. Se conservan
   solo el vigente y el anterior.
2. Para cada destino configurado, una subida con rclone en tres pasos: `assets/` (inmutables, con
   el `Cache-Control` de `pool/`), después las páginas y `site.json` (con el `Cache-Control`
   mutable, hoy `max-age=60`), y al final el borrado de lo que salió hace dos sincronizaciones
   (borrado diferido, como el repositorio: specs/002, research D6).
3. Un registro por destino, `<data>/sync/<dest>.site.json` (archivos y hash del sitio), hace que
   una ejecución sin cambios no suba nada y que una ejecución que falló se ponga al día en la
   siguiente (FR-013).

**Fallos** (FR-013, clarify): un fallo en cualquier punto del paso `site` se notifica con la etapa
`site`, el paquete `-` y el motivo exacto (principio VIII). El sitio anterior queda en línea. Los
paquetes ya están publicados y el PR de estado ya está abierto, así que nada se revierte. El job
termina en rojo para que el fallo se vea en Actions, después de haber hecho todo lo demás.

**A mano** (FR-015): `debfactory site sync --dest NAME` hace la misma subida a un destino
configurado (p. ej. el de pruebas) desde la generación vigente, como `repo sync` con el
repositorio (contracts/cli.md).

**Tiempo** (SC-001): el paso corre segundos después de la sincronización. Con `max-age=60` en las
páginas, la versión nueva se ve en menos de 2 minutos desde que termina la publicación, muy por
debajo de los 10.

**Alternativas**: *meter el sitio en la generación del repositorio* (`repo.Generate`): un fallo
del sitio fallaría la generación y bloquearía la publicación de paquetes, en contra del clarify.
*Un workflow aparte* disparado tras `main.yml`: otra ejecución, otro runner con credenciales y una
ventana en la que el sitio se atrasa sin que nada lo vigile.

## D6. "Atómico para el visitante" en un hosting sin transacciones

**Hecho**: R2, como S3, no puede reemplazar varios objetos a la vez.

**Decisión**: cada página es autosuficiente. Su único recurso, el CSS, tiene un nombre con hash y
se sube antes que las páginas. Nunca se borra en la misma sincronización que deja de usarse.
Cada objeto se reemplaza de forma atómica, así que un visitante siempre recibe una página completa
y coherente consigo misma.

FR-012 (atómico página a página) se cumple así. Durante la subida, de pocos segundos, una página puede ser de la
versión nueva y otra todavía de la anterior. Nunca se ve una página a medias, ni una página sin
estilos, ni un enlace a una página que no existe. Las páginas de un paquete retirado del todo se
borran con el borrado diferido, una sincronización después.

**Alternativas**: *directorios versionados (`/v/<serial>/`) con una raíz que apunta a ellos*: sin
redirecciones del servidor, las URLs públicas cambiarían en cada publicación.

## D7. Idiomas: traducciones embebidas y descripción opcional en español en el manifiesto

**Decisión**:

- Los textos fijos (títulos, explicación del repositorio, etiquetas, pasos) están en
  `internal/site/i18n/en.yaml` y `es.yaml`, embebidos. Una prueba exige que los dos archivos
  tengan las mismas claves y que ninguna quede vacía.
- El manifiesto gana un bloque opcional `i18n.es` con `summary` y `description`, validado por el
  esquema (contracts/manifest.md). El resumen y la descripción de siempre son los del `.deb`, en
  inglés. Si falta `i18n.es`, la página en español usa los del `.deb` y lo marca con `lang="en"`
  (FR-016; edge case "descripción sin traducción").
- Los datos (versiones, fechas, suites, fingerprint, comandos) se calculan una vez en `site.json`
  y las dos plantillas los leen sin transformarlos. Así son idénticos por construcción (FR-017).
- Las fechas se muestran en formato ISO 8601 (`2026-11-13`), igual en los dos idiomas.

**Motivo**: agregar un idioma nuevo es un archivo de traducciones. Agregar un paquete no toca
nada del sitio (FR-008).

## D8. Comandos de instalación: los mismos de la guía de usuario, probados igual

**Decisión**: los bloques de la portada se generan desde la configuración: URL, nombre del
keyring, componente, y la suite tomada de `/etc/os-release` (`VERSION_CODENAME`). Son el mismo
procedimiento deb822 con `Signed-By` de `docs/usuario.md` del repositorio de paquetes (fase 002).
Hay un bloque para Debian y otro para Ubuntu, más el de quitar el repositorio. Cada bloque lleva en
el HTML un atributo `data-debfactory-step="<paso>"`, el equivalente a las marcas
```` ```sh debfactory:<paso> ```` de la guía.

La prueba E2E (`internal/repotest`) extrae esos bloques del HTML generado, no de una copia, y los
ejecuta tal cual en contenedores limpios de las 5 suites × 2 arquitecturas contra el árbol servido
con Caddy (SC-002). Para extraerlos usa el mismo mecanismo que hoy prueba la guía (`guide.go`).

**Motivo**: lo que se prueba es exactamente lo que el visitante copia.

## D9. Sin JavaScript

**Decisión**: ningún script. Los comandos van en `<pre><code>`, seleccionables con triple clic o
con el teclado. El cambio de idioma es un enlace. Un botón "copiar" exige JavaScript y queda fuera
de esta fase. FR-018 lo permitiría como mejora opcional, pero se descarta para no tener que
mantener una política de CSP con excepciones.

## D10. Calidad: pruebas en Go y una auditoría de accesibilidad en contenedor

**Decisión**:

- **Pruebas unitarias** (`internal/site`, sin etiquetas):
  - el HTML se analiza con `golang.org/x/net/html`. `golang.org/x/net` v0.60.0 ya está en
    `go.mod` como dependencia indirecta y pasa a directa, sin módulos nuevos;
  - cada página declara `lang`, tiene un solo `h1` y encabezados sin saltos;
  - cada `<img>` tiene `alt`;
  - el enlace al otro idioma existe y apunta a una página generada;
  - ningún recurso externo (FR-021): todo `href`/`src` es relativo o de `repo.url`, salvo el enlace
    a upstream;
  - presupuesto de 100 KB por página, con su CSS (FR-022);
  - contraste AA de la paleta, calculado sobre los colores del CSS;
  - salida idéntica byte a byte en dos ejecuciones (FR-014);
  - escape: una descripción con `<script>` sale como texto (FR-010).
- **Auditoría de accesibilidad** (SC-006), dentro de `just e2e`. Excepción justificada al
  principio IX ("preferir herramientas empaquetadas en Debian"): no hay un auditor WCAG
  empaquetado en Debian. La herramienta solo corre en pruebas, en contenedor fijado por digest,
  fuera de la operación y sin credenciales. Las comprobaciones en Go cubren lo básico si deja de
  estar disponible (plan.md § Complexity Tracking). axe-core vía `pa11y-ci` en un
  contenedor con digest fijado, sobre el árbol servido por Caddy, a 320 px y a 1280 px. Corre en
  contenedor y solo en pruebas: no toca el host ni el runner de publicación. Un error A o AA hace
  fallar la prueba.
- **Ancho de 320 px** (FR-020): la misma auditoría comprueba que no haya desplazamiento horizontal
  del documento.

## D11. Diseño visual

**Decisión**: tipografía del sistema (sin fuentes descargadas: FR-021, FR-022), una sola hoja de
estilos pequeña y modo oscuro con `prefers-color-scheme`. Los bloques de comandos son el elemento
protagonista: un recuadro con desplazamiento horizontal propio (FR-020). La identidad
"OpenTepache" va en texto y en un SVG en línea con `aria-hidden` y texto equivalente. La dirección
visual concreta se decide al implementar, con la guía de diseño de interfaces, dentro de estos
límites.

## D12. Determinismo

**Decisión**: el sitio no lleva la fecha de generación ni nada que dependa del reloj o del orden
de un mapa. Los paquetes se ordenan por nombre y las versiones de mayor a menor (comparación de
Debian, la de `internal/watch`). La única fecha es la de publicación de cada versión, que sale del
estado. El hash del CSS es el SHA-256 de su contenido. Con el mismo `site.json`, `site build`
produce los mismos bytes (FR-014).

## D13. Configuración

**Decisión**: un bloque opcional `site` en `config/debfactory.yaml` (contracts/config.md):
`enabled` (por defecto `false` hasta que la fase se active, como `watch`) y `name` (el nombre que
se muestra, hoy `OpenTepache`). La URL y los demás datos salen de `repo` y no se repiten.
