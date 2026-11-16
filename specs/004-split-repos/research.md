# Research: Separar la herramienta y los paquetes en dos repositorios

Decisiones de diseño de la fase 004. Parte de la fase 003 implementada (commit `bf077e1`) y de lo
que sus spikes enseñaron (research 003 §3). Las referencias "003 Dn" apuntan a
[specs/003-ci-pipeline/research.md](../003-ci-pipeline/research.md).

## 1. Decisiones

### D1. Dos repositorios en `opentepache`, uno junto al otro en la PC

- **Decision**:
  - `opentepache/debfactory`: **privado**, el repositorio actual. Herramienta, imagen del
    runner, host, pruebas, specs y documentación de operación.
  - `opentepache/packages`: **público**, nuevo. `packages/`, `config/`, `repo/`,
    `docs/usuario.md`, `.github/workflows/`, `debfactory.version` y un `README.md` para
    contribuyentes.
  - En la PC, los dos clones viven uno junto al otro: `~/Develop/debfactory` y
    `~/Develop/packages`. El `justfile` de la herramienta pasa `--root` a debfactory con el
    valor de la variable `root`, que por defecto es `../packages`.
- **Rationale**:
  - En GitHub Free los rulesets solo existen en repositorios públicos (003 D1: la API responde
    403 en privados). El repositorio cuyo merge publica es el que necesita la protección.
  - debfactory solo lee del repositorio `config/`, `packages/`, `repo/` y `docs/usuario.md`
    (constantes `config.MainFile`, `config.LicensesFile`, `repo.StateFile`, `ci.AuditFile`,
    `repo.GuideFile`). Todo eso va a `packages`, y nada del código depende de que esté en el
    mismo repositorio que el código: ya se pasa `--root`.
  - `config/licenses.yaml` (allowlist de licencias) va a `packages`: es política del
    repositorio APT, y la constitución (X) exige que cambie solo por PR revisado.
- **Alternatives considered**:
  - **Submódulo de git** con la herramienta dentro de `packages`: obligaría a dar acceso de
    lectura al repositorio privado en cada checkout, contra FR-010.
  - **Un repositorio por paquete**: fuera de alcance (spec), rompe el único escritor.

### D2. Versión fijada: `debfactory.version` en la raíz de `packages`

- **Decision**: un archivo de una línea con el commit completo (40 hex) de
  `opentepache/debfactory`. Cambia solo por PR.
- **Rationale**:
  - Lo lee el lanzador del runner (D3), un script `sh`, antes de que se ejecute ningún binario.
    Leer YAML desde `sh` sería frágil; una línea con un hash se valida con un `case`.
  - Fuera de `config/` para que el diff de un PR que solo sube la herramienta sea de una línea.
- **Alternatives considered**: `tool.commit` dentro de `config/debfactory.yaml` (lo tendría que
  leer el propio binario que se quiere elegir); etiquetas semver (el mantenedor no publica
  versiones de la herramienta y un commit identifica el código sin ambigüedad).

### D3. Distribución: `/srv/debfactory/tools/<commit>/debfactory`, montado de solo lectura

- **Decision**:
  - El mantenedor instala cada versión compilada en `/srv/debfactory/tools/<commit>/debfactory`,
    de `root:root` (directorios `755`, binario `755`).
  - `run-runner.sh` monta `/srv/debfactory/tools` en **los dos** runners, en
    `/opt/debfactory/tools`, `readonly`.
  - La imagen del runner incluye un lanzador `/usr/local/bin/debfactory` (`sh`): lee
    `debfactory.version` del directorio de trabajo, valida el formato, comprueba que existe
    `/opt/debfactory/tools/<commit>/debfactory` y hace `exec` con los mismos argumentos. Si no
    existe, sale con código 2 y un mensaje que nombra la versión pedida, las instaladas y la
    orden para instalarla (FR-005, SC-004).
  - Varias versiones conviven (FR-006). Se borran a mano cuando ni `main` ni un PR abierto las
    fijan; `ci doctor` lista las que sobran.
