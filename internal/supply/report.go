package supply

import (
	"encoding/json"
	"os"
)

// Report es el informe de cadena de suministro de una versión (specs/009,
// data-model §7). Lo produce el build y lo resume la PR.
type Report struct {
	Package      string           `json:"package"`
	Version      string           `json:"version"`
	Arch         string           `json:"arch,omitempty"` // nivel 1: un informe por arquitectura
	Signature    SignatureReport  `json:"signature"`
	Provenance   Provenance       `json:"provenance"`
	TarballVsTag *TagReport       `json:"tarball_vs_tag,omitempty"`
	Toolchain    *ToolchainReport `json:"toolchain,omitempty"`
	Deps         *DepsReport      `json:"deps,omitempty"`
	Scorecard    *Scorecard       `json:"scorecard,omitempty"` // aviso, no bloquea (FR-017)
}

// SignatureReport: Type es minisign, gpg o sigstore; "none" si upstream no
// firma (el manifiesto no declara signing).
type SignatureReport struct {
	Type string `json:"type"`
	OK   bool   `json:"ok"`
}

// TagReport: archivos que el tarball trae de más o distintos respecto al tag,
// todos declarados en source.extra_files (si no, el build ya falló).
type TagReport struct {
	Extra []string `json:"extra"`
}

// ToolchainReport: toolchain de upstream verificado (sha256 y firma).
type ToolchainReport struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	OK      bool   `json:"ok"`
}

// DepsReport: dependencias del lenguaje verificadas y las omitidas.
type DepsReport struct {
	Count   int      `json:"count"`
	Skipped []string `json:"skipped,omitempty"`
	OK      bool     `json:"ok"`
}

// Write guarda el informe en JSON con sangría.
func (r *Report) Write(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
