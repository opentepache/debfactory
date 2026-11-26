# Research: Suites configurables y Debian forky

Decisiones de la fase 007. Referencias `FR-`/`SC-` de [spec.md](spec.md).

## D1. La suite se describe entera en la configuración

**Decisión**: cada entrada de `suites` en `config/debfactory.yaml` gana cuatro campos:

| Campo | Ejemplo (forky) | Uso |
|---|---|---|
| `distro` | `debian` | agrupa las suites en el sitio y elige el bloque de la portada; `debian` o `ubuntu` (constitución v1.8.0, alcance) |
| `label` | `Debian 14 (testing)` | nombre legible: tabla de disponibilidad, JSON-LD, títulos |
| `version` | `14` | versión corta: "Debian 12, 13, 14, sid" |
| `catchall` | `true` solo en sid | la suite que recibe cualquier versión de su distribución que no coincida con otra (D4); a lo sumo una por distribución |

Los cuatro son obligatorios salvo `catchall`, que por defecto es `false`. La validación nombra la
suite y el campo (FR-001). `internal/site` deja de tener su tabla `suiteInfo` y lee todo de aquí
(FR-002, FR-003). El nombre visible de la distribución ("Debian", "Ubuntu") sale de `distro` con
la primera letra en mayúscula.

**Motivo**: la constitución v1.8.0 dice que agregar una suite es configuración; la única lista fija
que quedaba era la del sitio.

**Alternativas**: *deducir el nombre legible del codename* (necesita una tabla en el código, justo
lo que se quiere quitar); *un bloque `distros` aparte*: sobra mientras solo haya Debian y Ubuntu.

## D2. Ampliar es "publicar otra vez la misma versión"

**Hecho**: hoy, `repo.Publish` de una versión que ya está en el estado con el mismo sha256 no hace
nada (`apply.go`, `Add`: "sin cambios").

**Decisión**: en ese caso, si las suites que trae la publicación (las que pasaron la prueba
de instalación, según `test.json`) incluyen alguna que la versión publicada no tiene, se agregan.
Se registra como un cambio `add` de esas suites, sube el serial y se genera, firma y sincroniza como
cualquier publicación. Así una ampliación:

- reutiliza los mismos archivos del almacén: el sha256 tiene que coincidir en cada arquitectura, o
  falla como hoy ("ya publicada con otro sha256") (FR-005);
- es atómica con todo lo demás de esa ejecución (FR-004);
- solo se permite sobre la versión más alta del paquete (FR-007): si no lo es, falla con un mensaje
  claro y no cambia nada;
- nunca quita suites: una publicación con menos suites que las publicadas sigue sin efecto (quitar
  suites queda fuera de alcance).

**Motivo**: no hay operación nueva en el repositorio, ni en la generación, ni en la sincronización;
solo cambia la regla de "ya publicada".

## D3. `ci main` amplía solo (FR-013)

**Decisión**: `PlanMain` agrega al plan, en una lista nueva `extend`, cada paquete cuya versión del
manifiesto ya está publicada, es la más alta del paquete y le falta alguna suite configurada que su
manifiesto (de `HEAD`) no excluye. Esos paquetes pasan por los mismos gates que una versión nueva,
construidos desde `HEAD` en el checkout: build, lintian y la prueba de instalación en todas las suites
no excluidas. Luego van a la misma publicación que las versiones nuevas. Si el `.deb` no sale
idéntico al publicado, falla con la etapa `compare` y no se publica.

Por qué `HEAD` y no el commit que declaró la versión (como `ci rebuild`): la versión del manifiesto
en `HEAD` es la publicada, y un PR que cambia el manifiesto o el lockfile de una versión publicada sin
subir `revision` ya tiene que reconstruir lo mismo (comparación de la fase 003). La comparación de
sha256 confirma que es el mismo archivo.

En la ejecución que agrega una suite cambia `config/` (un `ci.code_paths`), así que el paso 3b ya
reconstruye y compara todo lo publicado. Para no construir dos veces, las versiones que se amplían
salen de esa comparación: las cubre la de la ampliación.

