# Contrato: CLI `debfactory`

Binario único (`cmd/debfactory`). Todas las órdenes leen `config/` y `packages/` relativos a
`--root` (por defecto, el directorio actual).

## Órdenes

| Orden | Efecto | Red | Escribe |
|---|---|---|---|
| `validate [pkg…]` | Valida manifiestos (schema + reglas semánticas). Sin `pkg`, todos. | no | nada |
| `lock <pkg> [--force]` | Para la versión del manifiesto: valida el manifiesto (incluida la regla de `pav`); descarga los assets de amd64 y arm64, el archivo de checksums upstream si existe y el texto de licencia; verifica contra los checksums upstream; **después** pasa el gate de licencias; consulta `published_at`; escribe el lockfile. Si ya hay una entrada para esa versión con hashes distintos, falla salvo con `--force`. | sí | `packages/<pkg>.lock.yaml` |
| `lock <pkg> --prune` | Borra del lockfile todas las versiones salvo la del manifiesto. | no | `packages/<pkg>.lock.yaml` |
| `build <pkg> [--arch A] [--check-reproducible]` | validate → lock → fetch → verify → license → assemble → package → lintian (el gate de licencias va tras verificar y antes de ensamblar; constitución, Flujo). Sin `--arch`, amd64 y arm64. Imprime `lintian: pass` por `.deb`. Con `--check-reproducible` imprime además `reproducible: <sha256>`. | solo si el asset, el archivo de checksums o la licencia no están en caché | `dist/*.deb`, `dist/*.build.json` |
| `build --all [--check-reproducible]` | `build` de cada paquete; sigue con el siguiente si uno falla y resume al final. | ídem | ídem |
| `lint <deb…>` | Solo lintian sobre `.deb` existentes. | no | nada |
| `test <pkg> [--arch A] [--suite S]` | Prueba de instalación del `.deb` ya construido en `dist/`. | sí, solo en `apt-get` | `dist/*.test.json` |
| `test --all` | `test` de cada paquete; sigue con el siguiente si uno falla y resume al final. | ídem | ídem |

Opciones globales: `--root DIR`, `--workdir DIR` (por defecto `$XDG_RUNTIME_DIR/debfactory`),
`--cache DIR` (por defecto `~/.cache/debfactory`), `--json` (resultado en una línea JSON por
evento, para la automatización futura).

## Códigos de salida

| Código | Significado |
|---|---|
| 0 | éxito |
| 1 | fallo de una etapa (validate, verify, license, lintian, test…) |
| 2 | uso incorrecto (argumentos) |
| 3 | entorno incompleto (falta Docker, no hay binfmt para arm64, etc.); lo comprueban `build`, `lint` y `test` antes de empezar |

## Formato de errores (stderr)

```text
debfactory: <pkg> <version> <arch>: <stage>: <causa en una línea>
  <detalle opcional, indentado>
```

Ejemplos (casos negativos de SC-005). El gate de licencias comprueba en este orden: texto
identificable → licencia detectada compatible con la declarada → cada término de la licencia
**declarada** en la allowlist; el primer fallo es el que se informa.

```text
debfactory: fnm 1.39.0 -: validate: packages/fnm.yaml: license: falta la propiedad "file"
debfactory: fnm 1.39.0 amd64: verify: sha256 no coincide para fnm-linux.zip
  esperado (lock):  7807664f39d39fc5…
  obtenido:         0000aaaa…
debfactory: buf 1.75.0 arm64: verify: sha256 no coincide con sha256.txt de upstream para buf-Linux-aarch64.tar.gz
debfactory: foo 2.0.0 -: license: licencia declarada "SSPL-1.0" no está en config/licenses.yaml (detectada "SSPL-1.0")
debfactory: qux 1.0.0 -: license: licencia detectada "MIT" no coincide con la declarada "Apache-2.0"
debfactory: bar 0.3.0 -: license: texto de licencia no identificable (cobertura 41 % < 90 %; declarada "MIT")
debfactory: baz 1.0.0 -: validate: packages/baz.yaml: artifact.url: fuera de los releases de owner/baz
debfactory: fnm 1.39.0 amd64: lock: no hay entrada para 1.39.0 en packages/fnm.lock.yaml; ejecuta `debfactory lock fnm`
```

## `justfile` (atajos)

| Receta | Ejecuta |
|---|---|
| `just build <pkg> [arch]` | `debfactory build <pkg> [--arch arch]` |
| `just build-all` | `debfactory build --all` |
| `just lint <pkg>` | `debfactory lint dist/<pkg>_*.deb` |
| `just test <pkg> [arch]` | `debfactory test <pkg> [--arch arch]` |
| `just lock <pkg>` | `debfactory lock <pkg>` |
| `just validate` | `debfactory validate` |
| `just check` | `go test ./...` + `validate` + `build --all --check-reproducible` + `test --all` |
