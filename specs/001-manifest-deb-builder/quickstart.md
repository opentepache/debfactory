# Quickstart: validar la feature 001 de punta a punta

Guía para comprobar que la implementación cumple la spec. Los contratos están en
[contracts/](contracts/) y el modelo en [data-model.md](data-model.md).

## Requisitos (una sola vez, Debian trixie)

Requiere el repositorio `trixie-backports` en las fuentes de APT
(`deb http://deb.debian.org/debian trixie-backports main`).

```bash
sudo apt install -t trixie-backports golang-1.26
sudo apt install just qemu-user-binfmt   # binfmt registra arm64 para Docker; lintian corre en contenedor
# Docker: docker.io de Debian o docker-ce de upstream, con el usuario en el grupo docker
export PATH=/usr/lib/go-1.26/bin:$PWD/bin:$PATH GOTOOLCHAIN=local
go build -o bin/debfactory ./cmd/debfactory
docker run --rm --platform linux/arm64 debian:trixie uname -m   # → aarch64
```

`debfactory` comprueba estos requisitos y sale con código 3 y un mensaje claro si falta alguno.

## Escenario 1: construir un paquete (US1, SC-007)

```bash
just lock fnm            # crea packages/fnm.lock.yaml; revisa el diff
just build fnm amd64
dpkg-deb -f dist/fnm_1.39.0-0~pav1_amd64.deb Package Version Maintainer Homepage
```

**Esperado**: `fnm`, `1.39.0-0~pav1`, el mantenedor de la config y la homepage del manifiesto;
`build` termina con `lintian: pass`.

## Escenario 2: todos los paquetes, las dos arquitecturas (SC-001)

```bash
just build-all
ls dist/*.deb | wc -l    # → 8 (4 paquetes × 2 arch)
```

## Escenario 3: reproducibilidad (SC-006)

```bash
debfactory build --all --check-reproducible
```

**Esperado**: cada paquete informa `reproducible: <sha256>` con hashes idénticos en las dos
construcciones.

## Escenario 4: pruebas de instalación (US3, SC-002)

```bash
just test fnm
just test bob
```

**Esperado**: `fnm` da `pass` en las 5 suites × 2 arch. `bob` da `excluded` en bookworm (con el
motivo del manifiesto) y `pass` en las demás. Con cualquier `fail`, el resultado global es
`fail` y el informe nombra la suite, la etapa y la salida.

## Escenario 5: casos negativos (US2, SC-005)

Los fixtures de `testdata/negative/` reproducen cada caso con un servidor HTTP local
(`go test ./internal/... -run Negative`). A mano:

| Caso | Cómo provocarlo | Esperado |
|---|---|---|
| Manifiesto inválido | quitar `license.file` de una copia del manifiesto | `validate: …: license: falta la propiedad "file"` |
| Hash del lockfile alterado | cambiar un hash de `artifacts.amd64` en el lockfile | `verify: sha256 no coincide …` con esperado y obtenido |
| Checksum upstream que no coincide | fixture con un `sha256.txt` falso | `verify: sha256 no coincide con sha256.txt de upstream …` |
| Licencia no permitida | fixture con texto SSPL-1.0 declarado como `SSPL-1.0` | `license: licencia declarada "SSPL-1.0" no está en … (detectada "SSPL-1.0")` |
| Licencia distinta de la declarada | fixture con texto MIT declarado como `Apache-2.0` | `license: licencia detectada "MIT" no coincide con la declarada "Apache-2.0"` |
| Licencia no identificable | fixture con texto inventado | `license: texto de licencia no identificable (…; declarada "…")` |
| URL no oficial | `artifact.url` hacia otro repo | `validate: …: artifact.url: fuera de los releases de …` |

En todos los casos: código de salida 1 y ningún `.deb` nuevo en `dist/`.

## Escenario 6: un quinto paquete simple (US4, SC-003)

1. Copiar [examples/example.yaml](contracts/examples/example.yaml) a
   `packages/<nuevo>.yaml` y ajustarlo a una herramienta real con un binario estático
   (24 líneas) que cumpla la constitución V (sin repositorio APT del autor, no en Debian estable
   al día ni en griffo).
2. `just lock <nuevo> && just build <nuevo> && just test <nuevo>`.
3. Es un paquete de prueba: al terminar, borrar `packages/<nuevo>.yaml` y su lockfile (no forma
   parte de los paquetes iniciales).

**Esperado**: funciona sin tocar ningún otro archivo (`git status` muestra solo el manifiesto y
su lockfile).

## Escenario 7: actualizar una versión (SC-004)

Cambiar `version:` en el manifiesto y volver `pav` a `1` (N se reinicia con cada versión
upstream nueva, constitución VI); luego `just lock <pkg>` y `just build <pkg>`.
**Esperado**: el diff toca solo el manifiesto (`version` y `pav`) y el lockfile.
