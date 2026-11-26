package site

import "embed"

// Plantillas, estilos y traducciones van dentro del binario: el sitio de un
// estado dado depende solo de la versión de la herramienta (research D1).
//
//go:embed templates/*.html
var templatesFS embed.FS

//go:embed assets/site.css
var styleCSS []byte

// scriptJS es el único script del sitio, opcional: botones de copiar y de
// tema (FR-018).
//
//go:embed assets/site.js
var scriptJS []byte

// ogPNG es la imagen Open Graph de todo el sitio (FR-026).
//
//go:embed assets/og.png
var ogPNG []byte

//go:embed i18n/*.yaml
var i18nFS embed.FS
