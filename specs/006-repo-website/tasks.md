---
description: "Task list for 006-repo-website"
---

# Tasks: Sitio web del repositorio

**Input**: Design documents from `/specs/006-repo-website/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: se incluyen porque la spec define criterios verificables por máquina: comandos en 10
contenedores (SC-002), coincidencia con lo publicado (SC-004), peso, sin JavaScript y 320 px
(SC-005), auditoría de accesibilidad (SC-006). Como en 003–005: los tests Go viven junto al
código; los que usan git real llevan el tag `integration` y los que usan Docker el tag `e2e`.
Lo que se hace en Cloudflare, en GitHub o como root en la PC es **(manual)**.

**Organization**: por historia de usuario. US1 (portada) es el MVP. US2 (páginas de paquete) y
US4 (español) se apoyan en el modelo y el render de la fase 2. US3 (publicación automática) se
puede hacer en paralelo a US2 en cuanto existe `site build`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: se puede hacer en paralelo (archivos distintos, sin dependencias pendientes)
- **[Story]**: historia de usuario de la spec (US1–US4)

---

## Phase 1: Setup

**Purpose**: esqueleto del paquete, dependencia y configuración.

- [ ] T001 Crear `internal/site/` con `doc.go`: una línea por responsabilidad según plan.md § Source Code. Crear los directorios embebidos `internal/site/templates/`, `internal/site/assets/` e `internal/site/i18n/`, con un `embed.go` que los exponga como `embed.FS`.
- [ ] T002 [P] Pasar `golang.org/x/net` (v0.60.0, ya indirecto) a dependencia directa en `go.mod` importando `golang.org/x/net/html` en un test de `internal/site`. Comprobar con `go mod tidy` que no entra ningún módulo nuevo (research D10).
- [ ] T003 [P] Bloque `site` en `internal/config/config.go`: `Site struct { Enabled bool; Name string }` con `yaml:"site"`. Validación: "`name` obligatorio si `enabled`; texto de una línea"; "`enabled: true` exige `repo.url` no vacía" (contracts/config.md). Agregar el bloque comentado a `internal/config/testdata/` y tests en `internal/config/config_test.go`.
- [ ] T004 [P] Bloque `i18n.es` en el manifiesto: `I18n struct { ES *Text }` con `Text { Summary, Description string }` en `internal/manifest/types.go`, y en `schema/manifest.v1.schema.json` con `additionalProperties: false`: "solo la clave `es` en esta fase". `summary` y `description` toman las mismas reglas que los campos de nivel superior. Nombres reservados: el esquema y `internal/manifest/semantic.go` rechazan `name: assets` y `name: es` (research D3). Tests en `internal/manifest/load_test.go` y `semantic_test.go`, y ejemplo en `internal/manifest/examples_test.go`.
- [ ] T005 [P] Etapa nueva `Site Stage = "site"` en `internal/report/errors.go`, con su prueba de formato en `internal/report/errors_test.go`: `debfactory: - - -: site: <motivo>`.

**Checkpoint**: la herramienta acepta los bloques `site` e `i18n.es` y conoce la etapa `site`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: modelo (`site.json`), verificación contra los índices, render base y CLI. Todas las
historias lo necesitan.

**⚠️ CRITICAL**: ninguna historia empieza antes de terminar esta fase.

- [ ] T006 Modelo en `internal/site/model.go` según data-model.md §1: tipos `Model`, `Repo`, `Suite`, `Keyring`, `Command`, `Package`, `Version` con las etiquetas JSON del ejemplo y `schema: 1`. Función `Load(cfg *config.Config, root string, st *repo.State, gen *repo.GenInfo) (*Model, error)`, que:
  - recibe el **estado de referencia** (data-model §1), no lo lee de `root`: en CI lo entrega la ejecución, aunque venga de un PR de estado sin integrar; exige que `st.SHA256()` sea `gen.StateSHA256`; si no, falla con la etapa `site`, porque el estado no describe la generación vigente;
  - lee los manifiestos `packages/<p>.yaml`;
  - incluye solo los paquetes con al menos una versión en el estado, ordenados por `name`;
  - ordena las versiones "de mayor a menor (comparación de Debian)" con `watch.Compare` de `internal/watch/release.go`;
  - toma `published` del estado como "fecha UTC `YYYY-MM-DD`";
  - ordena las arquitecturas "como `repo.architectures`" y `excluded[]` "por el orden de `repo.suites`";
  - divide `description` en "lista de párrafos (separados por línea en blanco en el manifiesto), en texto plano", y deja `es: null` si no hay `i18n.es`;
  - con un paquete publicado sin manifiesto, lo incluye sin descripción y escribe un aviso en el log (research D2).
- [ ] T007 [P] Distribución por suite en `internal/site/model.go`: tabla fija "bookworm, trixie y sid → debian; noble y resolute → ubuntu"; una suite desconocida hace fallar `Load`. Fingerprints de `repo.signing.masters` "40 hex agrupados de 4 en 4 con un espacio doble en medio, como los muestra `gpg`". `upstream` es la versión sin `-0~tepacheN`. `url` es "`https://`, sin `/` final".
- [ ] T008 Verificación en `internal/site/verify.go` (data-model §1, invariante 1): `Verify(m *Model, genDir string) error` lee `dists/<suite>/main/binary-<arch>/Packages` de la generación vigente y compara en los dos sentidos el conjunto `(paquete, versión, suite, arquitectura)` del modelo con el de los índices, para los paquetes del estado. Cada diferencia es un error de la etapa `site` con el formato de contracts/cli.md: "el estado la publica en trixie/arm64 pero no está en dists/trixie/main/binary-arm64/Packages".
- [ ] T009 [P] Tests del modelo en `internal/site/model_test.go`, con un `<data>` y un `root` de prueba construidos con las utilidades de `internal/repo` (`internal/testutil`):
  - orden de paquetes y versiones;
  - `bob` sin `bookworm` y con el motivo;
  - una versión retirada no aparece (FR-006);
  - un paquete con manifiesto pero sin publicar no aparece;
  - `es: null` sin `i18n.es`;
  - estado con SHA distinto al de la generación → error `site`;
  - estado de referencia más nuevo que el `repo/state.yaml` de `root` (PR de estado pendiente) → se usa el de referencia y no falla;
  - nombre reservado → error.
