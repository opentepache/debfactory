# Contrato: configuración nueva en `config/debfactory.yaml`

Amplía la configuración de las fases 001 y 002
([002 config.md](../../002-signed-apt-repo/contracts/config.md)). La lectura sigue siendo
estricta.

```yaml
repo:
  # … lo de la fase 002 …
  validity:                  # research D11; sustituye a FR-004a de la fase 002
    days: 30                 # Valid-Until = fecha de firma + days
    warn_days: 7             # repo verify avisa y ci abre un issue "expiry" por debajo de esto

ci:
  repository: <owner>/debfactory      # GITHUB_REPOSITORY esperado (hook y ci)
  destination: r2                     # destino de repo.destinations que publica el pipeline
  state_branch: debfactory/state
  state_pr_max_age: 7d                # FR-007c
  failure_label: pipeline-failure
  labels:
    ci: debfactory-pr
    pub: debfactory-main
  code_paths:                         # un PR que toca alguno reconstruye y compara todo (FR-010)
    - cmd/
    - internal/
    - config/
    - schema/
    - ci/
    - go.mod
    - go.sum
```

## Reglas de validación (fallo = código 1, etapa `config`)

| Regla | Mensaje |
|---|---|
| `repo.validity.days` < 8 o > 90 | `config: repo.validity.days debe estar entre 8 y 90` |
| `repo.validity.warn_days` ≥ `days` − 7 | `config: repo.validity.warn_days deja menos de una semana entre refirmados` |
| `ci.repository` sin la forma `owner/nombre` | `config: ci.repository inválido` |
| `ci.destination` no está en `repo.destinations` o no tiene `public_url` | `config: ci.destination "r3" no existe` |
| `ci.labels.ci` == `ci.labels.pub` | `config: las etiquetas de los dos niveles deben ser distintas` |
| `ci.state_branch` == rama por defecto | `config: ci.state_branch no puede ser main` |

El mínimo de 8 días asegura que haya al menos una semana completa para que el refirmado semanal
(cada 7 días) llegue antes de que caduque la generación anterior.
