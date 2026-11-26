# Contrato: `debfactory site`

Grupo de órdenes nuevo (research D1). Ninguna necesita la llave. Solo `site sync` necesita las
credenciales de rclone del destino; en CI la subida la hace el paso de publicación de `ci`
(research D5).

```text
debfactory site data  --root DIR --data DIR [--state FILE] [--out FILE]
debfactory site build --in FILE --out DIR
debfactory site serve --dir DIR [--addr 127.0.0.1:8080]
debfactory site sync  --root DIR --data DIR --dest NAME [--state FILE] [--dry-run]
```

| Orden | Efecto |
|---|---|
| `site data` | lee config, manifiestos y el estado de referencia (`--state`, por defecto `repo/state.yaml`), exige que corresponda a la generación vigente de `<data>` (`GenInfo.StateSHA256`) y lo verifica contra los índices `Packages` (data-model §1); escribe `site.json` (por defecto en la salida estándar) |
| `site build` | renderiza el árbol completo (research D3) desde un `site.json` en un directorio nuevo o vacío. Determinista (FR-014) |
| `site serve` | sirve un árbol generado en local para revisarlo, resolviendo `index.html` como el hosting (FR-015). Solo escucha en loopback |
| `site sync` | `data` + `build` y la subida del paso `site` (research D5) a un destino configurado, p. ej. el de pruebas (FR-015). Necesita las credenciales de rclone del destino, como `repo sync`; `--dry-run` imprime qué subiría y borraría |

## Códigos de salida

| Código | Cuándo |
|---|---|
| 0 | hecho |
| 1 | datos incoherentes (estado contra índices, nombre reservado), ruta prohibida, página de más de 100 KB, error de E/S |
| 2 | uso incorrecto |

Los errores siguen el formato de `report` con la etapa `site`:

```text
debfactory: bob 4.3.0-0~tepache1 -: site: el estado la publica en trixie/arm64 pero no está en dists/trixie/main/binary-arm64/Packages
```

## En el pipeline

`ci main`, `ci retire` y `ci refresh`, con `site.enabled: true`, terminan generando el sitio
sobre la generación vigente con el estado de referencia de la ejecución y lo suben a
`ci.destination` (research D5). Corre también cuando no hay nada que publicar. Una línea en el log:

```text
site: serial 3, 12 páginas, 41 KB la mayor; r2: subidas 3, borradas 0
site: serial 3 sin cambios; nada que subir
```

Si falla: notificación `fallo: pipeline - en site` con el motivo, y el job termina con código 1 después
de terminar todo lo demás.

## `just`

| Receta | Qué hace |
|---|---|
| `just site` | vista previa en `dist/site/`: el estado de `testdata/project` (con una versión anterior de bob) y una generación simulada, sin llave ni repositorio publicado; imprime la ruta. El sitio real de un destino se sube con `site sync` |
| `just site-serve` | `just site` y `site serve` en `127.0.0.1:8080` |

## `ci doctor` (comprobación nueva)

Solo con `site.enabled: true`: `GET <public_url>/` de `ci.destination` (donde sube el pipeline)
responde 200 con `Content-Type: text/html`, es decir, la regla de reescritura de research D4 está
activa y el sitio subido. Si no, ❌ con el motivo.
