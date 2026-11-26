# Quickstart: validar las suites configurables y forky

## 1. Pruebas locales

```sh
just check   # config, site (suite ficticia), repo (ampliación), ci (main amplía; fallo no toca nada)
just e2e     # matriz con forky: 6 suites × 2 arquitecturas, comandos de la portada y apt install
```

Esperado: `TestE2ESite` 12/12 (forky amd64 y arm64 incluidas) y la auditoría de accesibilidad sin
problemas con la suite nueva en el sitio.

## 2. Activar en producción

1. `just tool-install` con el commit de esta fase.
2. PR en `packages` (research D7): pin nuevo, los cuatro campos en las suites existentes, forky, y
   `docs/usuario.md`. `verify` construye y prueba también en forky.
3. Al integrarlo, el log de `main.yml` muestra:

   ```text
   plan: … ampliar 5 (aqua bob buf fnm k9s → forky)
   repo: serial 2 → 3
   site: serial 3, …
   ```

## 3. Comprobar (SC-002, SC-003)

```sh
docker run --rm -it debian:forky sh -c '…bloque "Detectar" copiado de https://apt.opentepache.org/… && apt-get install -y aqua bob buf fnm k9s'
```

- `apt-cache policy bob` en forky muestra la versión publicada de apt.opentepache.org.
- La portada lista "Debian 12, 13, 14, sid · Ubuntu 24.04, 26.04" para cada paquete.
- `repo/audit.jsonl` (PR de estado) tiene 5 entradas `extend` con `"suites":["forky"]`.
- Lanzar `ci rebuild` de cualquiera de ellos termina en "nada que publicar" (SC-004).
