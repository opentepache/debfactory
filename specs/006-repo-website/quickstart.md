# Quickstart: validar el sitio web del repositorio

Escenarios que prueban la fase de punta a punta. Rutas en research D3, órdenes en
contracts/cli.md y contenido de las páginas en contracts/pages.md.

## 1. En local, sin publicar nada

```sh
just site          # genera dist/site/ desde el repositorio de prueba local
just site-serve    # lo sirve en http://127.0.0.1:8080/
```

Para verlo en el hosting de pruebas: `debfactory site sync --root . --data <data> --dest r2test`
(contracts/cli.md).

Esperado:

- `/` en inglés y `/es/` en español, cada uno con un enlace al otro.
- Una página por paquete publicado en `/packages/<p>/` y `/es/packages/<p>/`.
- La página de `bob` muestra solo trixie, sid, noble y resolute, y el motivo de bookworm.
- Repetir `just site` deja `dist/site/` idéntico (`diff -r`).
- Con el navegador sin JavaScript y a 320 px de ancho, todo se lee sin desplazar la página en
  horizontal.

## 2. Pruebas automáticas

```sh
just check   # incluye las pruebas de internal/site (research D10)
just e2e     # incluye los comandos de la portada en las 10 combinaciones y la auditoría a11y
```

Esperado:

- `TestE2ESite` ejecuta tal cual los bloques `add-debian`/`add-ubuntu`, `verify-key`, la
  instalación de un paquete y `remove`, extraídos del HTML generado, en contenedores limpios de
  bookworm, trixie, sid, noble y resolute, amd64 y arm64: 10 de 10 (SC-002).
- La auditoría de accesibilidad no reporta errores A ni AA en ninguna página (SC-006).

## 3. Activarlo en el hosting

Una vez, en Cloudflare, en la zona `opentepache.org`: *Rules → Transform Rules → URL Rewrite*
(research D4).

- Condición: `ends_with(http.request.uri.path, "/")` y host `apt.opentepache.org` (y el de
  pruebas).
- Ruta reescrita (dinámica): `concat(http.request.uri.path, "index.html")`.

Después, en `packages`, un PR con la herramienta nueva fijada y el bloque `site` en
`config/debfactory.yaml` (contracts/config.md). Al integrarlo, `main.yml` no tiene paquetes que
publicar, pero su paso `site` sube el sitio.

Esperado:

```sh
curl -sI https://apt.opentepache.org/ | head -1           # HTTP/2 200
curl -sI https://apt.opentepache.org/es/ | head -1        # HTTP/2 200
curl -s https://apt.opentepache.org/site.json | jq .serial
GH_TOKEN=$(gh auth token) debfactory ci doctor            # ✅ sitio en <public_url>/
```

y `apt update` en una máquina que ya tenía el repositorio sigue funcionando igual: las rutas del
repositorio no cambian.

## 4. El sitio se mantiene solo

- **Versión nueva**: cuando `main.yml` publica, el log termina con `site: serial N, …`. En menos
  de 10 minutos (en la práctica, 1–2), la portada y la página del paquete muestran la versión
  nueva, y la anterior aparece en "versiones anteriores" (SC-001).
- **Paquete nuevo**: un manifiesto nuevo que se publica aparece en la portada y tiene su página,
  sin ningún cambio en el sitio (SC-003).
- **Retiro**: tras `ci retire`, la versión desaparece del sitio.
- **Renovación periódica**: el log dice `site: serial N sin cambios; nada que subir`.
- **Fallo**: con el destino inaccesible solo para el paso del sitio (p. ej. en el entorno de
  prueba), llega la notificación `fallo: pipeline - en site`, los paquetes quedan publicados y el
  siguiente run sube el sitio (SC-007).
