package repo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"debfactory/internal/fetch"
)

// Store guarda una copia por contenido de cada .deb publicado
// (<data>/store/sha256/<aa>/<sha>.deb; research D5).
type Store struct {
	Dir string
}

// Path es la ruta del .deb con ese sha256.
func (s Store) Path(sha string) string {
	return filepath.Join(s.Dir, "sha256", sha[:2], sha+".deb")
}

// PathFor es la ruta de un archivo publicado por su nombre y sha256: los
// .deb como siempre y los demás (fuentes y .buildinfo de nivel 2) con
// extensión .blob.
func (s Store) PathFor(name, sha string) string {
	if strings.HasSuffix(name, ".deb") {
		return s.Path(sha)
	}
	return filepath.Join(s.Dir, "sha256", sha[:2], sha+".blob")
}

// HasFor indica si el almacén tiene ese archivo.
func (s Store) HasFor(name, sha string) bool {
	_, err := os.Stat(s.PathFor(name, sha))
	return err == nil
}

// PutFor copia src al almacén como el archivo publicado name.
func (s Store) PutFor(src, name, sha string) error {
	return s.put(src, sha, s.PathFor(name, sha))
}

// Has indica si el almacén tiene ese sha256.
func (s Store) Has(sha string) bool {
	_, err := os.Stat(s.Path(sha))
	return err == nil
}

// Put copia src al almacén si su sha256 es sha. Si ya está, no hace nada.
func (s Store) Put(src, sha string) error { return s.put(src, sha, s.Path(sha)) }

func (s Store) put(src, sha, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	got, err := fetch.SHA256File(src)
	if err != nil {
		return err
	}
	if got != sha {
		return fmt.Errorf("%s: sha256 %s, se esperaba %s", src, got, sha)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
