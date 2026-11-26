# Contrato: cambios en manifiesto y configuración

## Manifiesto: bloque opcional `i18n`

```yaml
summary: version manager for Neovim          # el del .deb, en inglés (sin cambios)
description: |                               # el del .deb, en inglés (sin cambios)
  bob installs, switches and removes Neovim versions …
i18n:
  es:
    summary: gestor de versiones de Neovim
    description: |
      bob instala, cambia y elimina versiones de Neovim …
```

| Campo | Reglas |
|---|---|
| `i18n` | opcional; solo la clave `es` en esta fase (el esquema rechaza otras) |
| `i18n.es.summary` | opcional; mismas reglas que `summary` en el esquema |
| `i18n.es.description` | opcional; mismas reglas que `description` |

Solo lo usa el sitio: no cambia el `.deb`. Un PR que solo cambia una traducción pasa por el
pipeline como cualquier cambio de manifiesto. El `.deb` sale idéntico, `ci main` no tiene nada que
publicar y su paso `site` sube la traducción (research D5). El detector (fase 005) no toca este
bloque.

**Nombres reservados**: el esquema rechaza `name: assets` y `name: es` (research D3).

## Configuración: bloque opcional `site`

```yaml
site:                      # sitio web del repositorio (specs/006-repo-website)
  enabled: true            # genera y sube el sitio en cada publicación; por defecto false
  name: OpenTepache        # nombre que muestra el sitio
```

| Campo | Reglas |
|---|---|
| `enabled` | booleano; por defecto `false` |
| `name` | obligatorio si `enabled`; texto de una línea |

La URL, las suites, las arquitecturas y las llaves salen de `repo` y no se repiten aquí.
`enabled: true` exige `repo.url` no vacía.

Una versión de la herramienta anterior a esta fase rechaza los bloques `site` e `i18n`
("field … not found"). El PR que los agrega en `packages` MUST fijar también la herramienta nueva,
igual que en la fase 005.
