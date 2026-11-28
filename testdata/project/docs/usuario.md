# Repositorio APT de debfactory

debfactory publica versiones recientes de herramientas de desarrollo que no están al día en
Debian ni en Ubuntu, empaquetadas como `.deb` firmados para amd64 y arm64. Cada paquete pasó
lintian y una prueba de instalación real en cada distribución antes de publicarse.

| Paquete | Qué es | No disponible en |
|---|---|---|
| `fnm` | gestor de versiones de Node.js | — |
| `bob` | gestor de versiones de Neovim | bookworm (requiere glibc ≥ 2.39; bookworm tiene 2.36) |
| `buf` | herramientas para Protocol Buffers | — |
| `aqua` | gestor declarativo de herramientas CLI | — |

Un paquete puede no estar en alguna distribución o arquitectura; la página de cada paquete en
el sitio lo indica con su motivo (por ejemplo, "no se compila para arm64").

**Llave del repositorio** (llave maestra; compruébala antes de confiar en el repositorio):

```text
Fingerprint: 7267 1CD2 2168 4BF7 FDCD  FB39 65B2 7897 5D4F E04B
```

## Suites

Elige el valor de `<suite>` según tu distribución. En Debian sid, `/etc/os-release` declara el
codename de testing (`forky`): puedes usar `forky` (los paquetes son los mismos) o `sid`.

| Distribución | `<suite>` |
|---|---|
| Debian 12 | `bookworm` |
| Debian 13 | `trixie` |
| Debian 14 (testing) | `forky` |
| Debian sid (inestable) | `sid` |
| Ubuntu 24.04 LTS | `noble` |
| Ubuntu 26.04 LTS | `resolute` |

## Agregar el repositorio

Copia el bloque, cambia `<suite>` por el valor de la tabla y ejecútalo:

```sh debfactory:add
sudo apt-get update && sudo apt-get install -y curl ca-certificates
sudo curl -fsSLo /etc/apt/keyrings/debfactory.gpg https://apt.opentepache.org/debfactory.gpg
sudo tee /etc/apt/sources.list.d/debfactory.sources >/dev/null <<'EOF'
Types: deb
URIs: https://apt.opentepache.org/
Suites: <suite>
Components: main
Architectures: amd64 arm64
Signed-By: /etc/apt/keyrings/debfactory.gpg
EOF
sudo apt-get update
```

La llave solo se usa para este repositorio (`Signed-By`): no afecta a la confianza en los
repositorios de tu distribución.

## Verificar la llave (opcional)

Requiere `gnupg` (`sudo apt-get install -y gnupg`):

```sh debfactory:verify
gpg --show-keys --with-fingerprint /etc/apt/keyrings/debfactory.gpg
```

El fingerprint de la llave maestra (`pub`) debe coincidir con el de arriba. Si no coincide,
quita el repositorio y no instales nada.

## Instalar

```sh debfactory:install
sudo apt-get install -y fnm buf aqua bob
```

En bookworm, sin `bob`.

## Actualizar

```sh debfactory:upgrade
sudo apt-get update
sudo apt-get upgrade -y
```

Los índices del repositorio caducan a los 30 días de firmarse y se vuelven a firmar cada
semana. Si `apt-get update` dice que el `Release` de `apt.opentepache.org` *is expired*, el
repositorio no se ha refirmado a tiempo: no instales nada de él y avisa abriendo un issue.

## Volver a una versión anterior

El repositorio conserva las últimas 3 versiones de cada paquete. Para ver cuáles hay y volver a
una de ellas:

```sh
apt-cache policy fnm
```

```sh debfactory:downgrade
sudo apt-get install -y --allow-downgrades fnm=<version>
```

Si una versión se retira por un defecto, `apt upgrade` no te la cambia sola: usa este mismo
comando con la versión anterior. Para que `apt upgrade` no vuelva a subirla hasta que quieras,
retenla con `sudo apt-mark hold fnm` (y `sudo apt-mark unhold fnm` para soltarla).

## Cambio de llave

Si se anuncia un cambio de llave, vuelve a descargar el keyring con la segunda línea del
bloque de "Agregar el repositorio":

```sh
sudo curl -fsSLo /etc/apt/keyrings/debfactory.gpg https://apt.opentepache.org/debfactory.gpg
```

El archivo `.sources` no cambia.

## Qué garantiza este repositorio (y qué no)

**Garantiza:**

- **Integridad**: cada `.deb` está en un índice firmado con la llave del repositorio; apt
  rechaza cualquier archivo alterado.
- **Procedencia**: cada paquete sale de la versión estable oficial de su proyecto, descargada
  con su sha256 fijado y, si el proyecto firma sus versiones, con su firma comprobada contra una
  llave fijada en el manifiesto; si publica procedencia (SLSA), también se comprueba. En los
  paquetes que se compilan desde el código fuente se compara además el tarball con el tag de git.
- **Reproducibilidad**: los paquetes se construyen en un entorno controlado; los que se compilan
  desde el código fuente se compilan dos veces en entornos distintos y solo se publican si dan el
  mismo `.deb`. Cualquiera puede repetirlo.
- **Transparencia**: la herramienta que construye los paquetes es pública, y cada versión tiene
  una página de procedencia con el commit, la ejecución, las atestaciones (registro de
  transparencia de Sigstore) y los resultados de las pruebas.
- **Retiro rápido**: una versión con un problema se retira de todas las distribuciones.

**No garantiza** que el software de upstream sea de fiar: si el proyecto original publica una
versión con un fallo o con código malicioso, este repositorio la empaqueta tal cual. Las
comprobaciones aseguran que lo que instalas es exactamente lo que publicó upstream y que nadie
lo alteró por el camino.

## Comprobar un paquete por tu cuenta

La página de procedencia de cada versión (`https://apt.opentepache.org/packages/<paquete>/<versión>/`)
muestra de dónde salió y qué se comprobó antes de firmarlo, y enlaza a su `provenance.json`.

Con la herramienta pública (`debfactory`), sin credenciales:

```sh
debfactory verify fnm 1.39.0-0~tepache1 --suite trixie --arch amd64
```

Comprueba la firma del índice con el keyring publicado, que el `.deb` es el registrado en la
procedencia, su atestación de GitHub (si se compiló allí) y lo reconstruye en contenedores desde
el commit del manifiesto para comparar el sha256. Informa "verificado" o "no verificado" con el
motivo. Con `--no-rebuild` omite la reconstrucción (más rápido).

## Quitar el repositorio

```sh debfactory:remove
sudo rm -f /etc/apt/sources.list.d/debfactory.sources /etc/apt/keyrings/debfactory.gpg
sudo apt-get update
```

Los paquetes instalados se conservan. Para desinstalarlos también:

```sh
sudo apt-get purge fnm bob buf aqua
```
