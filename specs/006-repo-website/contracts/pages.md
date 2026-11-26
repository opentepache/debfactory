# Contrato: páginas del sitio

Lo que un visitante (y las pruebas) puede esperar de cada página. Rutas en research D3; datos en
data-model §1.

## Comunes a todas las páginas

- `<html lang="en">` en la raíz y `lang="es"` bajo `/es/`. Los fragmentos sin traducir llevan su
  propio `lang="en"`.
- `<head>` (research D14): título y meta descripción propios, `canonical`, `hreflang` en/es/
  `x-default`, Open Graph y Twitter con la imagen única, la hoja de estilos, el script opcional
  (research D9) y un bloque JSON-LD.
- Cabecera: nombre, enlaces a instalar, paquetes, llave y código, el enlace al otro idioma con su
  nombre en ese idioma, y el botón de tema (`hidden` hasta que el script lo muestra).
- Un solo `<h1>`, sin saltos de nivel, `<main>` y un enlace "saltar al contenido" al principio.
- Recursos: solo `/assets/site.<hash>.css` y `/assets/site.<hash>.js`, del propio sitio. Sin
  fuentes ni imágenes externas.
- Pie: qué garantiza cada paquete, enlace al repositorio de paquetes y el serial publicado.
- Cada bloque de comandos es `<pre class="cmd" tabindex="0">` con `<code id=… data-debfactory-step=…>`
  y el script exacto de `site.json`, sin prompts ni texto añadido, con un botón "copiar" `hidden`
  que el script muestra.

## Portada (`/`, `/es/`)

En este orden:

1. Estado (paquetes publicados, firmado), título, qué es el repositorio y dos botones.
2. Agregar el repositorio: pestañas Detectar (`add`), Debian (`add-debian`) y Ubuntu
   (`add-ubuntu`), y el enlace a quitarlo.
3. Paquetes: tabla con nombre (enlace), versión, fecha, dónde está disponible en texto (y dónde
   no, aparte) y arquitecturas. Sin descripción (FR-004).
4. Comprobar la llave: fingerprint de cada llave maestra y `verify-key`.
5. Cómo se construye cada paquete.
6. Quitar el repositorio (`remove`).

## Página de paquete (`/packages/<p>/`, `/es/packages/<p>/`)

Ruta (breadcrumb) y cabecera de man page `<P>(1)`. Luego:

1. `<h1>` "Install <p> on Debian and Ubuntu" y una línea con versión, resumen y "signed .deb
   package".
2. SINOPSIS: `sudo apt install <p>` con enlace a cómo agregar el repositorio.
3. DESCRIPCIÓN: la del manifiesto en párrafos y el párrafo generado de construcción y pruebas.
4. DISPONIBILIDAD: tabla distribución × arquitectura de la versión actual y, debajo, cada suite
   excluida con su motivo.
5. VERSIONES ANTERIORES, si hay: versión, fecha y
   `sudo apt install --allow-downgrades <p>=<versión>`.
6. Ficha lateral: versión, versión del paquete Debian, fecha, licencia, upstream y arquitecturas.

## Archivos además de las páginas

- `sitemap.xml` y `robots.txt` (research D14).
- `site.json` (data-model §1).
- `assets/site.<hash>.css`, `assets/site.<hash>.js` y `assets/og.<hash>.png`.

## Lo que nunca aparece

- Versiones retiradas o no publicadas (FR-006).
- Paquetes sin versiones en el estado.
- Fechas de generación o cualquier dato que cambie sin que cambie lo publicado (research D12).