- [ ] T010 [P] Tests de `Verify` en `internal/site/verify_test.go`: coherente → nil. Falta en `Packages` → error. Sobra en `Packages` → error. Mensaje con suite, arquitectura y ruta.
- [ ] T011 Traducciones en `internal/site/i18n.go`, con `internal/site/i18n/en.yaml` y `es.yaml`: claves para títulos, etiquetas, la explicación del repositorio, los pasos y los textos del pie (contracts/pages.md). `LoadI18n()` falla si los dos archivos no tienen exactamente las mismas claves o si alguna está vacía (research D7). Test en `internal/site/i18n_test.go`.
- [ ] T012 Render base en `internal/site/render.go`:
  - `Build(m *Model, out string) error` renderiza con `html/template` desde `templates/` embebido, en un directorio temporal hermano de `out` y con `os.Rename` al terminar; `out` debe no existir o estar vacío (data-model §2);
  - `Digest(dir string) (string, error)`: SHA-256 del árbol (rutas y contenido, en orden), el que usan el nombre `<data>/site/<digest>/` y el registro de subida (data-model §3);
  - el CSS se escribe como `assets/site.<sha256-12>.css`;
  - funciones de plantilla para el idioma, la ruta al otro idioma, las rutas relativas y las fechas;
  - deja de guardia "alguna ruta empieza por `dists/` o `pool/`, o coincide con un archivo de la raíz del repositorio" (FR-023) y el presupuesto "alguna página supera 100 KB sumando su CSS" (FR-022): cualquiera de los dos hace fallar `Build`;
  - escribe `site.json` en la raíz del árbol;
  - sin fecha de generación ni nada dependiente del reloj (research D12).
