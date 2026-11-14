# Quickstart: validar el pipeline

Escenarios que demuestran la fase de punta a punta. Las órdenes están en
[contracts/cli.md](contracts/cli.md), los workflows en [contracts/workflows.md](contracts/workflows.md)
y la PC en [contracts/runner-host.md](contracts/runner-host.md).

## Requisitos

- **Fases anteriores**: lo de las fases 001 y 002, con el repositorio publicado
  (`repo verify --dest r2` en verde).
- **En GitHub**:
  - el repositorio, público, con el código en `main`;
  - un fine-grained PAT solo para ese repositorio, con `Administration: write` y
    `Actions: write`. Se pega solo en la entrada estándar de `setup.sh`, nunca en el chat ni en
    un archivo del repositorio.
- **En la PC**:
  - `sudo ci/host/setup.sh`: crea los usuarios, Docker rootless, las unidades, el temporizador y
    la regla de nftables;
  - `sudo ci/host/migrate-data.sh`: mueve los datos y las credenciales a `/srv/debfactory`;
  - `just runner-image`.
- **Comprobación**: `just ci-doctor` en verde, tanto para la configuración de GitHub (fork
  approval, permisos del token, ruleset en `main`, etiqueta) como para la del host.
- **Una cuenta de GitHub secundaria** que haga de contribuyente externo (escenario 3).

## 1. Un merge publica solo (US1, SC-001)

```bash
# en una rama: subir fnm a una versión nueva (o subir su revision)
$EDITOR packages/fnm.yaml && just lock fnm
git commit -am "fnm: 1.40.0" && git push -u origin fnm-1.40.0
# abrir el PR, esperar el check en verde e integrarlo
```

**Esperado** (anotar la hora del merge):

- **Ejecución de `main`** en `debfactory-main`: el plan publica solo fnm. Después: build, lint,
  test 10/10, `serial` +1, `sync r2` y `verify --dest r2` en verde.
- **PR de estado**: aparece `debfactory/state` con `repo/state.yaml` y una línea nueva en
  `repo/audit.jsonl`.
- **Desde un contenedor limpio de cada suite**, siguiendo `docs/usuario.md`,
  `apt-get install fnm` instala la versión nueva antes de 30 minutos desde el merge.
- **Retención**: `apt-cache policy fnm` sigue listando la versión anterior.
- **Integrar el PR de estado**: su ejecución de `main` termina en verde con "nada que
  publicar", y el `serial` no cambia.
- **Medición pendiente de la fase 002**: esta misma ejecución sirve para medir SC-007 (T062,
  paso 5).

## 2. Los PR se prueban y nunca publican (US2, SC-002)

| Provocar en un PR | Esperado |
|---|---|
| Cambiar un sha256 de `packages/fnm.lock.yaml` | El check falla en la etapa `verify`, nombrando el artefacto y los dos hashes. `repo status` muestra el mismo `serial` y el mismo `current` |
| Subir la versión de fnm correctamente | El check queda en verde, con la tabla por suite y arquitectura en el resumen. No hay publicación ni PR de estado |
| Cambiar la generación del changelog en `internal/` sin subir revisiones | El check falla, nombrando los 4 paquetes cuyo `.deb` cambiaría (FR-010) |
| Cambiar solo `docs/usuario.md` | No hay ejecución (`paths-ignore`) |

## 3. Secretos fuera del alcance de los forks (US3, SC-003)

Desde la cuenta secundaria, haz un fork y abre un PR que añada un paso a `pr.yml` y un workflow
nuevo con `runs-on: [self-hosted, debfactory-main]`. Ambos deben intentar lo siguiente:

```bash
env | sort; ls -la /srv/debfactory /home/pav0n ~/.local/share/debfactory ~/.gnupg
cat /srv/debfactory/data/gnupg/passphrase /srv/debfactory/rclone/rclone.conf
docker run --rm -v /:/host debian:trixie ls /host/srv/debfactory/data
curl -m 5 http://192.168.1.1/; touch /srv/debfactory/x
```

**Esperado:**

- **Antes de aprobar**: el PR muestra "Approve workflows to run" y no se ejecuta nada en la PC.
- **Después de aprobar**:
  - el job que pidió `debfactory-main` falla en "Set up runner" con `trusted-gate: rechazado`,
    sin ningún paso ejecutado;
  - el job de `pr.yml` corre en `debfactory-pr` y no ve ningún secreto ni `/home/pav0n`;
  - el `docker run -v /:/host` solo ve lo que puede leer `dfci`, y `/srv/debfactory/data` le da
    permiso denegado;
  - `curl` a la red local se rechaza y `touch` falla.
