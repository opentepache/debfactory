// Package lock lee y escribe packages/<name>.lock.yaml: los hashes verificados
// de cada versión (constitución III). Solo lo escribe `debfactory lock`.
package lock

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"go.yaml.in/yaml/v3"
)

const header = "# Generado por `debfactory lock`; no se edita a mano. Se revisa en el diff de git.\n"

// File es el lockfile de un paquete.
type File struct {
	Schema   int               `yaml:"schema"`
	Versions map[string]*Entry `yaml:"versions"`
}

// Entry son los hashes de una versión upstream.
type Entry struct {
	SourceDateEpoch int64             `yaml:"source_date_epoch"`
	LicenseSHA256   string            `yaml:"license_sha256"`
	ChecksumsSHA256 string            `yaml:"checksums_sha256,omitempty"`
	Artifacts       map[string]string `yaml:"artifacts,omitempty"`
	// SourceSHA256 es el hash del tarball del código fuente de un paquete de
	// nivel 2 (specs/008-source-builds, data-model §1).
	SourceSHA256 string `yaml:"source_sha256,omitempty"`
}

// Path devuelve la ruta relativa del lockfile de un paquete.
func Path(name string) string { return filepath.Join("packages", name+".lock.yaml") }

// Read lee el lockfile del paquete bajo root. Si no existe, devuelve uno vacío.
func Read(root, name string) (*File, error) {
	rel := Path(name)
	data, err := os.ReadFile(filepath.Join(root, rel))
	if errors.Is(err, os.ErrNotExist) {
		return &File{Schema: 1, Versions: map[string]*Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data, rel)
}

// Parse decodifica y valida el contenido de un lockfile; rel solo se usa para
// los mensajes. El detector de versiones lo usa con el lockfile de la rama de
// un PR (git show).
func Parse(data []byte, rel string) (*File, error) {
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %v", rel, err)
	}
	if f.Schema != 1 {
		return nil, fmt.Errorf("%s: schema: debe ser 1", rel)
	}
	if f.Versions == nil {
		f.Versions = map[string]*Entry{}
	}
	for v, e := range f.Versions {
		if e == nil {
			return nil, fmt.Errorf("%s: versions.%s: entrada vacía", rel, v)
		}
		if err := e.wellFormed(); err != nil {
			return nil, fmt.Errorf("%s: versions.%s.%v", rel, v, err)
		}
	}
	return &f, nil
}

// Write guarda el lockfile de forma determinista (claves ordenadas) y atómica.
func (f *File) Write(root, name string) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	path := filepath.Join(root, Path(name))
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lock-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (e *Entry) wellFormed() error {
	if e.SourceDateEpoch <= 0 {
		return errors.New("source_date_epoch: debe ser positivo")
	}
	if !hex64.MatchString(e.LicenseSHA256) {
		return errors.New("license_sha256: no es un sha256")
	}
	if e.ChecksumsSHA256 != "" && !hex64.MatchString(e.ChecksumsSHA256) {
		return errors.New("checksums_sha256: no es un sha256")
	}
	if e.SourceSHA256 != "" && !hex64.MatchString(e.SourceSHA256) {
		return errors.New("source_sha256: no es un sha256")
	}
	for arch, h := range e.Artifacts {
		if !hex64.MatchString(h) {
			return fmt.Errorf("artifacts.%s: no es un sha256", arch)
		}
	}
	return nil
}

// Complete comprueba que la entrada sirve para construir: hash de cada
// arquitectura y checksums_sha256 si el manifiesto declara checksums.
func (e *Entry) Complete(arches []string, needChecksums bool) error {
	for _, a := range arches {
		if e.Artifacts[a] == "" {
			return fmt.Errorf("falta artifacts.%s", a)
		}
	}
	if needChecksums && e.ChecksumsSHA256 == "" {
		return errors.New("falta checksums_sha256 (el manifiesto declara checksums)")
	}
	return nil
}

// CompleteSource comprueba que la entrada sirve para compilar un paquete de
// nivel 2: hash del tarball del código fuente.
func (e *Entry) CompleteSource() error {
	if e.SourceSHA256 == "" {
		return errors.New("falta source_sha256")
	}
	return nil
}

// Equal indica si dos entradas tienen los mismos hashes y fecha.
func (e *Entry) Equal(o *Entry) bool {
	if e.SourceDateEpoch != o.SourceDateEpoch || e.LicenseSHA256 != o.LicenseSHA256 ||
		e.ChecksumsSHA256 != o.ChecksumsSHA256 || e.SourceSHA256 != o.SourceSHA256 ||
		len(e.Artifacts) != len(o.Artifacts) {
		return false
	}
	for k, v := range e.Artifacts {
		if o.Artifacts[k] != v {
			return false
		}
	}
	return true
}
