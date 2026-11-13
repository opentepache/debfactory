# Contrato: operación de la llave de firma

El procedimiento vivirá en `docs/operacion-llave.md` (FR-024). Aquí se fijan los pasos y las
garantías; los comandos de GnuPG 2.4 están verificados en el spike (research D4). Los pasos con
la llave maestra se hacen en un llavero temporal en tmpfs (`GNUPGHOME=$(mktemp -d -p
/dev/shm)`), preferiblemente con la red desconectada, y ese llavero se borra al terminar.

## 0. Medios de respaldo (una vez)

Dos USB con una partición LUKS2 y ext4 (`cryptsetup-bin`), montados en `/media/RESPALDO` y
`/media/RESPALDO2`. La frase de paso de LUKS es distinta de la de la maestra y vive en el
gestor de contraseñas. Se montan antes de desconectar la red.

## 1. Creación (una vez)

1. Crear la llave maestra: `gpg --quick-gen-key 'debfactory <…>' ed25519 cert never`, con una
   frase de paso fuerte.
2. Crear la subllave de firma: `gpg --quick-add-key <MAESTRA> ed25519 sign 2y`.
3. Exportar el respaldo:
   - `--export-secret-keys --armor` (maestra y subllave, con la frase de paso de la maestra);
   - el certificado de revocación (`openpgp-revocs.d/<MAESTRA>.rev`).
4. Guardar el respaldo en los dos USB de §0, comprobar que las copias son idénticas y llevar
   uno fuera de casa. Opcional: copia en papel con `paperkey`.
5. Preparar la subllave para la PC de build. En una **copia** del llavero temporal:
   1. `gpg --passwd <MAESTRA>` para ponerle una frase de paso aleatoria de build.
   2. `gpg --export-secret-subkeys <SUB>!`.
   3. Borrar la copia.
6. En la PC de build:
   1. Crear `<data>/gnupg` (`700`).
   2. Importar la subllave exportada.
   3. Escribir la frase de paso de build en `<data>/gnupg/passphrase` (`600`).
   4. Comprobar que `gpg -K` muestra `sec#`, es decir, la maestra ausente.
7. Poner los fingerprints en `repo.signing` y en la guía de usuario, y hacer commit.

## 2. Respaldo y restauración

- **Qué contiene el respaldo:** la maestra con su subllave, el certificado de revocación y una
  nota con la fecha de caducidad de la subllave. **Nunca** incluye la frase de paso de la
  maestra: esa vive en el gestor de contraseñas del mantenedor.
- **Restaurar la PC de build:** repetir los pasos 5 y 6 desde el respaldo. La siguiente
  publicación firma con la misma subllave (US6-2).
- **Comprobación anual:** restaurar el respaldo en tmpfs y listar las llaves. Si falla, se
  rehace el respaldo.

## 3. Renovar la caducidad de la subllave

Se hace antes del aviso de FR-023 (60 días):

1. Restaurar la maestra en tmpfs.
2. `gpg --quick-set-expire <MAESTRA> 2y <SUB>`.
3. Exportar la llave pública.
4. Actualizar el respaldo.

En la PC no hace falta tocar la parte secreta: basta con importar la llave pública actualizada.
`publish` exporta el keyring nuevo, y los clientes lo reciben al volver a ejecutar la línea del
keyring de la guía. Hasta que lo hagan, apt sigue verificando con la caducidad vieja mientras
no se alcance.

## 4. Rotar la subllave (cambio rutinario o compromiso de la PC)

1. Con la maestra en tmpfs, crear una subllave nueva.
2. Si hubo compromiso, revocar la anterior: `gpg --edit-key <MAESTRA>`, `key N`, `revkey`.
3. Repetir los pasos 5 y 6 de §1 con la nueva.
4. Cambiar `repo.signing.subkey`.
5. Hacer `publish` o `repo rebuild` y luego `repo sync`.

El fingerprint de la maestra no cambia, así que los clientes no tocan su `.sources`. Para
validar la firma nueva, apt necesita la llave pública de la subllave nueva, por lo que la guía
pide refrescar el keyring. Durante esa ventana, los clientes que no lo han refrescado ven un
error de firma. Para evitarlo:

1. Publicar primero el keyring con la subllave nueva, todavía firmando con la vieja.
2. Esperar el periodo de transición (30 días por defecto, o 0 si hubo compromiso).
3. Cambiar `repo.signing.subkey`.

La prueba SC-006 recorre este orden.

## 5. Rotar la llave maestra (pérdida o compromiso)

1. Crear una llave maestra nueva con su subllave, siguiendo §1.
2. Añadirla a `repo.signing.masters`. El keyring publicado contiene ambas y la guía muestra los
   dos fingerprints.
3. Firmar con la subllave nueva. aptly 1.6.1 acepta `-gpg-key` repetido, pero solo firma con
   una (comprobado en T049), así que no se puede firmar con las dos a la vez:
   1. publicar el keyring con las dos maestras, todavía firmando con la subllave vieja;
   2. esperar 30 días, o 0 si la vieja está comprometida;
   3. cambiar `repo.signing.subkey` a la subllave nueva y publicar.
4. Al terminar, revocar la maestra vieja, si se tiene, y quitarla de `masters`.

## Garantías que se prueban

- `git grep` y el historial no contienen bloques `PRIVATE KEY` ni el contenido de `passphrase`
  (SC-005).
- La salida de todas las órdenes en las pruebas no contiene la frase de paso de build.
- `<data>/gnupg/passphrase` con permisos distintos de `600` hace fallar `publish` (código 3).
