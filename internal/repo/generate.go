package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/redact"
	"debfactory/internal/report"
	"debfactory/internal/sign"
)

// PublicDir es el directorio de generaciones dentro de <data>.
func PublicDir(dataDir string) string { return filepath.Join(dataDir, "public") }

// StoreFor devuelve el almacén de <data>.
func StoreFor(dataDir string) Store { return Store{Dir: filepath.Join(dataDir, "store")} }

// Generate crea desde cero una generación completa y firmada del árbol para
// st (research D2): por cada suite, un repositorio local de aptly, un
// snapshot y su publicación en un endpoint de archivos nuevo, con
// Acquire-By-Hash. aptly publica sin firmar; debfactory añade Valid-Until
// (repo.validity.days) a cada Release y lo firma con la subllave
// (specs/003-ci-pipeline, research D11). Devuelve el directorio de la
// generación. Ante cualquier fallo la borra. signedAt es la fecha de firma
// (Env.Now): los tests la fijan en el pasado para probar la caducidad sin
// tocar el reloj del host.
func Generate(ctx context.Context, cfg *config.Config, st *State, store Store, dataDir string, signedAt time.Time) (genDir string, err error) {
	home := sign.Home(dataDir)
	if err := sign.TestSign(ctx, home, cfg.Repo.Signing.Subkey); err != nil {
		return "", err
	}
	pub := PublicDir(dataDir)
	if err := os.MkdirAll(pub, 0o755); err != nil {
		return "", report.Newf(report.Generate, "%v", err)
	}
	genDir = filepath.Join(pub, genName(pub, st.Serial))
	aptlyRoot := genDir + ".aptly"
	os.RemoveAll(genDir)
	os.RemoveAll(aptlyRoot)
	defer os.RemoveAll(aptlyRoot)
	defer func() {
		if err != nil {
			os.RemoveAll(genDir)
		}
	}()
	fail := func(format string, args ...any) error {
		return report.Newf(report.Generate, format, args...)
	}

	conf := map[string]any{
		"rootDir":       aptlyRoot,
		"gpgProvider":   "gpg2",
		"architectures": cfg.Arches,
		"FileSystemPublishEndpoints": map[string]any{
			"gen": map[string]string{"rootDir": genDir, "linkMethod": "hardlink"},
		},
	}
	confData, _ := json.Marshal(conf)
	if err := os.MkdirAll(aptlyRoot, 0o755); err != nil {
		return "", fail("%v", err)
	}
	confPath := filepath.Join(aptlyRoot, "aptly.conf")
	if err := os.WriteFile(confPath, confData, 0o644); err != nil {
		return "", fail("%v", err)
	}
	aptly := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "aptly", append([]string{"-config=" + confPath}, args...)...)
		cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			lines := redact.Lines(out.Bytes())
			if len(lines) == 0 {
				lines = []string{err.Error()}
			}
			return fail("aptly %s: %s", args[0]+" "+args[1], strings.Join(lines, "; "))
		}
		return nil
	}

	for _, su := range cfg.Suites {
		repoName, snap := "r-"+su.Name, fmt.Sprintf("%s-g%d", su.Name, st.Serial)
		if err := aptly("repo", "create", "-distribution="+su.Name, "-component="+cfg.Repo.Component, repoName); err != nil {
			return "", err
		}
		var files []string
		for _, name := range st.PackageNames() {
			for _, p := range st.Packages[name] {
				if !contains(p.Suites, su.Name) {
					continue
				}
				for _, arch := range cfg.Arches {
					path := store.Path(p.Debs[arch])
					if !store.Has(p.Debs[arch]) {
						return "", fail("falta en el almacén %s (sha %s); ejecuta `debfactory repo rebuild`",
							DebFile(name, p.Version, arch), p.Debs[arch][:12])
					}
					files = append(files, path)
				}
			}
		}
		if len(files) > 0 {
			if err := aptly(append([]string{"repo", "add", repoName}, files...)...); err != nil {
				return "", err
			}
		}
		if err := aptly("snapshot", "create", snap, "from", "repo", repoName); err != nil {
			return "", err
		}
		if err := aptly("publish", "snapshot", "-batch", "-skip-signing",
			"-acquire-by-hash",
			"-origin="+cfg.Repo.Origin, "-label="+cfg.Repo.Label,
			"-architectures="+strings.Join(cfg.Arches, ","),
			snap, "filesystem:gen:."); err != nil {
			return "", err
		}
		dists := filepath.Join(genDir, "dists", su.Name)
		if err := sign.AddValidUntil(filepath.Join(dists, "Release"), signedAt, cfg.Repo.Validity.Days); err != nil {
			return "", report.Newf(report.Sign, "%s: %v", su.Name, err)
		}
		if err := sign.SignRelease(ctx, home, cfg.Repo.Signing.Subkey, dists); err != nil {
			return "", err
		}
	}
	if err := sign.ExportKeyring(ctx, home, cfg.Repo.Signing.Masters, filepath.Join(genDir, "debfactory.gpg")); err != nil {
		return "", report.Newf(report.Sign, "%v", err)
	}
	return genDir, nil
}

// genName elige gen-<serial>, o gen-<serial>-<k> si ya existe una
// generación con ese serial (repo rebuild).
func genName(pub string, serial int) string {
	name := "gen-" + strconv.Itoa(serial)
	cur, _ := os.Readlink(filepath.Join(pub, "current"))
	for k := 1; ; k++ {
		_, err := os.Stat(filepath.Join(pub, name))
		if os.IsNotExist(err) || name != cur && !isGen(pub, name) {
			return name
		}
		name = fmt.Sprintf("gen-%d-%d", serial, k)
	}
}

// isGen indica si name es una generación ya registrada (con su .json).
func isGen(pub, name string) bool {
	_, err := os.Stat(filepath.Join(pub, name+".json"))
	return err == nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
