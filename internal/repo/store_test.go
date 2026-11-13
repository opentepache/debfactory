package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/fetch"
)

func TestStorePut(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.deb")
	os.WriteFile(src, []byte("contenido"), 0o644)
	sha := fetch.SHA256([]byte("contenido"))
	s := Store{Dir: filepath.Join(dir, "store")}

	bad := strings.Repeat("0", 64)
	if err := s.Put(src, bad); err == nil {
		t.Fatal("un sha erróneo debería fallar")
	}
	if s.Has(bad) {
		t.Fatal("quedó un archivo con sha erróneo")
	}
	if err := s.Put(src, sha); err != nil {
		t.Fatal(err)
	}
	if !s.Has(sha) {
		t.Fatal("no está en el almacén")
	}
	if got, _ := fetch.SHA256File(s.Path(sha)); got != sha {
		t.Errorf("contenido alterado")
	}
}

func TestAcquire(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); err == nil || !strings.Contains(err.Error(), "lock-repo: otra operación está en curso") {
		t.Fatalf("segunda adquisición: %v", err)
	}
	release()
	r2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("tras liberar: %v", err)
	}
	r2()
}
