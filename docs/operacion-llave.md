# Operación de la llave de firma

El repositorio APT se firma con una llave GPG dedicada que tiene dos partes:

- **Llave maestra** (ed25519, solo certificación `[C]`, sin caducidad). Su fingerprint es el
  que se publica en la [guía de usuario](https://github.com/opentepache/packages/blob/main/docs/usuario.md) y en `repo.signing.masters`. Vive **solo**
  en el respaldo cifrado fuera de línea; nunca en la PC de build.
- **Subllave de firma** (ed25519 `[S]`, caduca a los 2 años). Firma los índices. Vive en la PC
  de build, en `<data>/gnupg/` (permisos `700`), protegida con una frase de paso propia guardada
  en `gnupg/passphrase` (permisos `600`), para que `debfactory publish` firme sin pedir nada.
  Con el pipeline (fase 003), `<data>` es `/srv/debfactory/data`, propiedad del usuario `dfpub`,
  y solo la ven los jobs de `main` ([operación del pipeline](operacion-ci.md)); antes era
  `~/.local/share/debfactory`. En las órdenes de este documento, sustituye
  `~/.local/share/debfactory` por `/srv/debfactory/data` y ejecútalas con
  `sudo -u dfpub` si ya migraste.

**Nunca van al repositorio git ni a la salida de ninguna orden:** la llave maestra, la
subllave, la frase de paso de la maestra, la de build ni el certificado de revocación. Solo los
fingerprints y la llave pública son públicos.

Los pasos con la llave maestra se hacen en un llavero temporal en memoria, preferiblemente con
la red desconectada, y ese llavero se borra al terminar:

```sh
export GNUPGHOME=$(mktemp -d -p /dev/shm)   # llavero temporal en RAM (700)
# … pasos …
gpgconf --kill gpg-agent; rm -rf "$GNUPGHOME"; unset GNUPGHOME
```

## 0. Preparar los medios de respaldo (una vez, con red)

El respaldo va en dos USB cifrados con LUKS2, uno de ellos fuera de casa. Hace falta
`cryptsetup`:

```sh
sudo apt install cryptsetup-bin
```

**Formatear borra todo el USB.** Identifica el disco antes: ejecuta esto sin el USB, conéctalo
y vuelve a ejecutarlo. El disco nuevo (`TRAN` = `usb`) es el tuyo, por ejemplo `sdb`:

```sh
lsblk -o NAME,SIZE,MODEL,TRAN,MOUNTPOINTS
```

Con cada USB:

```sh
D=/dev/sdX                                   # el de lsblk; revísalo dos veces
sudo umount ${D}* 2>/dev/null                # el escritorio suele montarlo solo
sudo wipefs -a $D
echo 'type=83' | sudo sfdisk $D              # una partición Linux: ${D}1
sudo cryptsetup luksFormat --type luks2 ${D}1
sudo cryptsetup open ${D}1 respaldo
sudo mkfs.ext4 -L debfactory-respaldo /dev/mapper/respaldo
sync; sudo cryptsetup close respaldo
```

La frase de paso de LUKS es **distinta** de la de la maestra y va en el gestor de
contraseñas. Si se pierde, el USB no sirve. Comprueba que los dos USB abren antes de seguir.

**Montar y desmontar** (en §1 y al restaurar). Usa `respaldo` y `/media/RESPALDO` para el
primero, y `respaldo2` y `/media/RESPALDO2` para el segundo:

```sh
sudo cryptsetup open /dev/sdX1 respaldo
sudo mkdir -p /media/RESPALDO && sudo mount /dev/mapper/respaldo /media/RESPALDO
sudo chown $USER: /media/RESPALDO
# … usar …
sync; sudo umount /media/RESPALDO; sudo cryptsetup close respaldo
```

## 1. Creación (una vez)

Monta los dos USB (§0) **antes** de desconectar la red.

```sh
export GNUPGHOME=$(mktemp -d -p /dev/shm)
gpg --quick-gen-key 'debfactory APT repository <apt@TU-DOMINIO>' ed25519 cert never
MASTER=$(gpg --with-colons -k | awk -F: '/^fpr/{print $10; exit}')
gpg --quick-add-key "$MASTER" ed25519 sign 2y
SUB=$(gpg --with-colons -k | awk -F: '/^fpr/{print $10}' | sed -n 2p)
echo "maestra: $MASTER"; echo "subllave: $SUB"
```

`--quick-gen-key` pide la frase de paso de la maestra: que sea fuerte y guárdala en tu gestor
de contraseñas, no junto al respaldo.

**Respaldo** en los dos USB de §0:

```sh
gpg --armor --export-secret-keys "$MASTER" > /media/RESPALDO/debfactory-master.asc
cp "$GNUPGHOME/openpgp-revocs.d/$MASTER.rev" /media/RESPALDO/debfactory-master.rev
gpg --armor --export "$MASTER" > /media/RESPALDO/debfactory-public.asc
date -d '+2 years' +%F > /media/RESPALDO/subllave-caduca.txt
cp -a /media/RESPALDO/. /media/RESPALDO2/
diff -r /media/RESPALDO /media/RESPALDO2 && echo "copias idénticas"
```

Desmonta y cierra los dos USB (§0) al terminar §1. Guarda uno fuera de casa.

Opcional: una copia en papel. `paperkey` solo lee el formato binario, así que el `.asc` pasa
antes por `gpg --dearmor`:

```sh
gpg --dearmor < /media/RESPALDO/debfactory-master.asc \
  | paperkey --output /media/RESPALDO/debfactory-master.paper.txt
```

La parte secreta sigue cifrada con la frase de paso de la maestra. Imprímela con la red cortada,
en una impresora conectada por USB, y borra después la cola de impresión.

**Subllave para la PC de build.** Se le pone una frase de paso propia (aleatoria) en una copia
del llavero, y solo se exporta la subllave:

```sh
read -rsp 'Frase de la maestra: ' MP; echo      # no queda en el historial
BUILDPASS=$(head -c 32 /dev/urandom | base64)
cp -a "$GNUPGHOME" "$GNUPGHOME.c" && export GNUPGHOME="$GNUPGHOME.c"
printf '%s\n%s\n' "$MP" "$BUILDPASS" | \
  gpg --batch --pinentry-mode loopback --command-fd 0 --passwd "$MASTER"
unset MP
printf '%s\n' "$BUILDPASS" | gpg --batch --pinentry-mode loopback --passphrase-fd 0 \
  --export-secret-subkeys "$SUB!" > /dev/shm/subllave.gpg
gpgconf --kill gpg-agent; rm -rf "$GNUPGHOME"
```

**Instalar en la PC de build:**

```sh
D=~/.local/share/debfactory/gnupg
install -d -m 700 "$D"
GNUPGHOME="$D" gpg --batch --import /dev/shm/subllave.gpg
printf '%s\n' "$BUILDPASS" > "$D/passphrase"; chmod 600 "$D/passphrase"
shred -u /dev/shm/subllave.gpg; unset BUILDPASS
GNUPGHOME="$D" gpg -K     # debe mostrar "sec#" (maestra ausente) y "ssb" con [S]
```

**Configurar:** pon `$MASTER` en `repo.signing.masters` y `$SUB` en `repo.signing.subkey`
(`config/debfactory.yaml` de `packages`). En la [guía de usuario](https://github.com/opentepache/packages/blob/main/docs/usuario.md), sustituye
`Fingerprint: <pendiente>` por el fingerprint de la maestra en grupos de 4 (como lo muestra
`gpg --fingerprint`). Comprueba con `debfactory repo verify` y haz commit.

## 2. Respaldo y restauración

**Restaurar la PC de build** (disco nuevo o llavero perdido): importa el respaldo en un llavero
temporal (`gpg --import /media/RESPALDO/debfactory-master.asc`, con la red cortada), define
`MASTER` y `SUB` con los fingerprints de `config/debfactory.yaml` y repite "Subllave para la PC
de build" e "Instalar en la PC de build". Sirve también si se pierden `BUILDPASS` o
`subllave.gpg`: la frase de build es nueva, pero la subllave es la misma. La siguiente publicación firma con la misma subllave
y el fingerprint publicado no cambia.

**Comprobación anual:** importa el respaldo en un llavero temporal y lista las llaves
(`gpg -K`). Si algo falla, rehaz el respaldo desde el otro medio.

## 3. Renovar la caducidad de la subllave

`debfactory publish` avisa cuando faltan menos de 60 días. Antes de que caduque:

```sh
export GNUPGHOME=$(mktemp -d -p /dev/shm)
gpg --import /media/RESPALDO/debfactory-master.asc
gpg --quick-set-expire "$MASTER" 2y "$SUB"
gpg --armor --export-secret-keys "$MASTER" > /media/RESPALDO/debfactory-master.asc
gpg --export "$MASTER" > /dev/shm/publica.gpg
gpgconf --kill gpg-agent; rm -rf "$GNUPGHOME"
GNUPGHOME=~/.local/share/debfactory/gnupg gpg --batch --import /dev/shm/publica.gpg
```

Solo cambia la parte pública: `publish` exporta el keyring nuevo y los usuarios lo reciben al
volver a ejecutar la línea del keyring de la guía. Mientras no lo hagan, apt sigue aceptando la
firma hasta la fecha de caducidad vieja; por eso se renueva con margen.

## 4. Rotar la subllave (cambio rutinario o PC comprometida)

1. Con la maestra en un llavero temporal, crea una subllave nueva
   (`gpg --quick-add-key "$MASTER" ed25519 sign 2y`) y actualiza el respaldo.
2. Si la PC está comprometida, revoca la vieja: `gpg --edit-key "$MASTER"`, `key N`, `revkey`,
   `save`.
3. Importa en la PC de build la llave pública actualizada y la subllave nueva (pasos de §1).
4. **Transición:** publica (`debfactory repo rebuild` y `just sync r2`) firmando todavía con la
   subllave vieja; el keyring publicado ya incluye la nueva. Espera 30 días (0 si hubo
   compromiso) para que los usuarios lo refresquen.
5. Cambia `repo.signing.subkey` a la nueva, publica y sincroniza.

El fingerprint de la maestra no cambia: los usuarios no tocan su `.sources`.

## 5. Rotar la llave maestra (pérdida o compromiso)

1. Crea una maestra nueva con su subllave (§1).
2. Añádela a `repo.signing.masters` (el keyring publicado contiene las dos) y añade su
   fingerprint a la guía de usuario.
3. aptly 1.6.1 solo firma con una subllave aunque reciba varias `-gpg-key`, así que la
   transición es por ventana: publica el keyring doble firmando con la subllave vieja, espera
   30 días (0 si la vieja está comprometida), y cambia `repo.signing.subkey` a la nueva.
4. Al terminar, revoca la maestra vieja (con su certificado de revocación del respaldo) y
   quítala de `repo.signing.masters` y de la guía.

## Lista de verificación

- [ ] `GNUPGHOME=~/.local/share/debfactory/gnupg gpg -K` muestra `sec#` y una `ssb` `[S]`.
- [ ] `~/.local/share/debfactory/gnupg` tiene `700` y `passphrase` tiene `600`.
- [ ] El respaldo está en dos USB LUKS2 (uno fuera de casa) y se comprobó este año.
- [ ] Las frases de paso de LUKS y de la maestra están en el gestor de contraseñas, no en los USB.
- [ ] `debfactory repo verify` pasa (fingerprint de la guía = configuración).
- [ ] `git grep -I -E 'PRIVATE[ ]KEY BLOCK'` no devuelve ningún bloque de llave.
