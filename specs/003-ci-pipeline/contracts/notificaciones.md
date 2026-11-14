# Contrato: notificaciones de fallo (issues)

Las abre y cierra `debfactory ci` con la API REST de GitHub, usando el `GITHUB_TOKEN` del job
(`issues: write`) y `User-Agent: debfactory` (FR-020 a FR-022, research D10). Solo las generan
las ejecuciones del nivel `pub`. Los PR muestran sus fallos en el propio check.

## Formato

```markdown
<!-- debfactory:failure package=fnm version=1.40.0-0~tepache1 stage=test -->
**Paquete:** fnm · **Versión:** 1.40.0-0~tepache1 · **Etapa:** test

**Motivo:** la orden de comprobación salió con código 127 en noble/arm64: `fnm: not found`

**Log:** https://github.com/<owner>/debfactory/actions/runs/1234 · **Commit:** a3f9e1c
```

| Campo | Valor |
|---|---|
| Título | `fallo: fnm 1.40.0-0~tepache1 en test` |
| Etiqueta | `ci.failure_label` (`pipeline-failure`) |
| Marcador | Primera línea del cuerpo; clave de deduplicación `(package, version, stage)` |
| `package` | nombre del paquete; `pipeline` si el fallo no es de un paquete |
| `version` | versión completa; `-` en `pipeline`, `state`, `audit` y `expiry` |
| `stage` | etapa de debfactory ([data-model](../data-model.md#1-ejecución-run)) o el paso del workflow |

Los textos de motivo pasan por `internal/redact` antes de enviarse.

## Ciclo de vida

1. **Fallo**:
   - Si hay un issue abierto con la etiqueta y el mismo marcador, se le añade un comentario con
     el motivo, el log y el commit nuevos.
   - Si no hay ninguno, se abre un issue nuevo.
2. **Publicación de `(package, version)`**: se cierran todos los issues abiertos de ese paquete
   y esa versión, sea cual sea la etapa, con el comentario
   `resuelto por <run>: publicado en serial N`.
3. **Etapa resuelta sin publicación**: `state`, `audit`, `expiry` y `pipeline` se cierran cuando
   una ejecución `pub` pasa esa etapa. Un `rebuild` de una versión ya publicada y con hashes
   iguales cierra los issues de esa versión.
4. **Aviso de caducidad (FR-032)**: es un issue de etapa `expiry` con la fecha `Valid-Until`. Se
   abre cuando quedan menos de `repo.validity.warn_days` días y se cierra cuando un `refresh`
   deja la validez por encima de ese margen.
5. **PR de estado antiguo (FR-007c)**: es un issue de etapa `state-pr-age` que enlaza el PR. Se
   cierra cuando el PR se integra.

## Límites

- La búsqueda de issues usa `GET /repos/{owner}/{repo}/issues?labels=pipeline-failure&state=open`,
  paginada y filtrada por marcador en el cliente. No usa la API de búsqueda, que tiene un límite
  de peticiones aparte y llega con retraso.
- Si la API de issues falla, la ejecución sigue con el mismo resultado (no lo empeora) y el
  error aparece en el log y en el resumen. En ese caso no hay otra notificación: es la única
  dependencia de la forja que se acepta.
