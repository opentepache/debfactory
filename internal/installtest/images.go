// Package installtest prueba un .deb en contenedores limpios de cada suite y
// arquitectura: instala, ejecuta sin red, comprueba la versión y purga
// (FR-021 a FR-024).
package installtest

import (
	"context"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/container"
)

// imageMaxAge: las imágenes de prueba se reconstruyen (con apt-get update
// fresco) si tienen más de un día.
const imageMaxAge = 24 * time.Hour

// ImageTag es la imagen local de una suite y arquitectura.
func ImageTag(suite, arch string) string { return "debfactory-test/" + suite + ":" + arch }

// EnsureImage prepara debfactory-test/<suite>:<arch>: la imagen oficial de la
// suite con los índices de APT ya descargados.
func EnsureImage(ctx context.Context, s config.Suite, arch string) error {
	dockerfile := "FROM " + s.Image + "\nRUN apt-get update\n"
	return container.EnsureImage(ctx, ImageTag(s.Name, arch), dockerfile, container.Platform(arch), imageMaxAge)
}
