package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goreleaser/nfpm/v2"
	"github.com/goreleaser/nfpm/v2/deb"
	"github.com/goreleaser/nfpm/v2/files"

	"debfactory/internal/assemble"
)

// Plan es el BuildPlan: metadatos de control y archivos ya resueltos.
type Plan struct {
	Name        string
	Version     string // upstream
	Pav         int    // N de -0~pav<N>
	Arch        string
	Maintainer  string
	Summary     string
	Description string
	Homepage    string
	Section     string
	Depends     []string
	Recommends  []string
	Conflicts   []string
	Replaces    []string
	Provides    []string
	Files       []assemble.File
	MTime       time.Time // SOURCE_DATE_EPOCH
}

// DebVersion es <upstream>-0~pav<N>.
func (p *Plan) DebVersion() string { return fmt.Sprintf("%s-0~pav%d", p.Version, p.Pav) }

// FileName es <name>_<upstream>-0~pav<N>_<arch>.deb.
func (p *Plan) FileName() string { return fmt.Sprintf("%s_%s_%s.deb", p.Name, p.DebVersion(), p.Arch) }

// Build escribe el .deb en outDir y devuelve su ruta. Con las mismas entradas
// produce los mismos bytes (research §3 riesgo 1).
func Build(p *Plan, outDir string) (string, error) {
	contents := make(files.Contents, 0, len(p.Files))
	for _, f := range p.Files {
		contents = append(contents, &files.Content{
			Source: f.Src, Destination: f.Dst, Type: files.TypeFile,
			FileInfo: &files.ContentFileInfo{Owner: "root", Group: "root", Mode: f.Mode, MTime: p.MTime},
		})
	}
	info := &nfpm.Info{
		Name:            p.Name,
		Arch:            p.Arch,
		Platform:        "linux",
		Version:         p.Version,
		VersionSchema:   "none",
		Release:         fmt.Sprintf("0~pav%d", p.Pav),
		Section:         p.Section,
		Priority:        "optional",
		Maintainer:      p.Maintainer,
		Description:     description(p.Summary, p.Description),
		Homepage:        p.Homepage,
		MTime:           p.MTime,
		DisableGlobbing: true,
		Overridables: nfpm.Overridables{
			Depends:    p.Depends,
			Recommends: p.Recommends,
			Conflicts:  p.Conflicts,
			Replaces:   p.Replaces,
			Provides:   p.Provides,
			Contents:   contents,
		},
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(outDir, p.FileName())
	fh, err := os.Create(out)
	if err != nil {
		return "", err
	}
	if err := deb.Default.Package(info, fh); err != nil {
		fh.Close()
		os.Remove(out)
		return "", fmt.Errorf("nfpm: %w", err)
	}
	if err := fh.Close(); err != nil {
		return "", err
	}
	return out, nil
}

// description une la sinopsis y la descripción larga; nfpm indenta las
// líneas y escribe "." en las vacías.
func description(summary, long string) string {
	return strings.TrimSpace(summary) + "\n" + strings.TrimSpace(long)
}

// BuildInfo es dist/<name>_<version>_<arch>.build.json.
type BuildInfo struct {
	SHA256         string   `json:"sha256"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	LockSHA256     string   `json:"lock_sha256"`
	Lintian        string   `json:"lintian"`
	Overrides      []string `json:"overrides"`
	TestCommand    string   `json:"test_command"`
}

// Marshal da el JSON indentado y con salto de línea final.
func (b *BuildInfo) Marshal() ([]byte, error) {
	if b.Overrides == nil {
		b.Overrides = []string{}
	}
	out, err := json.MarshalIndent(b, "", "  ")
	return append(out, '\n'), err
}

// ReadBuildInfo lee un build.json.
func ReadBuildInfo(path string) (*BuildInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b BuildInfo
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &b, nil
}