- [ ] T013 Plantilla base en `internal/site/templates/base.html` (contracts/pages.md § Comunes):
  - `<html lang>` y `<link rel="alternate" hreflang>`;
  - enlace visible al otro idioma con su nombre en ese idioma;
  - enlace "saltar al contenido", `<main>` y un solo `<h1>` por página (lo pone cada plantilla);
  - logotipo SVG en línea con `aria-hidden` y texto equivalente;
  - pie con el enlace a `opentepache/packages` (de `ci.repository`) y el serial;
  - un solo `<link rel="stylesheet">`, sin scripts.
- [ ] T014 Hoja de estilos en `internal/site/assets/site.css` (research D11), con la guía de diseño de interfaces (skill `frontend-design`):
  - tipografía del sistema y modo oscuro con `prefers-color-scheme`;
  - contraste AA en los dos modos;
  - bloques `pre` con desplazamiento horizontal propio y sin desplazamiento del documento a 320 px (FR-020);
  - foco visible.
  - Objetivo de menos de 15 KB.
- [ ] T015 Órdenes `site data | build | serve` en `cmd/debfactory/site.go`, registradas en el despacho de `cmd/debfactory/main.go` (contracts/cli.md):
  - `data --root --data [--state FILE] [--out]` lee el estado de `--state` (por defecto `<root>/repo/state.yaml`) con `repo.ReadState`, usa `repo.Current`, `site.Load` y `site.Verify`, y escribe JSON indentado y determinista;
  - `build --in --out` llama a `site.Build`;
  - `serve --dir [--addr 127.0.0.1:8080]` usa `http.FileServer`, que resuelve `index.html`, y rechaza direcciones que no sean de loopback;
  - códigos de salida 0, 1 y 2 según el contrato.
  - Tests en `cmd/debfactory/main_test.go`: uso incorrecto → 2, y `serve` con `0.0.0.0` → 2.

**Checkpoint**: `debfactory site data | debfactory site build` produce un árbol (vacío de páginas
salvo la base) y el modelo está verificado.

---

## Phase 3: User Story 1 - Agregar el repositorio desde la portada (Priority: P1) 🎯 MVP

**Goal**: la portada explica el repositorio, da los comandos de Debian y Ubuntu, el fingerprint,
cómo quitarlo y la lista de paquetes.

**Independent Test**: los bloques de la portada, extraídos del HTML generado, funcionan tal cual
en contenedores limpios de las 5 suites × 2 arquitecturas (SC-002).

### Tests for User Story 1

- [ ] T016 [P] [US1] Tests de comandos en `internal/site/commands_test.go`:
  - los scripts usan `repo.url`, el keyring de la configuración y `VERSION_CODENAME` de `/etc/os-release`;
  - no llevan prompts (`$`);
  - `remove` borra el `.sources` y el keyring.
- [ ] T017 [P] [US1] Test E2E `TestE2ESite` en `internal/repotest/site_test.go` (tag `e2e`, agregado a la receta `e2e` del `justfile`):
  - genera el sitio del árbol de prueba y lo sirve junto al repositorio con Caddy, como `server.go`;
  - extrae los bloques `data-debfactory-step` del HTML con un extractor nuevo en `internal/repotest/guide.go`, que reutiliza el mecanismo de `Blocks`;
  - ejecuta `add-debian` o `add-ubuntu` según la suite, `verify-key`, `apt install` de **todos** los paquetes de la portada publicados en esa suite (respetando exclusiones como `bob` en bookworm) y `remove`, tal cual, en contenedores limpios de bookworm, trixie, sid, noble y resolute, amd64 y arm64 (SC-002: "cualquier paquete listado");
  - exige `apt update` sin advertencias de firma: 10 de 10.