- **Commit nuevo en el PR**: vuelve a pedir aprobación.
- **Logs**: `grep -E 'PRIVATE KEY|secret_access_key|access_key_id'` sobre los logs
  descargados no encuentra nada.
- **Tras el job**: `/srv/debfactory/ci/work` está vacío y `dfci` no tiene imágenes.

## 4. Serialización (SC-004)

Integra dos PR seguidos, con menos de un minuto entre ambos, que suban versiones de dos paquetes
distintos (o dos revisiones en `r2test` con `ci.destination: r2test` en una rama de prueba).

**Esperado:**

- las dos ejecuciones corren una tras otra; la segunda espera en cola y no se cancela;
- el `serial` sube dos veces y hay dos entradas en la auditoría;
- un solo PR de estado, actualizado;
- `repo verify --dest` en verde.

Si la primera ejecución quedó superada porque la segunda ya traía los dos cambios, el resultado
final es el mismo: ambas versiones publicadas.

## 5. Notificaciones (US4, SC-005)

| Provocar en `main` (con un PR integrado) | Issue esperado |
|---|---|
| Un lockfile con un sha256 incorrecto, integrado sin esperar el check | `fallo: … en verify` |
| Un error de lintian provocado (por ejemplo, un binario sin permisos de ejecución en el `install` del manifiesto) | `fallo: … en lint` |
| Una orden de comprobación que falla (`test.command`) | `fallo: … en test`, con suite y arquitectura |
| `chmod 644` de la frase de paso en la PC | `fallo: pipeline - en sign` (código 3) |
| Un remote `r2` con un bucket inexistente | `fallo: … en sync` |
| Repetir cualquiera de las anteriores | Un comentario nuevo en el mismo issue |
| Corregir y volver a ejecutar | El issue se cierra con el enlace a la ejecución |

Cada issue aparece menos de 5 minutos después de que falle la etapa. Al terminar, restaura la
configuración real.

## 6. Auditoría (US5, SC-006)

```bash
bin/debfactory ci audit check --against origin/main
# para cada .deb del pool público: buscar su sha256 en repo/audit.jsonl, hacer checkout del commit y reconstruir
git worktree add /tmp/a "$(jq -r 'select(.version=="1.40.0-0~tepache1").commit' repo/audit.jsonl)"
(cd /tmp/a && go build -o bin/debfactory ./cmd/debfactory && bin/debfactory build fnm)
sha256sum /tmp/a/dist/fnm_1.40.0-0~tepache1_*.deb   # = debs de la entrada
```

**Esperado**: `audit check` en verde y los hashes iguales. Editar a mano una línea vieja de
`repo/audit.jsonl` en un PR hace fallar la siguiente ejecución de `main` en la etapa `audit`.

## 7. Ejecuciones manuales (US6, SC-008)

Desde Actions → main → Run workflow:

| Entrada | Esperado |
|---|---|
| `rebuild`, `fnm`, `1.39.0-0~tepache1` (ya publicada) | Build y test en verde, "sha256 iguales; nada que publicar" |
| `rebuild` de una versión que falló por la red | Se publica y se cierra su issue |
| `retire`, `fnm`, `<versión>`, motivo | Retirada, sync y verify en verde. Entrada `retire` en la auditoría y PR de estado actualizado |
| `retire` de la única versión sin `last` | Falla sin cambios y pide `last` |
| `refresh` | `Valid-Until` nuevo en todos los `Release` remotos; `state.yaml` sin cambios |

Lanzar el workflow desde la cuenta secundaria no es posible (no tiene escritura).

## 8. Caducidad y recuperación (SC-007, SC-009)

1. **Caducidad**: `curl -s https://apt.opentepache.org/dists/trixie/InRelease | grep Valid-Until`
   da una fecha a unos 30 días de la última firma.
2. **Refirmado semanal**: `systemctl list-timers debfactory-refresh.timer` muestra la próxima
   ejecución. Tras 5 semanas, el historial de Actions tiene 5 ejecuciones `refresh` en verde, y
   ninguna generación servida bajó de 23 días de validez.
3. **Recuperación**: durante una ejecución de `main`, después de que el log muestre el swap
   local y antes del sync, haz `sudo systemctl kill debfactory-runner@pub`. Relanza con
   `rebuild` de esa versión o con un push vacío.
   **Esperado:** sync y verify se completan, no se reconstruye la generación, el PR de estado se
   actualiza y no hay entradas de auditoría duplicadas.
4. **PR de estado cerrado sin integrar**: la siguiente ejecución de `main` falla en la etapa
   `state` y abre un issue. `restore-state-pr` vuelve a crear el PR y la siguiente ejecución
   pasa.