- **Rationale**:
  - **Sin ventana de fallos (SC-005)**: instalar la versión nueva no quita la anterior. `main`
    sigue usando la que fija hasta que se integra el PR que sube la versión.
  - **Sin reconstruir la imagen**: la imagen cambia solo al actualizar el runner de GitHub (003
    D5). Subir la herramienta es instalar un archivo, no recargar imágenes por ID en dos Docker.
  - **Misma protección que la imagen**: el directorio es de root y el montaje es de solo
    lectura; ni `dfci` ni `dfpub` ni un job pueden cambiar un binario. Como la imagen por ID
    (003 D5), un job no puede sustituir la herramienta.
  - **Sin credenciales (FR-010)**: el binario llega por el sistema de archivos de la PC. Ningún
    job habla con el repositorio privado.
  - Ajusta la redacción de FR-010, que decía "dentro de la imagen": lo que importa es que llega
    compilado desde la PC y que el job no puede modificarlo. Se actualiza la spec.
- **Alternatives considered**:
  - **Binario dentro de la imagen**: una versión por imagen. Para tener dos habría que construir
    imágenes con varias, y cada subida obligaría a recargar las imágenes y cambiar sus IDs en
    `/etc/default/debfactory-runner`.
  - **Compilar en el job desde el repositorio privado**: necesita credenciales de lectura en el
    job, prohibido por FR-010.
  - **Descargar un binario publicado (release)**: el repositorio es privado; necesitaría
    credenciales, y publicarlo sería hacer pública la herramienta.

### D4. Identidad de la versión: commit embebido y autocomprobación

- **Decision**:
  - Paquete nuevo `internal/version` con `var Commit = "dev"`, que la compilación de
    instalación fija con `-ldflags "-X debfactory/internal/version.Commit=<sha>"`, además de
    `-trimpath` (sin rutas de la PC en el binario).
  - Orden `debfactory version`: imprime el commit.
  - Con `GITHUB_ACTIONS=true`, toda orden `ci` comprueba al empezar que `version.Commit` es
    exactamente el contenido de `<root>/debfactory.version`. Si no, falla en la etapa nueva
    `tool`, código 2. Es la segunda barrera tras el lanzador (FR-005).
- **Rationale**: el lanzador elige por nombre de directorio. Si alguien instalara un binario en
  el directorio equivocado, la autocomprobación lo detecta.

### D5. Instalar una versión: `just tool-install` (FR-007, FR-007a)

- **Decision**: receta nueva en el `justfile` de la herramienta, en este orden:
  1. Árbol limpio: `git status --porcelain` vacío. Si no, se niega (FR-007).
  2. Todas las pruebas: `just check` y `just e2e`, con `root` apuntando a los datos de prueba
     propios (D9). Si alguna falla, se niega (FR-007a).
  3. `go build -trimpath -ldflags "-X …Commit=$(git rev-parse HEAD)"`.
  4. `sudo install -D -m 755 -o root -g root` en `/srv/debfactory/tools/<commit>/debfactory`.
  5. Imprime el commit y la línea para `debfactory.version`.
- **Rationale**: es la opción A de la clarificación. No hay CI en el repositorio privado. La
  segunda barrera es el check del PR que fija la versión en `packages` (D6).
- **Alternatives considered**: las opciones B y C de la clarificación (runners de GitHub en el
  privado; runner de la PC registrado en los dos repositorios).

### D6. Subir la versión dispara la comparación; desaparece la etapa `code`