### Implementation for User Story 1

- [ ] T018 [US1] Comandos en `internal/site/commands.go`: genera `commands.debian`, `commands.ubuntu`, `commands.verify` y `commands.remove` (data-model §1) desde la configuración, con el mismo procedimiento deb822 con `Signed-By` de `docs/usuario.md` del repositorio de paquetes (research D8). `Load` los incluye en el modelo.
- [ ] T019 [US1] Plantilla de portada en `internal/site/templates/index.html` (contracts/pages.md § Portada), en este orden: qué es OpenTepache; agregar el repositorio (un bloque por distribución, con sus suites); verificar la llave (fingerprints y `verify-key`); tabla de paquetes con nombre enlazado, resumen y versión upstream actual; quitar el repositorio. Cada bloque va en `<pre><code data-debfactory-step="…">`. `Build` la escribe en `index.html` (inglés) y la repetición en español se agrega en US4.
- [ ] T020 [US1] Tests de render de la portada en `internal/site/render_test.go`:
  - el fingerprint coincide con `repo.signing.masters`;
  - cada paquete del modelo aparece con su versión actual y un enlace a `/packages/<p>/`;
  - con el repositorio vacío, la portada se genera con la tabla vacía y los comandos (edge case);
  - escape: un `summary` con `<script>` sale como texto (FR-010).
- [ ] T021 [US1] Receta `just site` en `justfile`: genera el repositorio de prueba local si falta, después `site data` y `site build` en `dist/site/`, e imprime la ruta. Receta `just site-serve` (contracts/cli.md § just).

**Checkpoint**: `just site-serve` muestra la portada en inglés y `TestE2ESite` pasa en las 10
combinaciones.

---

## Phase 4: User Story 2 - Conocer un paquete antes de instalarlo (Priority: P2)

**Goal**: una página por paquete publicado con todos sus datos y versiones anteriores.

**Independent Test**: para cada paquete del estado de prueba, su página muestra exactamente los
datos del manifiesto y del estado (SC-004).

### Tests for User Story 2

- [ ] T022 [P] [US2] Tests en `internal/site/render_test.go` (`TestPackagePage`):
  - por cada paquete del modelo existe `packages/<p>/index.html`, con descripción en párrafos, versión upstream y versión Debian, fecha, licencia SPDX, enlace a `homepage` con `rel="noopener"` y `sudo apt install <p>`;
  - la tabla de suites × arquitecturas es la de la versión actual;
  - `bob` muestra solo trixie, sid, noble y resolute, más el motivo de bookworm;
  - las versiones anteriores, de la más reciente a la más antigua, llevan `sudo apt install <p>=<versión>`;
  - una versión retirada no aparece;
  - un paquete sin manifiesto se genera sin descripción.

### Implementation for User Story 2

- [ ] T023 [US2] Plantilla de paquete en `internal/site/templates/package.html` (contracts/pages.md § Página de paquete), en este orden: `<h1>` con nombre y resumen; descripción; datos; instalar, con una nota que enlaza a la portada; disponibilidad y suites excluidas; versiones anteriores, solo si hay. `Build` escribe una por paquete en `packages/<p>/index.html`.
- [ ] T024 [US2] Guardia de rutas y nombres en `internal/site/render.go`: `packages/<p>/` solo para nombres válidos del manifiesto, ya filtrados por T004. Test de que un nombre `assets` o `es` que llegue al modelo hace fallar `Build`, para cubrir un estado importado sin pasar por el manifiesto.

**Checkpoint**: cada paquete publicado tiene su página en inglés con datos idénticos al estado.

---

## Phase 5: User Story 3 - El sitio se mantiene solo (Priority: P2)

