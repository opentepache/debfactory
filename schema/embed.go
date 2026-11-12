// Package schema embebe el JSON Schema de los manifiestos de paquete.
package schema

import _ "embed"

// ManifestV1 es el JSON Schema 2020-12 de packages/<name>.yaml. Es una copia de
// specs/001-manifest-deb-builder/contracts/manifest.v1.schema.json.
//
//go:embed manifest.v1.schema.json
var ManifestV1 []byte
