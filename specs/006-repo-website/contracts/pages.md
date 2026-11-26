# Contrato: páginas del sitio

Lo que un visitante (y las pruebas) puede esperar de cada página. Rutas en research D3; datos en
data-model §1.

## Comunes a todas las páginas

- `<html lang="en">` en la raíz y `lang="es"` bajo `/es/`. Los fragmentos sin traducir llevan su
  propio `lang="en"`.
- `<link rel="alternate" hreflang="en|es">` a la página equivalente, y un enlace visible al otro
  idioma con su nombre en ese idioma ("Español", "English").
- Un solo `<h1>`, sin saltos de nivel en los encabezados, `<main>` y un enlace "saltar al
  contenido" al principio.
- Un solo recurso enlazado: `/assets/site.<hash>.css`. Sin scripts, sin fuentes, sin imágenes
  externas. El logotipo es un SVG en línea.
- Pie: enlace al repositorio de paquetes en GitHub (código y manifiestos) y el serial publicado.
- Cada bloque de comandos es `<pre><code data-debfactory-step="<paso>">` con el script exacto de
  `site.json`, sin prompts (`$`) ni texto añadido.

## Portada (`/`, `/es/`)

En este orden:

1. Qué es OpenTepache: herramientas de desarrollo recientes como `.deb` firmados, solo versiones
   estables, Debian y Ubuntu, amd64 y arm64, construidas y probadas automáticamente desde el
   código publicado por upstream.
2. Agregar el repositorio: un bloque para Debian (`add-debian`) y otro para Ubuntu
   (`add-ubuntu`), con las suites soportadas de cada distribución.
3. Verificar la llave: el fingerprint completo de cada llave maestra y el comando para comprobarlo
   (`verify-key`).
4. Paquetes: tabla con nombre (enlace a su página), resumen y versión actual (upstream).
5. Quitar el repositorio (`remove`).

## Página de paquete (`/packages/<p>/`, `/es/packages/<p>/`)

1. `<h1>` con el nombre y el resumen.
2. Descripción, en párrafos.
3. Datos: versión publicada (upstream y versión del paquete Debian), fecha de publicación,
   licencia (SPDX), enlace a upstream (`homepage`, con `rel="noopener"`).
4. Instalar: `sudo apt install <p>` y una nota que enlaza a la portada si el repositorio no está
   agregado.
5. Disponibilidad: tabla de suites × arquitecturas de la versión actual. Debajo, las suites
   excluidas con su motivo.
6. Versiones anteriores, si hay: versión, fecha y `sudo apt install <p>=<versión>`, de la más
   reciente a la más antigua.

## Lo que nunca aparece

- Versiones retiradas o no publicadas (FR-006).
- Paquetes sin versiones en el estado.
- Fechas de generación o cualquier dato que cambie sin que cambie lo publicado (research D12).
