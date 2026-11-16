package testutil

import (
	"path/filepath"
	"runtime"
)

// ProjectRoot devuelve la ruta absoluta de testdata/project: la copia
// congelada del repositorio de paquetes con la que se prueba la herramienta
// (specs/004-split-repos, research D9). No depende del directorio de
// trabajo del test.
func ProjectRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("testutil: runtime.Caller falló")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "project")
}

// RepoRoot devuelve la ruta absoluta de la raíz del repositorio de la
// herramienta (para lo que sigue siendo suyo: dist/, el historial de git).
func RepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("testutil: runtime.Caller falló")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}