**Goal**: cada publicación, retiro o renovación regenera y sube el sitio. Un fallo se notifica,
deja el sitio anterior y no afecta a los paquetes.

**Independent Test**: en el entorno de integración (`citest`), una publicación sube el sitio con
la versión nueva; una segunda ejecución sin cambios no sube nada; un fallo de subida notifica
`site` y la siguiente ejecución repara.

### Tests for User Story 3

- [ ] T025 [P] [US3] Tests de la subida en `internal/sync/site_test.go`, con un destino local (directorio) como los de `internal/sync`:
  - orden `assets/` → páginas y `site.json` → borrado diferido;
  - `Cache-Control` inmutable para `assets/` y mutable para lo demás;
  - un árbol con el mismo `digest` no sube nada;
  - un paquete que desaparece se borra en la sincronización siguiente a la que deja de estar (`previous_files − (actual ∪ files)`);
  - el registro solo se escribe al terminar las páginas.
- [ ] T026 [P] [US3] Tests de integración en `internal/ci/site_test.go` (tag `integration`, con `citest`):
  - `ci main` con publicación deja el sitio en el destino de prueba con la versión nueva y el log `site: serial N, …`;
  - `ci main` sin nada que publicar ejecuta el paso y, si el sitio no cambió, el log dice `site: serial N sin cambios; nada que subir`;
  - `ci retire` quita la versión del sitio;
  - `ci refresh` repara un sitio atrasado;
  - un destino que falla solo en el paso `site` notifica `fallo: - - en site` con el motivo, deja el registro y el estado de paquetes intactos y termina con código 1 después de abrir el PR de estado.

### Implementation for User Story 3

- [ ] T027 [US3] Subida del sitio en `internal/sync/site.go` (research D5, D6; data-model §3):
  - `SyncSite(ctx, e *repo.Env, destName, siteDir string) (*SiteRecord, error)` reutiliza el rclone y los destinos de `internal/sync/rclone.go`;
  - sube por fases `assets/`, luego páginas y `site.json`, luego el borrado diferido;
  - el registro `<data>/sync/<dest>.site.json` lleva los campos `schema`, `serial`, `digest`, `files`, `previous_files` y `time`, y se escribe solo "cuando termina la subida de las páginas";
  - el `digest` es el SHA-256 del árbol (rutas y contenido, en orden), y si coincide no se sube nada.
- [ ] T028 [US3] Paso `site` en `internal/ci/site.go`: `(x *run) site(ctx) error` se llama al final de `ci main` (`internal/ci/run.go`, después de la sincronización de la línea `reposync.Sync` y del PR de estado), de `ci retire` (`internal/ci/manual.go`) y de `ci refresh` (`internal/ci/refresh.go`), solo con `site.enabled`. Hace:
  - `site.Load` con el estado de referencia que ya determinó la ejecución (el de `run.go`, no el `repo/state.yaml` del checkout) y `site.Verify` sobre `repo.Current`;
  - `site.Build` en un temporal de `<data>/site/`, renombrado a `<data>/site/<digest>/`; si ya existe ese digest, se reutiliza; se conservan solo el vigente y el anterior;
  - `sync.SyncSite` por cada destino;
  - escribe en el log la línea de contracts/cli.md.

  Ante cualquier error, `x.failErr(ctx, err, report.Site, "", "")`, sin revertir nada; el comando devuelve error después de terminar todo lo demás (FR-013). Corre también cuando no hay nada que publicar (research D5).
- [ ] T029 [US3] Comprobación en `ci doctor` (`internal/ci/doctor.go`): solo con `site.enabled`, `GET <public_url>/` de cada destino con `public_url` responde 200 con `Content-Type: text/html` (research D4). Si no, ❌ con el texto "falta la regla de reescritura de `/` a `/index.html` (docs/operacion-ci.md)". Test en `internal/ci/doctor_test.go` con `httptest`.

