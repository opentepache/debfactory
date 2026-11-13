# Quickstart: validar el repositorio APT firmado

Escenarios que demuestran la fase de punta a punta. Las órdenes están en
[contracts/cli.md](contracts/cli.md) y los formatos en [data-model.md](data-model.md).

## Requisitos

- Lo de la fase 001 (Go, just, Docker, qemu-user-binfmt) y los 8 `.deb` en `dist/` con
  `test.json` que incluya `deb_sha256`. Si falta, vuelve a ejecutar `just test <pkg>`.
- `sudo apt install aptly rclone` (gnupg ya está en el host).
- La llave creada según [operacion-llave.md](contracts/operacion-llave.md) §1. Para probar sin
  la llave real, `just e2e` crea una desechable.

## 1. Primera publicación (US1, SC-001)

```bash
just publish fnm && just publish bob && just publish buf && just publish aqua
# o, en una sola generación:
bin/debfactory publish --all
bin/debfactory repo verify            # sin errores
git diff repo/state.yaml              # revisar y hacer commit
```

**Esperado:**

- `serial: 1`, con las 4 versiones. `bob` tiene `suites: [trixie, sid, noble, resolute]`.
- `current/pool/main/f/fnm/` tiene 2 `.deb`, uno por arquitectura, compartidos por las 5
  suites.
- Volver a publicar `fnm` no cambia nada y sale con código 0.

## 2. Agregar el repositorio siguiendo la guía (US2, SC-001, SC-002)

```bash
just e2e             # 5 suites × 2 arquitecturas con los bloques de docs/usuario.md
```

El E2E publica en un `<data>` temporal y sirve su `current` directamente, así que no depende de
US5. Para probar a mano: `just sync local` y `just serve`.

**Esperado:** 10/10 combinaciones. `apt-get update` no da `W:`, `E:` ni `N:` del repositorio.
Se instalan las versiones de `state.yaml`, y `remove` no deja rastro. Para probar a mano,
sigue `docs/usuario.md` en un contenedor, con `http://<ip-del-host>:8080` como URL.

## 3. Todo o nada (US3, SC-004)

| Provocar | Orden | Esperado |
|---|---|---|
| Mover `dist/fnm_*_arm64.deb` | `debfactory publish fnm` | `inputs: falta dist/…arm64.deb`; `serial` y `current` sin cambios |
| Editar `dist/fnm_*_amd64.test.json` con `"overall": "fail"` | ídem | `inputs: test.json no aprobado` |
| `chmod 644 <data>/gnupg/passphrase` | ídem | código 3 |
| Cambiar `repo.signing.subkey` por una que no existe | ídem | `sign: …`; `current` sin cambios; `repo verify` OK |
| Matar `repo sync r2` a mitad (Ctrl-C tras la fase `pool`) | `repo verify --dest r2` en otra terminal, y luego repetir `sync` | el cliente sigue viendo el estado anterior; la repetición completa la sincronización |

`just e2e` automatiza las cuatro primeras filas y la sincronización interrumpida contra el
destino local.

## 4. Actualizar, retener y retirar (US4, SC-003, SC-008)

Con fnm instalado en un contenedor de prueba:

```bash
# version nueva en el manifiesto → lock, build, test (fase 001) y:
just publish fnm && just sync local       # en el contenedor: apt update && apt upgrade → nueva
just retire fnm 1.40.0-0~tepache1 "prueba de retiro" && just sync local
# en el contenedor: apt update; apt install --allow-downgrades fnm=1.39.0-0~tepache1
bin/debfactory publish fnm 1.40.0-0~tepache1  # → state: versión retirada…
```

**Esperado:**

- Tras la cuarta versión, `apt-cache policy fnm` lista 3.
- La más vieja ya no está en `current/pool` y, después de dos `sync`, tampoco en el destino.

## 5. Sincronizar al hosting público (US5)

1. Configura el remote `r2` en `~/.config/rclone/rclone.conf` (`600`), fuera del repositorio,
   como en [config.md](contracts/config.md), con `no_check_bucket = true` y `no_head = true`.
2. En Cloudflare, en la zona del dominio, crea una Cache Rule (*Caching → Cache Rules*):
   - condición: `Hostname` *is in* los hostnames del repositorio;
   - *Eligible for cache*;
   - *Edge TTL:* usar el encabezado `Cache-Control` si existe;
   - *Browser TTL:* respetar el del origen.

   Sin esa regla, el *Browser Cache TTL* de la zona sube a 4 h el `max-age=60` de
   `Packages.gz` y `Packages.bz2`.
3. Pon `repo.url` y `destinations.r2.public_url`.
4. Prueba antes contra el bucket de prueba: `just e2e-r2` (destino `r2test`, llave
   desechable).
5. Ejecuta:

```bash
bin/debfactory repo sync r2 --dry-run   # lista de fases y archivos
just sync r2                             # sync + verify --dest r2
```

**Esperado:**

- Un segundo `sync` no sube nada.
- `verify --dest r2` comprueba la firma remota y los encabezados `Cache-Control`. Justo después
  de `sync`, el destino puede servir la generación anterior durante el `max-age` de los índices;
  `verify` reintenta hasta 2 min antes de fallar.
- `go test -tags e2e -run TestE2ESecrets ./internal/repotest/` pasa: ni el árbol ni el historial
  de git tienen bloques de llave privada ni credenciales. Un `git grep` simple de
  `secret_access_key` encuentra el filtro de `internal/redact` y el ejemplo de
  [config.md](contracts/config.md), que no son secretos.

## 6. Llave: respaldo, restauración y rotación (US6, SC-006)

`just e2e-key` recorre [operacion-llave.md](contracts/operacion-llave.md) §1–§4 con una llave
desechable y un cliente trixie:

1. Crear la llave.
2. Respaldarla.
3. Borrar `<data>/gnupg`.
4. Restaurarla y publicar.
5. Publicar el keyring con una subllave nueva.
6. Firmar con la subllave nueva y luego revocar la vieja.

En cada paso el cliente hace `apt-get update`. **Esperado:** el fingerprint de la maestra no
cambia y no hay errores.

## 7. Reconstrucción desde git (SC-009)

```bash
git clone <repo> /tmp/df && cd /tmp/df   # sin dist/ ni caché
go build -o bin/debfactory ./cmd/debfactory
# las opciones globales van después de la orden; la subllave, restaurada en /tmp/dfdata/gnupg
bin/debfactory repo rebuild --data /tmp/dfdata --cache /tmp/dfcache
diff <(cd /tmp/dfdata/public/current && find pool -type f -exec sha256sum {} +) \
     <(cd ~/.local/share/debfactory/public/current && find pool -type f -exec sha256sum {} +)
```

**Esperado:** sin diferencias en `pool/` ni en los `Packages`. Solo cambian `Release` e
`InRelease`, por la fecha y la firma, y el enlace `by-hash/*/Contents-<arch>.gz` que aptly deja
apuntando al archivo por hash, con el nombre de la última arquitectura procesada. Ese enlace no
se sube.
