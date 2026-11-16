# debfactory

Fábrica declarativa que reempaqueta herramientas de desarrollo modernas como paquetes `.deb`
para Debian y Ubuntu (amd64 y arm64). Cada herramienta es un archivo de datos; no hay código por
paquete. Las reglas del proyecto están en la [constitución](.specify/memory/constitution.md) y
el diseño de esta fase en [specs/001-manifest-deb-builder/](specs/001-manifest-deb-builder/).

**Dos repositorios** ([specs/004-split-repos](specs/004-split-repos/spec.md)). Este,
`opentepache/debfactory` (privado), tiene la herramienta. Los manifiestos, la configuración, el
estado publicado y los workflows viven en `opentepache/packages` (público), que se clona al lado
en `../packages`: las recetas `just` trabajan sobre él (`DEBFACTORY_ROOT` lo cambia). Las
pruebas usan la copia congelada `testdata/project/`.

## Requisitos

Debian trixie con `trixie-backports`, `golang-1.26`, `just`, `qemu-user-binfmt` y Docker; para
publicar, además `aptly` y `rclone` (`sudo apt install aptly rclone`). La guía
paso a paso está en el [quickstart](specs/001-manifest-deb-builder/quickstart.md). `debfactory`
comprueba Docker y binfmt al empezar y sale con código 3 si falta algo.

## Componentes

**Manifiesto** (`packages/<nombre>.yaml`). Describe una herramienta: de qué release oficial se
descarga cada arquitectura, qué archivos instala y dónde, su licencia esperada, completions, man
pages, relaciones y suites excluidas. Lo valida un JSON Schema
([manifest.v1.schema.json](specs/001-manifest-deb-builder/contracts/manifest.v1.schema.json))
más reglas semánticas; los errores dicen archivo, campo y regla. Ejemplos en
[contracts/examples/](specs/001-manifest-deb-builder/contracts/examples/).
Orden: `debfactory validate [pkg…]`.

**Lockfile** (`packages/<nombre>.lock.yaml`). Guarda, por versión, el sha256 de cada artefacto,
del texto de licencia y del archivo de checksums upstream, y la fecha del release. Solo lo
escribe `lock`, que verifica contra los checksums upstream y pasa el gate de licencias antes de
escribir; el build nunca lo toca. Su diff se revisa en git.
Orden: `debfactory lock <pkg> [--force]` (y `--prune` para borrar versiones viejas).

**Configuración** (`config/`). Lo común a todos los paquetes: mantenedor, suites y sus imágenes,
arquitecturas, etiquetas de lintian que nunca se pueden silenciar, mapa de bibliotecas ELF a
dependencias y la allowlist de licencias
([config.md](specs/001-manifest-deb-builder/contracts/config.md)).

**Caché** (`~/.cache/debfactory/sha256/`). Descargas direccionadas por contenido: un archivo
solo entra en la caché después de verificar su hash, así que lo que hay en ella es válido por
definición. Se puede borrar en cualquier momento.

**Build**. Descarga y verifica, pasa el gate de licencias, ensambla el árbol con permisos y
fechas fijados, calcula `Depends` desde los ELF, genera copyright, changelog y overrides, y
empaqueta con nfpm. Dos builds de la misma versión dan el mismo `.deb` byte a byte. El resultado
va a `dist/` junto a un `build.json`.
Orden: `debfactory build <pkg> [--arch A] [--check-reproducible]` o `debfactory build --all`.

**lintian**. Corre en un contenedor `debian:trixie` sin red; un error no cubierto por un override
declarado en el manifiesto hace fallar el build. Los overrides de seguridad, licencia o copyright
están prohibidos por la configuración.
Orden: `debfactory lint <deb…>` (el build ya lo ejecuta).

**Pruebas de instalación**. Para cada suite no excluida y la arquitectura del `.deb` (arm64 por
qemu): instala con apt, corta la red, ejecuta la orden de comprobación sin shell, verifica la
versión, purga y comprueba que no quedan archivos. Escribe `dist/<…>.test.json`.
Orden: `debfactory test <pkg> [--arch A] [--suite S]` o `debfactory test --all`.

**Repositorio APT**. Publica los `.deb` ya construidos y probados en un repositorio firmado
(suites bookworm, trixie, sid, noble y resolute; componente `main`; amd64 y arm64). La fuente de
verdad es `repo/state.yaml`, en git: cada publicación o retiro genera con aptly un árbol completo
y firmado, lo verifica y lo expone de golpe, así que nunca queda a medias. Conserva las últimas 3
versiones de cada paquete para que apt pueda volver atrás. `repo sync` lo sube con rclone a un
directorio local, a R2 o a cualquier S3 o VPS, en un orden que nunca deja índices apuntando a
archivos que faltan. La llave maestra vive fuera de línea; la PC solo tiene una subllave de firma
([operación de la llave](docs/operacion-llave.md)). Para el usuario final:
[guía de usuario](docs/usuario.md).
Órdenes: `debfactory publish <pkg> [version]`, `debfactory retire <pkg> <version> --reason …`,
`debfactory repo sync <destino>`, `debfactory repo verify [--dest D]`, `debfactory repo rebuild`,
`debfactory repo status` ([cli.md](specs/002-signed-apt-repo/contracts/cli.md)).

**Pipeline**. GitHub Actions con dos runners efímeros en la PC: `debfactory-pr` (usuario `dfci`,
sin secretos ni red local) prueba cada pull request con los gates 1–6, y `debfactory-main`
(usuario `dfpub`) publica lo que llega a `main`: concilia los manifiestos con el estado
publicado, construye y prueba solo lo que falta, publica, sincroniza, verifica, deja la entrada
en `repo/audit.jsonl` y abre un PR de estado que integra el mantenedor. Los fallos abren issues;
cada semana se vuelven a firmar los índices (`Valid-Until` de 30 días). Desde que está en marcha
es el único que escribe en el repositorio público. Los jobs no compilan: un lanzador de la
imagen ejecuta la versión de la herramienta que fija `debfactory.version` en `packages`, que el
mantenedor instala con `just tool-install` (pruebas en verde, commit embebido).
Órdenes: `debfactory ci main|pr|rebuild|retire|refresh|restore-state-pr|doctor` y
`debfactory version`
([cli.md](specs/003-ci-pipeline/contracts/cli.md); [operación](docs/operacion-ci.md)).

## Atajos

```bash
just lock fnm        # genera el lockfile
just build fnm       # construye amd64 y arm64
just test fnm        # prueba en las 5 suites × 2 arquitecturas
just build-all       # todos los paquetes
just check           # gofmt, vet, tests y build reproducible y pruebas de testdata/project
just tool-install    # instala esta versión para los runners (árbol limpio y todas las pruebas)
just publish fnm     # publica en el repositorio APT local
just sync r2         # sube el repositorio al hosting y lo verifica
```

Las órdenes y sus códigos de salida están en
[cli.md](specs/001-manifest-deb-builder/contracts/cli.md).

## Agregar o actualizar una herramienta

1. Copiar [example.yaml](specs/001-manifest-deb-builder/contracts/examples/example.yaml) a
   `packages/<nombre>.yaml` y ajustarlo. Antes, comprobar que la herramienta no está al día en
   Debian estable, no tiene repositorio APT de su autor y no está en griffo (constitución V).
2. `just lock <nombre> && just build <nombre> && just test <nombre>`.

Para una versión nueva: cambiar `version`, volver `revision` a `1` y repetir el paso 2.
