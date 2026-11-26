# Contrato: cambios en el pipeline y la herramienta

## `ci main` (specs/003-ci-pipeline/contracts/cli.md)

Paso 3 (plan): además de `publish`, calcula `extend` (data-model §3). Paso 3b: las versiones de
`extend` no se comparan aparte. Paso 4 (gates): los paquetes de `extend` pasan por los gates como
los de `publish`, con su versión publicada. Pasos 5–7: una sola publicación con los dos.

Log:

```text
plan: publicar 1 (fnm 1.40.0-0~tepache1), ampliar 4 (aqua bob buf k9s → forky), …
bob 4.3.0-0~tepache1: build ok · lint ok · test ok
repo: bob 4.3.0-0~tepache1: + forky
```

Fallos:

| Caso | Etapa | Efecto |
|---|---|---|
| prueba de instalación falla en la suite nueva | `test` | ese paquete no se amplía; los demás sí (FR-004 de la fase 003: los paquetes son independientes) |
| el `.deb` reconstruido no es el publicado | `compare` | no se amplía; se notifica con los dos sha256 |
| la versión del manifiesto no es la más alta publicada | — | no entra en `extend` (no es un error) |

## `ci rebuild --package P --version V`

| Estado de `V` | Antes | Ahora |
|---|---|---|
| publicada, en todas las suites configuradas no excluidas | compara y "nada que publicar" | igual |
| publicada, le faltan suites, es la más alta | compara y "nada que publicar" | compara y, si coincide y pasa las pruebas, publica la ampliación con entrada `extend` |
| publicada, le faltan suites, no es la más alta | compara | compara y no publica: "no es la versión más alta; no se amplía" |
| no publicada | publica | igual |

## `debfactory publish P V` (fase 002)

Publicar una versión ya publicada con los mismos sha256 y suites nuevas en sus `test.json` las
agrega (research D2). Mismos sha256 y las mismas suites, o menos: sin cambios (como hoy).

## `debfactory site`

Sin órdenes nuevas. `site data` y la portada toman distribución, nombre legible y versión de cada
suite de la configuración; el bloque "Detectar" usa los codenames configurados y `catchall`
(research D4).
