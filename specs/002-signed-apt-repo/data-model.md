# Data Model: Repositorio APT firmado

Las entidades de la fase 001 (manifiesto, lockfile, `build.json`, `test.json`) no cambian,
salvo un campo nuevo en `test.json` (§6).

## 1. Estado del repositorio — `repo/state.yaml` (en git)

Es la fuente de verdad (FR-035). Solo lo escriben `publish`, `retire` y `repo rebuild`. Formato
exacto en [contracts/state.md](contracts/state.md).

| Campo | Tipo | Reglas |
|---|---|---|
| `schema` | int | `1` |
| `serial` | int | Sube en 1 con cada operación que cambia el estado. Empieza en `0` (repositorio vacío). |
| `packages.<name>[]` | lista de Versión publicada | Ordenada de mayor a menor según `dpkg --compare-versions`. Hay como mucho `repo.keep` entradas por paquete y suite. |
| `retired[]` | lista de Retiro | Solo se agregan entradas; nunca se borran. |

### Versión publicada

| Campo | Tipo | Reglas |
|---|---|---|
| `version` | string | Versión Debian completa, `<upstream>-0~pav<N>`. Única por paquete. |
| `published` | fecha RFC 3339 (UTC) | Momento de la operación. |
| `suites` | lista de suites | Subconjunto ordenado de `config.suites`. Sale de `test.json` (suites con `pass`) y es igual en todas las arquitecturas. |
| `debs.<arch>` | sha256 (64 hex) | Una entrada por cada arquitectura de `config.arches`; no puede faltar ninguna. |

### Retiro

| Campo | Tipo | Reglas |
|---|---|---|
| `package`, `version` | string | Deben haber estado publicados. |
| `date` | fecha RFC 3339 | |
| `reason` | string | No vacío y de una línea. Se toma de `--reason`, que es obligatorio. |

**Invariantes** (las comprueban `repo verify` y cada operación antes de escribir):

- Ninguna `(package, version)` está a la vez en `packages` y en `retired`.
- Un mismo sha256 no aparece con dos nombres de archivo distintos.
- En cada suite, cada paquete tiene como mucho `keep` versiones.

## 2. Generación del árbol — `<data>/public/gen-<serial>/` (fuera de git)

Es el árbol APT completo para un `serial`. `<data>/public/current` es un enlace simbólico a la
generación vigente.

```text
gen-<N>/
├── debfactory.gpg                  # keyring (llaves maestras públicas + subllaves)
├── pool/main/<l>/<name>/<name>_<version>_<arch>.deb
└── dists/<suite>/
    ├── InRelease, Release, Release.gpg
    ├── by-hash/…
    └── main/binary-{amd64,arm64}/{Packages,Packages.gz,Packages.bz2,Release,by-hash/…}
gen-<N>.json                        # fuera del árbol servido
```

`gen-<N>.json` contiene:

- `serial`;
- `state_sha256`, el sha256 de `state.yaml` con el que se generó;
- `files`, la lista de rutas regulares con su sha256 (sin enlaces simbólicos);
- `signing_subkey`;
- `created`.

Transiciones:

```text
(nada) ──generar──▶ gen-N (incompleta) ──verificar──▶ gen-N (verificada) ──swap──▶ current
                         │ fallo                          │ fallo
                         └──────── se borra ◀─────────────┘   (current sigue en N-1)
```

Se conservan `current` y la generación anterior. Las más viejas se borran tras el swap.

## 3. Almacén — `<data>/store/sha256/<aa>/<sha256>.deb` (fuera de git)

Guarda una copia por contenido de cada `.deb` publicado alguna vez. Solo entra un archivo
después de verificar su sha256 contra `build.json` y `test.json`. Se puede vaciar: la
regeneración lo repone desde `dist/` o reconstruyendo (research D5).

## 4. Destino de sincronización

Se define en `config/debfactory.yaml` (`repo.destinations`) y su formato está en
[contracts/config.md](contracts/config.md).

| Campo | Reglas |
|---|---|
| nombre (clave) | `^[a-z][a-z0-9-]*$` |
| `remote` | Ruta de rclone (`/ruta/local`, `r2:bucket`, `vps:/srv/apt`). No contiene credenciales. |
| `public_url` | Opcional. URL con la que los clientes ven ese destino; la usa `repo verify --dest`. |
| `cache_control` | `true` para destinos HTTP con caché (R2) y `false` para el local. |

Registro de la última sincronización: `<data>/sync/<destino>.json`, con `serial` y `files`
(rutas y sha256). Sirve para el borrado diferido de D6.

## 5. Llave de firma (fuera de git, salvo la parte pública)

| Elemento | Dónde | Notas |
|---|---|---|
| Maestra (secreta) | Solo en el respaldo cifrado fuera de línea. | ed25519 `[C]`, sin caducidad. |
| Subllave de firma (secreta) | `<data>/gnupg/` (`700`). | ed25519 `[S]`, caduca a los 2 años. |
| Frase de paso de la subllave | `<data>/gnupg/passphrase` (`600`). | Distinta de la de la maestra. |
| Fingerprints | `config/debfactory.yaml` → `repo.signing`. | Son públicos. |
| Keyring público | `gen-<N>/debfactory.gpg`. | Exportado del llavero dedicado en cada generación. |

Estados de una subllave: `vigente` → (`caducada` | `revocada`). La subllave en uso para firmar
es la única `vigente` que está en `repo.signing.subkey`.

## 6. Cambio en `test.json` (fase 001)

Se añade `deb_sha256`, el sha256 del `.deb` que se instaló en las pruebas. `publish` exige que
coincida con `build.json.sha256` y con el archivo en `dist/` (research D8). Un `test.json` sin
este campo, escrito por una versión anterior, impide publicar hasta volver a ejecutar
`debfactory test`.

## 7. Relaciones

```text
manifiesto + lock ──build──▶ .deb + build.json ──test──▶ test.json
                                   │                        │
                                   └──── publish (D8) ◀─────┘
                                            │
                       state.yaml (git) ◀───┼───▶ store/
                                            ▼
                                    gen-N/ ──swap──▶ current ──sync──▶ destino(s)
```
