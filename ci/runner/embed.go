// Package runner expone la versión fijada del runner de GitHub Actions
// (runner.env) a debfactory: ci doctor la compara con el último release sin
// depender de que --root sea el repositorio de la herramienta
// (specs/004-split-repos, research D12).
package runner

import _ "embed"

// Env es el contenido de runner.env.
//
//go:embed runner.env
var Env string