- **Decision**:
  - `ci.code_paths` pasa a valer `[config/, debfactory.version]`: un cambio en cualquiera
    reconstruye y compara todo lo publicado (FR-008 de 004, FR-010 de 003), tanto en el check
    del PR como en el paso 3b de `ci main` (`last_main_sha..HEAD`).
  - La etapa `code` (gofmt, vet y pruebas de Go) desaparece de `ci pr` y `ci main`: en
    `packages` no hay código Go. Su papel lo cumple `just tool-install` (D5).
  - Si el PR cambia `debfactory.version`, el resumen del check incluye el enlace
    `https://github.com/<ci.tool_repository>/compare/<anterior>...<nueva>` (FR-009). Solo el
    mantenedor puede abrirlo; para los demás da 404, que es lo esperado en un privado.
  - Configuración nueva `ci.tool_repository: opentepache/debfactory`, solo para ese enlace.
- **Rationale**: mantiene el mecanismo de la fase 003 (`Classify`, `compare`) y solo cambia qué
  rutas lo disparan.

### D7. Auditoría: campo `tool`

- **Decision**: las entradas nuevas llevan `"tool": "<commit de 40 hex>"` justo después de
  `commit`. `ParseAudit` lo acepta opcional (las anteriores no lo tienen); `CheckAudit` exige que
  sea un hash válido si está; toda entrada que escribe `ci` lo incluye con `version.Commit`.
- **Rationale**: FR-014 y FR-015. El registro sigue siendo de solo añadir. Hoy `repo/audit.jsonl`
  todavía no existe en `main` (la fase 003 no ha publicado nada en producción), así que en la
  práctica todas las entradas, incluidas las `import` de la primera ejecución, llevarán `tool`.

### D8. Migración

- **Decision**:
  - **`packages`**: `git filter-repo` (paquete `git-filter-repo` de Debian) sobre un clon nuevo
    del repositorio actual, conservando `packages/`, `config/`, `repo/`, `docs/usuario.md` y
    `.github/`. Encima, un commit con `debfactory.version`, `README.md`, `LICENSE` y los
    workflows adaptados (D11). Licencia del repositorio público: **MIT** (decisión del mantenedor,
    2026-11-16). **Se conserva la autoría** de los commits, correo incluido
    (decisión del mantenedor, 2026-11-16: le sirve como referencia pública de su trabajo; el
    correo ya es público en el campo `Maintainer` de cada `.deb`).
  - **`debfactory`**: sin reescribir la historia. Un commit borra lo que pasa a `packages` y crea
    los datos de prueba propios (D9). La historia anterior se queda, y es privada.
  - **Orden**: (1) instalar la versión de la herramienta que va a fijar `packages`; (2) crear y
    subir `packages` con su configuración de GitHub; (3) `setup.sh --repository
    opentepache/packages`; (4) `ci doctor` en verde; (5) arrancar los runners. El estado
    publicado no se toca en ningún paso (FR-017).
- **Rationale**: `filter-repo` es la herramienta recomendada por git para esto y está en trixie
  (IX). No reescribir el privado evita perder referencias de commits (las de la fase 003 citan
  hashes).

### D9. Pruebas de la herramienta: `testdata/project/`

- **Decision**: una copia congelada del contenido de `packages` en el momento de la migración:
  `testdata/project/{config,packages,repo,docs}`. Las pruebas que hoy usan la raíz del
  repositorio (`config.Load("../..")`, `repo.ReadState("../..")`, la e2e, `newMainFixture`,
  `examples_test`) pasan a usar esa ruta mediante una constante compartida. `just check` y
  `just e2e` usan `--root testdata/project`.
- **Rationale**: FR-019. Una copia congelada hace las pruebas deterministas: no cambian cuando
  cambia un manifiesto real. Las pruebas de reproducibilidad (`build --all
  --check-reproducible`) siguen construyendo paquetes reales, los de la copia.

### D10. Contribuyentes sin la herramienta: el mantenedor genera los lockfiles

- **Decision**: mientras la herramienta sea privada, un contribuyente externo propone el cambio
  del manifiesto y el mantenedor ejecuta `lock` y lo sube a la rama del PR (GitHub permite al
  mantenedor escribir en la rama de un fork con "Allow edits by maintainers"). El check falla
  hasta entonces en la coherencia del lockfile, con el mensaje de siempre. Lo explica el
  `README.md` de `packages`.
