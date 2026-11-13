package repo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// Has indica si el almacén tiene ese sha256.
func (s Store) Has(sha string) bool {
	_, err := os.Stat(s.Path(sha))
	return err == nil
}

// Put copia src al almacén si su sha256 es sha. Si ya está, no hace nada.
func (s Store) Put(src, sha string) error {
	if s.Has(sha) {
		return nil
	}
	got, err := fetch.SHA256File(src)
	if err != nil {
		return err
	}
	if got != sha {
		return fmt.Errorf("%s: sha256 %s, se esperaba %s", src, got, sha)
	}
	dst := s.Path(sha)
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
