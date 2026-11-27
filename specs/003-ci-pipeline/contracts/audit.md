# Contrato: `repo/audit.jsonl`

Es el registro de auditoría (FR-023 a FR-025, research D9): una entrada JSON por línea,
terminada en `\n`, UTF-8 y sin líneas en blanco. Las claves van en el orden de la tabla y las
que no aplican se omiten, salvo `run`, que en `import` vale `null`. Solo lo escribe `ci`; si
alguien lo edita a mano, la siguiente ejecución lo detecta (etapa `audit`).

```json
{"seq":1,"time":"2026-11-13T15:47:39Z","op":"import","package":"aqua","version":"2.66.1-0~tepache1","commit":"9306cdd…","debs":{"amd64":"a39c9eed…","arm64":"41e13bf7…"},"serial":1,"run":null}
{"seq":9,"time":"2026-12-01T09:12:44Z","op":"publish","package":"fnm","version":"1.40.0-0~tepache1","commit":"a3f9e1c…","debs":{"amd64":"…","arm64":"…"},"serial":2,"run":"https://github.com/<owner>/debfactory/actions/runs/1234"}
{"seq":10,"time":"2026-12-03T18:30:02Z","op":"retire","package":"fnm","version":"1.40.0-0~tepache1","commit":"b71c0d2…","serial":3,"run":"https://github.com/<owner>/debfactory/actions/runs/1301","reason":"segfault al iniciar en arm64 (issue #12)"}
```

| Clave | Tipo | `publish` | `retire` | `import` |
|---|---|---|---|---|
| `seq` | entero ≥ 1, consecutivo | ✓ | ✓ | ✓ |
| `time` | RFC 3339 UTC, sin fracciones | ✓ | ✓ | ✓ (el `published` de `state.yaml`) |
| `op` | `publish` \| `retire` \| `import` | ✓ | ✓ | ✓ |
| `package` | nombre de manifiesto | ✓ | ✓ | ✓ |
| `version` | versión Debian completa | ✓ | ✓ | ✓ |
| `commit` | 40 hex | manifiesto y lockfile construidos | commit de la ejecución | `9306cdd` completo |
| `tool` | 40 hex | commit de la herramienta (fase 004) | ídem | ídem; ausente en entradas anteriores a la fase 004 o de una compilación `dev` |
| `debs` | `{amd64, arm64}`, 64 hex | ✓ | — | ✓ |
| `serial` | `serial` de `state.yaml` tras la operación | ✓ | ✓ | ✓ |
| `run` | URL de la ejecución | ✓ | ✓ | `null` |
| `reason` | texto de una línea | — | ✓ | — |

> **Fase 008**: `publish`, `import` y `extend` de un paquete de nivel 2 llevan `orig` y `builds`
> (en `extend`, solo las suites agregadas) en lugar de `debs` (specs/008-source-builds/data-model.md §3).

## Reglas (fallo = código 1, etapa `audit`)

| Regla | Mensaje |
|---|---|
| Línea que no es JSON válido o tiene claves desconocidas | `repo/audit.jsonl:7: clave desconocida "note"` |
| `seq` no consecutivo | `repo/audit.jsonl:7: seq 9 tras 7` |
| El registro de `REF` no es prefijo exacto | `repo/audit.jsonl: la línea 4 difiere de main (solo se permite añadir)` |
| Un `.deb` de `state.yaml` no tiene entrada `publish` o `import` con su sha256 | `repo/audit.jsonl: falta la publicación de fnm 1.40.0-0~tepache1 arm64` |
| `retire` sin `publish` o `import` previo | `repo/audit.jsonl:10: retiro de una versión nunca publicada` |

## Primera ejecución

Si `repo/audit.jsonl` no existe y `state.yaml` tiene `serial ≥ 1`, `ci` crea una entrada
`import` por cada versión publicada, en orden de paquete y versión, con
`commit = git log -1 --format=%H -- repo/state.yaml` en el momento de la importación. Con el
estado actual, ese commit es `9306cdd`. Las entradas llegan a `main` en el primer PR de estado.
