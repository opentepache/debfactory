# Quickstart: validar la separación en dos repositorios

Escenarios que demuestran la fase de punta a punta. Detalles en
[contracts/herramienta.md](contracts/herramienta.md) y [contracts/migracion.md](contracts/migracion.md).

## Requisitos

- La fase 003 implementada y sus spikes del host (T019, T041) cerrados.
- La migración hecha (contracts/migracion.md, pasos 1–9).
- Clones en `~/Develop/debfactory` y `~/Develop/packages`.
- Una cuenta de GitHub secundaria para los escenarios de fork.

## 1. Las pruebas de la herramienta no necesitan `packages` (US5, SC-008)

```bash
git clone git@github.com:opentepache/debfactory.git /tmp/df && cd /tmp/df
just check && just e2e
```

**Esperado**: todo pasa sin `../packages`.

## 2. `tool-install` se niega con cambios o pruebas en rojo (FR-007, FR-007a)

| Acción | Esperado |
|---|---|
| `echo x >> README.md && just tool-install` | se niega: hay cambios sin commit |
| commit que rompe una prueba, `just tool-install` | se niega con la prueba que falla; nada se instala |
| árbol limpio y pruebas en verde | instala `/srv/debfactory/tools/<commit>/debfactory` (root, 755) e imprime el commit |

## 3. Un merge publica con la herramienta fijada (US1, SC-002, SC-006)

En `packages`, en una rama: subir la `revision` de `fnm`, PR, check verde, merge.

**Esperado**: se publica la revisión nueva; la entrada de auditoría tiene `commit` (de
`packages`) y `tool` (el contenido de `debfactory.version`).

## 4. Subir la herramienta (US2, SC-004, SC-005)

| Acción | Esperado |
|---|---|
| PR que fija un commit no instalado | el check falla en menos de 1 min, antes de construir, nombrando el commit, los instalados y la orden |
| `just tool-install` de una versión nueva H2 que no cambia ningún `.deb`; PR que fija H2 | el check muestra el enlace a las diferencias, reconstruye todo y pasa |
| mientras ese PR está abierto, un push a `main` | `main` corre con la versión anterior y no falla |
| merge del PR | la ejecución de `main` usa H2, compara y no publica nada |
| H3 que cambia el `.deb` de una versión publicada; PR sin subir su `revision` | el check falla nombrando el paquete |

## 5. La herramienta no sale de la PC (US3, SC-003)

Desde la cuenta secundaria, fork de `packages` y PR que añade a `pr.yml`:

```bash
git clone https://github.com/opentepache/debfactory.git /tmp/x || true
env | sort; find / -name '*.go' -path '*debfactory*' 2>/dev/null | head
ls -la /opt/debfactory/tools; touch /opt/debfactory/tools/x
```

**Esperado**: no corre nada hasta aprobarlo; aprobado, el clon falla (repositorio privado, sin
credenciales), no hay código fuente, `touch` falla (solo lectura) y los logs no contienen
credenciales. Un workflow que pide `debfactory-main` falla en "Set up runner" (trusted-gate).

## 6. Migración invisible para el usuario final (US4, SC-001)

```bash
debfactory repo verify --root ~/Develop/packages --dest r2
```

y `apt update && apt install --reinstall fnm` en contenedores limpios de las 5 suites.

**Esperado**: mismo serial que antes de migrar, sin avisos.

## 7. `ci doctor` gratis (SC-007)

```bash
GH_TOKEN=$(gh auth token) just ci-doctor
```

**Esperado**: todo en verde contra `opentepache/packages`, con la organización en el plan Free.
