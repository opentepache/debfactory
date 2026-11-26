// Package site genera el sitio web estático del repositorio
// (specs/006-repo-website): una portada y una página por paquete, en inglés en
// la raíz y en español bajo es/, a partir del estado publicado, los
// manifiestos y la configuración.
//
//   - model.go: el modelo del sitio (site.json, data-model.md §1) y Load.
//   - verify.go: el modelo contra los índices Packages de la generación (FR-009).
//   - commands.go: los comandos de agregar, verificar y quitar el repositorio.
//   - i18n.go: los textos fijos de cada idioma (i18n/*.yaml).
//   - render.go: el árbol de páginas con html/template, determinista (FR-014).
//
// No sube nada: la subida es internal/sync (SyncSite) y la orquestación,
// internal/ci.
package site
