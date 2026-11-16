# Contrato: versión fijada, instalación y lanzador

Cubre FR-004 a FR-011 y research D2–D5.

## `debfactory.version` (repositorio `packages`)

Una línea: el commit completo de `opentepache/debfactory`. Ejemplo:

```text
bf077e169e7c201a0f300308deb2759ee5ecad02
```

## `just tool-install` (repositorio `debfactory`)

| Paso | Falla si | Mensaje |
|---|---|---|
| 1. árbol limpio | `git status --porcelain` no está vacío | `tool-install: hay cambios sin commit; la versión no identificaría el código` |
| 2. pruebas | falla `just check` o `just e2e` (con `--root testdata/project`) | la salida de la prueba |
| 3. compilación | `go build` falla | la salida del compilador |
| 4. instalación | `sudo install` falla | la de `install` |

Al terminar imprime:

```text
tool-install: instalada /srv/debfactory/tools/<commit>/debfactory
para usarla, en packages: echo <commit> > debfactory.version  (y abre un PR)
```

Instalar una versión que ya existe la reemplaza solo si el binario es distinto (y lo avisa).

## `debfactory version`

Imprime `version.Commit` y termina con código 0. Sin compilar con `tool-install`, imprime `dev`.

## Lanzador `/usr/local/bin/debfactory` (imagen del runner)

```text
debfactory <args…>
```

1. Lee `./debfactory.version`. Si no existe o el formato no es `^[0-9a-f]{40}$`: código 2,
   `debfactory: debfactory.version falta o no es un commit (40 hex)`.
2. Si no existe `/opt/debfactory/tools/<commit>/debfactory`: código 2, y

   ```text
   debfactory: la versión <commit> no está instalada en el runner
   instaladas: <commit1> <commit2> …
   para instalarla: en el checkout de debfactory, git checkout <commit> && just tool-install
   ```

3. `exec /opt/debfactory/tools/<commit>/debfactory "$@"`.

El lanzador no hace nada más: no lee otros archivos del checkout ni variables de entorno.

## Autocomprobación del binario

Con `GITHUB_ACTIONS=true`, antes de cualquier orden `ci`:

| Condición | Resultado |
|---|---|
| `version.Commit` = contenido de `<root>/debfactory.version` | sigue |
| distinto, o `version.Commit` = `dev` | error en la etapa `tool`, código 2: `debfactory: soy <c1> y packages fija <c2>` |

Fuera de CI no se comprueba (el mantenedor ejecuta versiones `dev` en local).

## Montaje en los runners (`run-runner.sh`)

```text
--mount type=bind,src=/srv/debfactory/tools,dst=/opt/debfactory/tools,readonly
```

En los dos niveles (`ci` y `pub`).

## Enlace de diferencias en el check (FR-009)

Si `debfactory.version` cambia entre la base y la cabeza del PR, el resumen del job empieza con:

```text
Herramienta: <anterior12> → <nueva12>
Diferencias: https://github.com/<ci.tool_repository>/compare/<anterior>...<nueva>
```

Con `ci.tool_repository` vacío, se omite la línea del enlace.