- **Rationale**: los lockfiles los escribe una persona (spec 003, Assumptions) y la constitución
  impide que el pipeline los escriba. Es una limitación conocida que desaparece al publicar la
  herramienta.

### D11. Imagen y workflows

- **Decision**:
  - La imagen pierde Go (`golang-1.26-go`, `GOTOOLCHAIN`, `GOFLAGS`): los jobs ya no compilan.
    Gana el lanzador (D3).
  - `pr.yml` y `main.yml` se mueven a `packages`, sin el paso `go build`. Llaman a `debfactory`
    (el lanzador).
  - `trusted-gate.sh` no cambia: ya compara `GITHUB_REPOSITORY` con `DEBFACTORY_REPOSITORY`
    (FR-012). Pasa a valer `opentepache/packages`.
- **Rationale**: menos superficie en la imagen y jobs más rápidos (sin descargar módulos).

### D12. `ci doctor` sin `ci/runner/runner.env` en la raíz

- **Decision**: la versión del runner se embebe en el binario (`go:embed` de `runner.env` en
  un paquete `ci/runner`), porque `--root` ya no es el repositorio de la herramienta. Nuevas
  comprobaciones del host: `/srv/debfactory/tools` es de root y nadie más puede escribir; la
  versión que fija `packages` está instalada; lista de versiones instaladas que nada fija.
- **Rationale**: `doctor` lo ejecuta el mantenedor con `--root ../packages`.

## 2. Configuración de GitHub tras la fase

| Repositorio | Visibilidad | Ruleset en `main` | Forks | Runners | Workflows |
|---|---|---|---|---|---|
| `opentepache/debfactory` | privado | no (Free no lo permite) | — | ninguno | ninguno |
| `opentepache/packages` | público | PR, sin force push, sin borrado, sin bypass | aprobación para todos los externos | `debfactory-pr`, `debfactory-main` | `pr.yml`, `main.yml` |

El PAT fine-grained de los runners pasa a tener acceso solo a `opentepache/packages`.

## 3. Riesgos

1. **Pruebas que dependen de la raíz del repositorio** (unas 20 llamadas a `"../.."`): el cambio
   a `testdata/project` es mecánico, pero hay que verificar que todas pasan en un clon limpio sin
   `packages` (SC-008). Se ataca primero.
2. **`git filter-repo` y los commits que tocan archivos de los dos lados**: quedan partidos; el
   mensaje se conserva en los dos. Se revisa el `git log` resultante antes de subirlo.
3. **Primer `ci main` en `packages`**: debe reconocer que todo está publicado (SC-002). Se
   prueba antes con `ci plan` sobre el clon de `packages`.

### Resultados (2026-11-16, `/speckit-implement`)

- **Riesgo 1, cerrado (T010).** En una copia del árbol sin `config/`, `packages/`, `repo/`,
  `docs/usuario.md` ni `.github/` (como quedará el repositorio de la herramienta tras T037):
  `go vet` con los tres tags, `go test ./...`, `go test -tags integration ./...` (38 paquetes) y
  `just check` pasan. `just check` (413 s) construye los 4 paquetes de `testdata/project` de forma
  reproducible y los prueba en las 5 suites y las dos arquitecturas; los 8 `.deb` tienen los
  mismos sha256 que los publicados (`repo/state.yaml`). Se hizo con una copia del árbol de
  trabajo, no con un `git clone`, porque los cambios aún no tienen commit; T040 lo repite con un
  clon desde GitHub. La e2e (`just e2e`) no se ejecutó en esta pasada; la ejecuta
  `just tool-install` (T035).
- **Riesgos 2 y 3**: pendientes de T021 (spike del lanzador con `dfci`) y T035 (`ci plan` sobre
  el clon filtrado).