- [ ] T029a [US3] Orden `site sync --root --data --dest NAME [--state FILE] [--dry-run]` en `cmd/debfactory/site.go` (contracts/cli.md, FR-015): `site data` + `site build` en `<data>/site/<digest>/` y `sync.SyncSite` al destino `NAME` de `repo.destinations` (p. ej. `r2test`). `--dry-run` imprime qué subiría y borraría. Un destino inexistente → código 2. Test en `cmd/debfactory/main_test.go` con un destino local.

**Checkpoint**: el pipeline mantiene el sitio solo; los fallos se notifican y se reparan.

---

## Phase 6: User Story 4 - Leer el sitio en español (Priority: P3)

**Goal**: cada página existe también bajo `/es/`, con los mismos datos y comandos.

**Independent Test**: cada página tiene su equivalente en el otro idioma, enlazado, y los datos y
comandos son idénticos (SC-004, FR-017).

### Tests for User Story 4

- [ ] T030 [P] [US4] Tests en `internal/site/render_test.go` (`TestLanguages`):
  - por cada página en inglés existe la misma ruta bajo `es/` y al revés;
  - `lang="en"` y `lang="es"`;
  - el enlace visible y los `hreflang` apuntan a páginas generadas;
  - los bloques `data-debfactory-step`, los fingerprints, las versiones, las fechas y las suites son idénticos byte a byte entre idiomas;
  - un paquete sin `i18n.es` muestra en `/es/` la descripción del `.deb` dentro de un elemento con `lang="en"`.

### Implementation for User Story 4

- [ ] T031 [US4] `Build` renderiza cada plantilla dos veces, inglés en la raíz y español bajo `es/` (FR-016), en `internal/site/render.go`. Las plantillas toman los textos de `i18n` y los datos del modelo sin transformarlos (research D7). Agregar `i18n.es` a `bob` en `internal/manifest/testdata` (o en el árbol de prueba de `internal/repotest`) para cubrir los dos casos.
- [ ] T032 [US4] Revisar y completar `internal/site/i18n/es.yaml` (español neutro, RFC 2119 no aplica en el sitio) y `en.yaml` con todos los textos de contracts/pages.md.

**Checkpoint**: el sitio completo existe en los dos idiomas.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T033 [P] Pruebas de calidad en `internal/site/quality_test.go`, sobre el árbol generado con `golang.org/x/net/html` (research D10):
  - un solo `h1` y encabezados sin saltos;
  - `lang` en `<html>` y todo `<img>` con `alt`;
  - ningún `<script>` ni atributo `on*` (FR-018);
  - todo `href`/`src` relativo o de `repo.url`, salvo los `homepage` (FR-021);
  - presupuesto de 100 KB por página con su CSS (FR-022, SC-005);
  - contraste AA de los pares de color declarados en `site.css`, para los dos modos.
- [ ] T034 [P] Determinismo en `internal/site/render_test.go` (`TestDeterministic`): dos `Build` del mismo modelo producen árboles idénticos byte a byte (FR-014). Lo mismo para dos `site data` del mismo estado.
- [ ] T035 [P] Auditoría de accesibilidad E2E en `internal/repotest/a11y_test.go` (tag `e2e`, en la receta `e2e`): `pa11y-ci` (axe) en contenedor con imagen fijada por digest, en una constante con comentario de versión, como las demás imágenes de prueba. Corre sobre el árbol servido por Caddy a 320 px y 1280 px, en todas las páginas de los dos idiomas. Falla con cualquier error A o AA (SC-006) o con desplazamiento horizontal del documento a 320 px (FR-020).
- [ ] T036 [P] Documentación en `docs/operacion-ci.md`, sección nueva "Sitio web":
  - qué es y dónde vive;
  - la regla de reescritura de Cloudflare (quickstart §3), con su condición y su expresión;
  - activar `site.enabled`;
  - `just site` y `just site-serve`;
  - qué hacer ante `fallo: - - en site`.

  En `README.md`, una línea con el sitio.
