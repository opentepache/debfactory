# Data Model: Sitio web del repositorio

## 1. Modelo del sitio (`site.json`)

Lo escribe `debfactory site data` (research D1, D2) y lo lee `site build`. El estado del que
sale es el **estado de referencia**: el que describe la generación vigente (su SHA-256 es
`GenInfo.StateSHA256`). En CI lo entrega la ejecución (`internal/ci/run.go`), aunque venga de un PR
de estado sin integrar; en la CLI, `--state FILE` (por defecto `repo/state.yaml`). También se publica en
`/site.json`. Es el único insumo del render: las plantillas no leen ni manifiestos ni estado.

```json
{
  "schema": 1,
  "name": "OpenTepache",
  "url": "https://apt.opentepache.org",
  "serial": 3,
  "repo": {
    "suites": [
      {"name": "bookworm", "distro": "debian"},
      {"name": "trixie",   "distro": "debian"},
      {"name": "sid",      "distro": "debian"},
      {"name": "noble",    "distro": "ubuntu"},
      {"name": "resolute", "distro": "ubuntu"}
    ],
    "architectures": ["amd64", "arm64"],
    "component": "main",
    "keyring": {
      "path": "debfactory.gpg",
      "installed_as": "/etc/apt/keyrings/debfactory.gpg",
      "fingerprints": ["7267 1CD2 2168 4BF7 FDCD  FB39 65B2 7897 5D4F E04B"]
    }
  },
  "commands": {
    "debian":  {"step": "add-debian",  "script": "…"},
    "ubuntu":  {"step": "add-ubuntu",  "script": "…"},
    "verify":  {"step": "verify-key",  "script": "…"},
    "remove":  {"step": "remove",      "script": "…"}
  },
  "packages": [
    {
      "name": "bob",
      "summary": {"en": "version manager for Neovim", "es": null},
      "description": {"en": ["bob installs, switches and removes Neovim versions …"], "es": null},
      "license": "MIT",
      "homepage": "https://github.com/MordechaiHadad/bob",
      "excluded": [{"suite": "bookworm", "reason": "Upstream builds on Ubuntu 24.04 …"}],
      "install": "sudo apt install bob",
      "versions": [
        {
          "version": "4.3.0-0~tepache1",
          "upstream": "4.3.0",
          "published": "2026-11-13",
          "suites": ["trixie", "sid", "noble", "resolute"],
          "architectures": ["amd64", "arm64"],
          "install": "sudo apt install bob=4.3.0-0~tepache1"
        }
      ]
    }
  ]
}
```

| Campo | Origen | Reglas |
|---|---|---|
| `name` | `site.name` (config) | no vacío |
| `url` | `repo.url` (config) | `https://`, sin `/` final |
| `serial` | estado de referencia | el de la generación que describe el sitio |
| `repo.suites[].distro` | `repo.suites` (config) | `debian` o `ubuntu`, según la suite (tabla fija en el código: bookworm, trixie y sid → debian; noble y resolute → ubuntu). Una suite desconocida hace fallar `site data` |
| `repo.keyring.path` | constante de la fase 002 | `debfactory.gpg`, en la raíz del repositorio (como `internal/sync/plan.go` y `docs/usuario.md`) |
| `repo.keyring.installed_as` | constante de la fase 002 | `/etc/apt/keyrings/debfactory.gpg`, la ruta de `docs/usuario.md`; un solo lugar en el código |
| `repo.keyring.fingerprints` | `repo.signing.masters` | 40 hex agrupados de 4 en 4 con un espacio doble en medio, como los muestra `gpg` |
| `commands.*.script` | plantilla de comandos + config | texto exacto que se copia y se prueba (research D8); `step` es el valor de `data-debfactory-step` |
| `packages[]` | estado de referencia ∩ manifiestos | ordenados por `name`; solo paquetes con al menos una versión en el estado |
| `summary`, `description` | manifiesto (`summary`, `description`, `i18n.es`) | `description` es una lista de párrafos (separados por línea en blanco en el manifiesto), en texto plano. `es: null` si no hay traducción |
| `license` | `license.spdx` | expresión SPDX tal cual |
| `homepage` | `homepage` | URL `https://`; la única URL externa del sitio |
| `excluded[]` | `exclude.suites` | ordenadas por el orden de `repo.suites` |
| `versions[]` | estado de referencia (`packages.<p>[]`) | ordenadas de mayor a menor (comparación de Debian); la primera es la actual |
| `versions[].upstream` | versión sin `-0~tepacheN` | para mostrar |
| `versions[].published` | `published` del estado | fecha UTC `YYYY-MM-DD` |
| `versions[].architectures` | claves de `debs` | ordenadas como `repo.architectures` |

**Invariantes** que `site data` comprueba antes de escribir (falla con la etapa `site`):

1. Cada `(paquete, versión, suite, arquitectura)` del modelo aparece en el `Packages` de esa suite
   y arquitectura de la generación vigente, y viceversa para los paquetes del estado (FR-009).
2. Ningún nombre de paquete es `assets` ni `es` (research D3).
3. Los textos de los manifiestos se guardan tal cual. El escape lo hace la plantilla (FR-010), no
   el modelo.

## 2. Árbol generado

`site build` escribe en un directorio que no existe o está vacío el árbol de research D3, por
idioma. En CI es un temporal que se renombra a `<data>/site/<digest>/` (§3), así un cambio que no
cambia el serial no choca con el sitio anterior; en local, el que se indique. `site build` falla si:

- alguna ruta empieza por `dists/` o `pool/`, o coincide con un archivo de la raíz del repositorio
  (FR-023);
- alguna página supera 100 KB sumando su CSS (FR-022).

El directorio se llena entero en una carpeta temporal y se renombra al terminar, para que nunca
quede un árbol a medias.

## 3. Registro de sincronización del sitio

`<data>/sync/<dest>.site.json`, uno por destino (research D5):

```json
{
  "schema": 1,
  "serial": 3,
  "digest": "sha256:…",
  "files": ["index.html", "es/index.html", "assets/site.3f2a….css", "…"],
  "previous_files": ["…"],
  "time": "2026-11-26T05:00:00Z"
}
```

| Campo | Uso |
|---|---|
| `digest` | SHA-256 del árbol (rutas y contenido, en orden). Si coincide con el del sitio recién generado, no se sube nada (US3, escenario 5) |
| `files` / `previous_files` | borrado diferido: se borra `previous_files − (actual ∪ files)`, como en el repositorio |

El registro se escribe solo cuando termina la subida de las páginas. Si algo falla antes, la
siguiente ejecución ve un `digest` distinto y vuelve a subir (FR-013).

## 4. Cambios en entidades existentes

| Entidad | Cambio |
|---|---|
| Manifiesto (`packages/<p>.yaml`) | bloque opcional `i18n.es: {summary, description}` (contracts/manifest.md) |
| Configuración (`config/debfactory.yaml`) | bloque opcional `site: {enabled, name}` (contracts/config.md) |
| Etapas de notificación (`internal/report`) | etapa nueva `site` |
