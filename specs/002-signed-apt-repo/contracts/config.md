# Contrato: sección `repo` de `config/debfactory.yaml`

Se añade a la configuración de la fase 001
([001 config.md](../../001-manifest-deb-builder/contracts/config.md)). Las suites y las
arquitecturas se toman de `suites` y `arches`, que ya existen. La lectura sigue siendo estricta.

```yaml
repo:
  url: ""                      # https://apt.<dominio>; vacío hasta tener dominio (research D11)
  origin: OpenTepache
  label: OpenTepache
  component: main
  keep: 3                      # versiones por paquete y suite (FR-013); mínimo 1
  signing:
    masters:                   # fingerprints completos (40 hex) de las llaves maestras en el keyring
      - 7A1798EE1F842B3AC4FBA6D0AEE4D97E303806E7
    subkey: 9990E287F03E70F590DED142C9CC7E5409A3415C   # la que firma
    warn_days: 60              # FR-023
  destinations:
    local:
      remote: /var/tmp/debfactory-repo      # directorio que sirve `just serve`
      cache_control: false
    r2:
      remote: "r2:debfactory-apt"            # remote de rclone; credenciales fuera del repo
      public_url: ""                         # normalmente igual a repo.url
      cache_control: true
  cache_control:
    immutable: "public, max-age=31536000, immutable"   # pool/ y by-hash/
    mutable: "public, max-age=60"                      # InRelease, Release*, Packages*, keyring
```

## Validación

| Regla | Error |
|---|---|
| `keep` < 1 | `config/debfactory.yaml: repo.keep: mínimo 1` |
| fingerprint que no tiene 40 caracteres hex en mayúsculas | `repo.signing.masters[0]: fingerprint inválido` |
| `subkey` que no es subllave de ninguna `masters` en el llavero dedicado | `sign: la subllave … no pertenece a las llaves maestras configuradas` (al publicar) |
| nombre de destino inválido | `repo.destinations: nombre "R2" inválido` |
| `remote` con `@`, `password`, `key=` o `secret` | `repo.destinations.r2.remote: no puede contener credenciales` |

## Fuera de la configuración, y del repositorio git

| Qué | Dónde |
|---|---|
| Llavero con la subllave | `<data>/gnupg/` (`700`) |
| Frase de paso de la subllave | `<data>/gnupg/passphrase` (`600`) |
| Credenciales de R2 o del VPS | `~/.config/rclone/rclone.conf` (`600`) o variables `RCLONE_CONFIG_R2_*` |
| Llave maestra | Solo en el respaldo fuera de línea ([operacion-llave.md](operacion-llave.md)) |

Ejemplo de remote de rclone para R2 (en `rclone.conf`, no en el repositorio):

```ini
[r2]
type = s3
provider = Cloudflare
endpoint = https://<account-id>.r2.cloudflarestorage.com
access_key_id = …
secret_access_key = …
no_check_bucket = true
# rclone 1.60: R2 responde 501 al HEAD ?versionId= que sigue a cada subida
no_head = true
```

Para usar otro S3 o un VPS, solo cambia ese remote (`type = sftp` para un VPS).
