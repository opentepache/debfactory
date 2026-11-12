package fetch

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var hexSHA256 = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ParseChecksums lee un archivo en formato sha256sum ("<hex>  <archivo>" o
// "<hex> *<archivo>") y devuelve nombre base → sha256 en minúsculas.
func ParseChecksums(data []byte) (map[string]string, error) {
	sums := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		hash, name, ok := strings.Cut(line, " ")
		name = strings.TrimPrefix(strings.TrimLeft(name, " "), "*")
		if !ok || !hexSHA256.MatchString(hash) || name == "" {
			return nil, fmt.Errorf("línea %d: no tiene el formato sha256sum", i+1)
		}
		sums[path.Base(name)] = strings.ToLower(hash)
	}
	return sums, nil
}

// ChecksumMismatch indica que un artefacto no coincide con el archivo de
// checksums de upstream (etapa verify).
type ChecksumMismatch struct {
	File, Asset, Expected, Got string
}

func (e *ChecksumMismatch) Error() string {
	return fmt.Sprintf("sha256 no coincide con %s de upstream para %s", e.File, e.Asset)
}

// VerifyChecksum comprueba el sha256 de un asset contra el archivo de
// checksums de upstream ya leído.
func VerifyChecksum(sums map[string]string, checksumsURL, assetURL, got string) error {
	file, asset := path.Base(checksumsURL), path.Base(assetURL)
	want, ok := sums[asset]
	if !ok {
		return fmt.Errorf("%s de upstream no incluye %s", file, asset)
	}
	if want != got {
		return &ChecksumMismatch{File: file, Asset: asset, Expected: want, Got: got}
	}
	return nil
}
