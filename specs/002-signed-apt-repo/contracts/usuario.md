# Contrato: guía de usuario (`docs/usuario.md`)

Es el documento que sigue el usuario final y, a la vez, la fuente de los comandos que ejecutan
las pruebas E2E (FR-032). Este contrato fija su estructura y los bloques ejecutables. El texto
explicativo puede cambiar.

## Reglas

- Los bloques ejecutables usan el info string `sh debfactory:<paso>`. Pasos: `add`, `verify`,
  `install`, `upgrade`, `downgrade` y `remove`. Los demás bloques no se ejecutan.
- Los bloques con `<suite>` se escriben una sola vez; la tabla de suites dice qué valor poner.
  La prueba reemplaza `<suite>` por la suite del contenedor y `https://apt.example.org` (la
  `repo.url` configurada) por `http://repo`.
- Los comandos llevan `sudo`. La prueba, que corre como root, lo quita.
- `add` tiene como mucho 4 comandos (FR-031); el heredoc del `.sources` cuenta como uno.
- La prueba ejecuta cada bloque entero con `sh -e`, no línea a línea, para respetar el
  heredoc.
- La guía MUST incluir el fingerprint completo de la llave maestra (o de las llaves maestras,
  durante una rotación). `repo verify` comprueba que coincide con `repo.signing.masters`.

## Estructura

1. **Qué es** (un párrafo) y la tabla de paquetes con la suite en la que falta cada uno (p. ej.
   `bob`: no está en bookworm porque requiere glibc ≥ 2.39).
2. **Suites**: tabla con la versión de cada distribución y el valor de `<suite>` (`bookworm`,
   `trixie`, `sid`, `noble`, `resolute`). Hay que escribirlo explícitamente: en sid,
   `/etc/os-release` dice `forky`.
3. **Agregar el repositorio**:

   ```sh debfactory:add
   sudo apt-get install -y curl ca-certificates
   sudo curl -fsSLo /etc/apt/keyrings/debfactory.gpg https://apt.example.org/debfactory.gpg
   sudo tee /etc/apt/sources.list.d/debfactory.sources >/dev/null <<'EOF'
   Types: deb
   URIs: https://apt.example.org/
   Suites: <suite>
   Components: main
   Architectures: amd64 arm64
   Signed-By: /etc/apt/keyrings/debfactory.gpg
   EOF
   sudo apt-get update
   ```

   El repositorio no publica `.sources`: así el árbol no contiene la URL y es el mismo en
   todos los destinos (research D9).

4. **Verificar la llave** (opcional; requiere `gnupg`):

   ```sh debfactory:verify
   gpg --show-keys --with-fingerprint /etc/apt/keyrings/debfactory.gpg
   ```

   El resultado debe mostrar el fingerprint publicado en esta guía.
5. **Instalar**:

   ```sh debfactory:install
   sudo apt-get install -y fnm buf aqua bob
   ```

   En bookworm, sin `bob`. La prueba toma la lista de paquetes de `state.yaml` para esa suite.
6. **Actualizar**: `sudo apt-get update && sudo apt-get upgrade` (bloque `upgrade`).
7. **Volver a una versión anterior**: `apt-cache policy <pkg>` para ver las disponibles y luego
   `sudo apt-get install --allow-downgrades <pkg>=<version>` (bloque `downgrade`). Incluye una
   nota sobre retenerla con `apt-mark hold`.
8. **Cambio de llave**: qué hacer cuando se anuncia una rotación. Es volver a ejecutar la línea
   del keyring de `add`.
9. **Quitar el repositorio**:

   ```sh debfactory:remove
   sudo rm -f /etc/apt/sources.list.d/debfactory.sources /etc/apt/keyrings/debfactory.gpg
   sudo apt-get update
   ```

   Opcional: `sudo apt-get purge fnm bob buf aqua`.

## Comprobaciones de la prueba E2E (SC-001, SC-002)

- `add` termina con código 0 y la salida de `apt-get update` no tiene líneas `W:`, `E:` ni
  `N:` relacionadas con el repositorio.
- `install` deja instaladas las versiones más altas de `state.yaml` para esa suite
  (`dpkg-query -W`).
- `remove` deja `apt-get update` sin peticiones a `http://repo`, y no quedan ni
  `/etc/apt/sources.list.d/debfactory.sources` ni `/etc/apt/keyrings/debfactory.gpg`.