- [ ] T037 [P] Notas en las specs anteriores: en `specs/002-signed-apt-repo/spec.md` y `specs/003-ci-pipeline/spec.md`, "sitio web" deja de estar fuera de alcance y pasa a la fase 006. En `specs/003-ci-pipeline/contracts/cli.md`, el paso `site` al final de `ci main`, `ci retire` y `ci refresh`.
- [ ] T038 `just check` y `just e2e` en verde. Revisar a mano `just site-serve` con JavaScript desactivado y a 320 px de ancho, y `debfactory site sync --dest r2test` en `https://apt-test.opentepache.org/` (quickstart §1).
- [ ] T039 **(manual)** Regla de reescritura en Cloudflare para `apt.opentepache.org` y el host de pruebas (quickstart §3). Antes de subir el sitio, `/` sigue en 404 porque `index.html` aún no existe. La comprobación de verdad es la de T041, con el sitio ya subido.
- [ ] T040 `just tool-install`; PR en `packages` que fija la herramienta nueva y agrega `site: {enabled: true, name: OpenTepache}` a `config/debfactory.yaml`, opcionalmente con `i18n.es` en los manifiestos, y que hace que `docs/usuario.md` enlace la portada. **(manual)** el push y el merge.
- [ ] T041 **(manual)** Quickstart §3–§4 en producción:
  - `curl -sI https://apt.opentepache.org/` y `/es/` responden 200;
  - `site.json` tiene el serial vigente;
  - `ci doctor` sin ❌ del sitio;
  - `apt update` en una máquina con el repositorio sigue igual;
  - en la siguiente publicación real, la versión aparece en el sitio en menos de 10 minutos (SC-001).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (fase 1)**: sin dependencias. T002–T005 en paralelo tras T001.
- **Foundational (fase 2)**: tras la fase 1. T006 antes que T007–T010. T011 antes que T012–T013. T015 al final de la fase.
- **US1 (fase 3)**: tras la fase 2. Es el MVP.
- **US2 (fase 4)**: tras la fase 2. Independiente de US1 salvo por compartir `render_test.go` (hacerlo en orden).
- **US3 (fase 5)**: tras la fase 2. T027 antes que T028 y T029a. Se puede hacer en paralelo a US1 y US2.
- **US4 (fase 6)**: tras US1 y US2, porque duplica sus plantillas por idioma.
- **Polish (fase 7)**: T033–T037 en cuanto existan las páginas. T038 tras todo el código. T039 puede hacerse en cualquier momento. T040–T041 al final.

### User Story Dependencies

```text
Fase 1 → Fase 2 ─┬─ US1 (P1) ─┐
                 ├─ US2 (P2) ─┼─ US4 (P3) ─ Polish
                 └─ US3 (P2) ─┘
```

### Parallel Opportunities

- Fase 1: T002, T003, T004 y T005, en archivos distintos.
- Fase 2: T009 y T010, cuando existen T006 y T008.
- Fase 3 y fase 5 entre sí. Dentro de US3, T025 y T026 en paralelo.
- Polish: T033, T034, T035, T036 y T037.

---

## Parallel Example: User Story 3

```text
Task: "Tests de la subida en internal/sync/site_test.go"
Task: "Tests de integración en internal/ci/site_test.go"
# luego, en orden:
Task: "Subida del sitio en internal/sync/site.go"
Task: "Paso site en internal/ci/site.go"
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Fases 1 y 2.
2. US1: portada en inglés con comandos probados en las 10 combinaciones.
3. **Parar y validar** con `just site-serve` y `TestE2ESite`.

### Incremental Delivery

1. MVP (US1).
2. US2: páginas de paquete.
3. US3: el pipeline lo publica solo. A partir de aquí se puede activar en producción (T039–T041)
   aunque falte el español.
4. US4: español.
5. Polish y activación.