**Reintento** (FR-014): la lista `extend` se recalcula en cada ejecución desde el estado, así que
una ampliación que falló vuelve a intentarse en la siguiente sin guardar nada aparte. El fallo se
notifica con su etapa (`test` o `compare`) y el paquete; ese issue se cierra solo cuando la versión
queda ampliada (el aviso de "publicada" de la fase 003) o deja de estar en `extend` porque el
manifiesto excluyó la suite.

**`ci rebuild --package P --version V`**: si `V` está publicada, es la más alta y le faltan suites,
en vez de "nada que publicar" publica la ampliación con los `.deb` reconstruidos (mismo camino que
una versión no publicada). Si no le falta ninguna, sigue igual (FR-004, US2 escenario 4).

**Alternativas**: *solo manual* (descartada en el clarify: principio VIII).

## D4. La portada elige la suite por codename, desde la configuración (FR-009, FR-010)

**Decisión**: los bloques se generan desde las suites configuradas:

- **Detectar**: `case "$s" in <codenames de todas las suites sin catchall>) echo "$s"`; si no
  coincide, para cada suite `catchall` de distribución `D`, `[ "$ID" = D ]` la elige (en Debian,
  `grep sid /etc/debian_version`, como hoy); si nada, no se agrega el repositorio (fase 006). Con
  forky configurada, testing y sid (que declara el codename de testing) eligen forky.
- **Debian / Ubuntu**: `case "$VERSION_CODENAME" in <codenames de esa distribución sin catchall>)
  echo "$VERSION_CODENAME" ;; *) echo <catchall>`, o sin rama `*)` si la distribución no tiene
  catchall (Ubuntu).

`catchall` reemplaza el nombre `sid` que hoy está escrito en `commands.go`.

## D5. Pruebas: una suite ficticia demuestra "cero código" (SC-001)

**Decisión**:

- Pruebas unitarias de `config`, `site` y `repo` con una suite ficticia agregada solo en la
  configuración de prueba (`plucky` de Ubuntu, por ejemplo), sin otro cambio: el sitio la muestra,
  el bloque "Detectar" la incluye, la ampliación la acepta.
- Integración de `ci` (`citest`): publicar, agregar una suite a la configuración y ejecutar `main`:
  la versión aparece en la suite nueva con el mismo sha256 y una entrada `extend` en la auditoría; un
  fallo simulado de la prueba en esa suite deja todo igual y notifica (SC-004, SC-005).
- E2E: la matriz de `TestE2ESite` y `TestE2ERepo` ya sale de `cfg.Suites`; con forky en la
  configuración de prueba pasa a 6 suites × 2 arquitecturas (SC-002). `internal/repotest/site_test.go`
  deja de tener su mapa `distro` y lee `Suite.Distro`.

## D6. Auditoría de la ampliación (FR-008)

**Decisión**: una operación nueva `extend` en `repo/audit.jsonl`, con `package`, `version`,
`commit` (el de `HEAD`), `tool`, `debs` (los mismos sha256), `serial`, `run` y un campo nuevo
`suites` con las suites agregadas. Las comprobaciones de la auditoría (fase 003, FR-024/FR-025)
la aceptan: no cambia qué versiones existen, solo dónde.

## D7. Forky en `packages`

**Decisión**: el PR de activación en `packages` fija la herramienta nueva y, en el mismo cambio:

- agrega `distro`, `label` y `version` a las cinco suites existentes y `catchall: true` a sid;
- agrega `forky: {image: "debian:forky", level2_suffix: deb14, distro: debian, label: "Debian 14
  (testing)", version: "14"}`, entre trixie y sid;
- actualiza `docs/usuario.md` (tabla de suites con forky y la nota sobre sid).

Al integrarlo, `main.yml` amplía los 5 paquetes a forky en esa ejecución (SC-003). Una versión
anterior de la herramienta rechaza los campos nuevos de `suites`: por eso van juntos.
