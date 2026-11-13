# Contrato: órdenes del repositorio

Amplía la CLI de la fase 001 ([001 cli.md](../../001-manifest-deb-builder/contracts/cli.md)).
Las opciones globales, el formato de errores y `--json` son los mismos. Hay una opción global
nueva: `--data DIR`, por defecto `~/.local/share/debfactory`, que contiene `gnupg/`, `store/`,
`public/`, `sync/` y `lock`.

## Órdenes

| Orden | Efecto | Red | Escribe |
|---|---|---|---|
| `publish <pkg> [<version>] [--dry-run]` | Publica una versión. Sin `<version>`, la del manifiesto (`<upstream>-0~tepache<N>`). Pasos: verificar entradas (research D8) → estado nuevo con retención → generar `gen-<N+1>` → verificar → swap. Si la versión ya está publicada con el mismo sha256, no hace nada y sale con 0. | no | `repo/state.yaml`, `<data>/store`, `<data>/public` |
| `publish --all [--dry-run]` | Publica la versión del manifiesto de cada paquete de `packages/` en **una sola** generación. Si uno falla, no se publica ninguno y se listan todos los motivos (US3-4). | no | ídem |
| `retire <pkg> <version> --reason TEXTO [--last] [--dry-run]` | Quita la versión de todas las suites y la añade a `retired`. Si es la única versión publicada del paquete, exige `--last`. | no | ídem |
| `repo rebuild` | Regenera `current` desde `state.yaml` (FR-036). Toma los `.deb` del almacén, de `dist/` o los reconstruye con `build`. | solo si hay que reconstruir | `<data>/store`, `<data>/public` |
| `repo verify [--dest NOMBRE]` | Sin `--dest`, comprueba el árbol local (detalle abajo). Con `--dest`, descarga por `public_url` cada `InRelease`, verifica su firma y compara los índices y una muestra de `pool/` con el árbol local, además de los encabezados `Cache-Control` si `cache_control: true`. | solo con `--dest` | nada |
| `repo sync <destino> [--dry-run]` | Sube `current` al destino en el orden de research D6 y luego borra lo que ya no referencian ni la generación actual ni la última sincronizada. Repetirla es seguro. | sí | destino, `<data>/sync/<destino>.json` |
| `repo serve [--port 8080]` | Levanta `debfactory-repo-server` (Caddy en contenedor) sirviendo `destinations.local.remote` en `127.0.0.1:<port>` hasta Ctrl-C. | no | nada |
| `repo status` | Muestra el `serial`, la generación servida, las versiones por paquete y suite, el último `serial` sincronizado a cada destino y la caducidad de la subllave. | no | nada |

Comprobaciones de `repo verify` sin `--dest`:

- `state.yaml` cumple su contrato;
- `current` corresponde a `state.yaml` (`state_sha256`);
- cada `InRelease` está firmado por la subllave configurada y verifica con `debfactory.gpg`;
- cada índice coincide con sus hashes;
- cada entrada de `Packages` apunta a un archivo con el sha256 de `state.yaml`;
- la guía (`docs/usuario.md`) tiene la URL y el fingerprint de la configuración;
- la subllave caduca en más de 60 días; si no, da un aviso, que no hace fallar.

Cambio en una orden de la fase 001: `lock <pkg> --prune` falla si va a borrar una versión que
`state.yaml` tiene publicada (FR-038):

```text
debfactory: fnm 1.40.0 -: lock: 1.39.0 sigue publicada; retírala o déjala caer por retención antes de --prune
```

## Salida

```text
$ debfactory publish fnm
fnm 1.40.0-0~tepache1: agregar en bookworm trixie sid noble resolute (amd64 arm64)
fnm 1.37.0-0~tepache1: quitar por retención (keep 3) de bookworm trixie sid noble resolute
repo: serial 8 → 9; firmado con 9990E287F03E70F5…
aviso: la subllave de firma caduca el 2028-11-11 (en 54 días)
```

`--json` emite un evento por línea:

- `{"event":"add",…}`, `{"event":"remove",…}` y `{"event":"serial",…}` para los cambios;
- `{"event":"upload","phase":"pool|by-hash|indices|release|inrelease|delete","files":N}` en
  `repo sync`.

## Etapas y errores

Las etapas nuevas son `state`, `inputs`, `generate`, `sign`, `verify-repo`, `swap`, `sync` y
`lock-repo`.

```text
debfactory: fnm 1.40.0-0~tepache1 arm64: inputs: falta dist/fnm_1.40.0-0~tepache1_arm64.deb
debfactory: fnm 1.40.0-0~tepache1 amd64: inputs: test.json no aprobado (overall: fail)
debfactory: fnm 1.40.0-0~tepache1 amd64: inputs: sha256 del .deb no coincide con test.json; vuelve a ejecutar `debfactory test fnm`
debfactory: fnm 1.40.0-0~tepache1 amd64: inputs: test.json no cubre noble; ejecuta `debfactory test fnm` sin --suite
debfactory: fnm 1.40.0-0~tepache1 -: inputs: suites distintas entre amd64 [bookworm trixie sid noble resolute] y arm64 [trixie sid noble resolute]
debfactory: fnm 1.39.0-0~tepache1 amd64: state: ya publicada con otro sha256 (las versiones no se reescriben; sube revision)
debfactory: fnm 1.40.1-0~tepache1 -: state: versión retirada el 2026-12-03 (las versiones no se reutilizan; sube revision)
debfactory: fnm 1.39.0-0~tepache1 -: state: es la única versión publicada de fnm; usa --last para retirarla
debfactory: - - -: sign: la subllave 9990E287F03E70F5 caducó el 2028-11-11
debfactory: - - -: sign: gpg: no hay llave secreta para 9990E287F03E70F5 en ~/.local/share/debfactory/gnupg
debfactory: - - -: verify-repo: current (serial 8) no corresponde a repo/state.yaml (serial 9); ejecuta `debfactory repo rebuild`
debfactory: - - -: lock-repo: otra operación está en curso sobre ~/.local/share/debfactory
debfactory: - - -: sync: r2: rclone: AccessDenied (revisa las credenciales del remote "r2")
debfactory: - - -: sync: r2: repo.url está vacío; no se sincroniza un destino público sin URL
```

Ningún mensaje incluye la frase de paso, el contenido del llavero ni las credenciales. La salida
de `gpg`, `aptly` y `rclone` se filtra: se muestran solo las líneas de error y se ocultan
`passphrase`, `secret_access_key`, `access_key_id` y `token`.

## Códigos de salida

Los mismos que en la fase 001:

- `0`: éxito;
- `1`: fallo de una etapa;
- `2`: uso incorrecto;
- `3`: entorno incompleto. `publish`, `retire` y `rebuild` lo dan si falta `aptly` o `gpg`, si
  no existe el llavero dedicado o si `passphrase` no tiene permisos `600`; `sync` lo da si falta
  `rclone`; las pruebas E2E, si falta Docker.

## `justfile`

| Receta | Ejecuta |
|---|---|
| `just publish <pkg> [version]` | `debfactory publish <pkg> [version]` |
| `just retire <pkg> <version> <reason>` | `debfactory retire <pkg> <version> --reason <reason>` |
| `just sync <dest>` | `debfactory repo sync <dest>` y después `debfactory repo verify --dest <dest>` |
| `just serve` | levanta `debfactory-repo-server` sirviendo el destino `local` en `http://localhost:8080/` |
| `just e2e` | pruebas E2E del repositorio (SC-001, SC-003, SC-004) |
