package supply

import "time"

// imageMaxAge: las imágenes auxiliares (cosign) se renuevan cada semana.
const imageMaxAge = 7 * 24 * time.Hour
