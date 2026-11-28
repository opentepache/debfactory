// Package verify implementa `debfactory verify` (specs/009, research D18):
// cualquiera, sin credenciales, comprueba que un .deb publicado es el que
// dice la página de procedencia y que se puede reconstruir.
package verify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/fetch"
	"debfactory/internal/supply"
)

// Options de una verificación.
type Options struct {
	Site      string // URL del sitio y del repositorio APT (https://apt.opentepache.org)
	Package   string
	Version   string // versión Debian publicada (p. ej. 1.4.0-0~tepache1)
	Suite     string // vacío = la primera suite probada
	Arch      string // vacío = amd64
	Keyring   string // keyring propio; vacío = el publicado, comprobado contra las huellas de site.json
	NoRebuild bool
	Workdir   string
	Client    *http.Client
	// Attest verifica la atestación del .deb (por defecto supply.Attestor
	// con la API pública de GitHub y cosign).
	Attest func(ctx context.Context, run supply.Run, file, sha256 string) error
	// Rebuild reconstruye el paquete y devuelve el sha256 del .deb
	// (por defecto, clona el repositorio de paquetes al commit y construye).
	Rebuild func(ctx context.Context, pv *Provenance, suite, arch, work string) (string, error)
}

// Provenance es lo que verify lee de provenance.json (data-model §8).
type Provenance struct {
	Package        string      `json:"package"`
	Repository     string      `json:"repository"`
	Version        string      `json:"version"`
	Level          int         `json:"level"`
	ManifestCommit string      `json:"manifest_commit"`
	Run            *supply.Run `json:"run"`
	Attestations   []struct {
		Suite   string `json:"suite"`
		Arch    string `json:"arch"`
		Subject string `json:"subject"`
		Digest  string `json:"digest"`
	} `json:"attestations"`
	Tests []struct {
		Suite string `json:"suite"`
		Arch  string `json:"arch"`
	} `json:"tests"`
}

// Result de una verificación: Verified, o el motivo por el que no.
type Result struct {
	Verified bool
	Deb      string
	SHA256   string
	Steps    []string // lo que se comprobó, en orden
	Reason   string
}

func (r *Result) step(format string, args ...any) {
	r.Steps = append(r.Steps, fmt.Sprintf(format, args...))
}

// Run verifica un paquete publicado. Un error es un fallo del entorno (red,
// herramientas); un resultado no verificado lleva su motivo.
func Run(ctx context.Context, o Options) (*Result, error) {
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	if o.Arch == "" {
		o.Arch = "amd64"
	}
	site := strings.TrimSuffix(o.Site, "/")
	if o.Workdir != "" {
		if err := os.MkdirAll(o.Workdir, 0o700); err != nil {
			return nil, err
		}
	}
	work, err := os.MkdirTemp(o.Workdir, "debfactory-verify-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	res := &Result{}
	no := func(format string, args ...any) (*Result, error) {
		res.Reason = fmt.Sprintf(format, args...)
		return res, nil
	}

	// 1. provenance.json
	pvBytes, err := get(ctx, o.Client, site+"/packages/"+o.Package+"/"+o.Version+"/provenance.json")
	if err != nil {
		return no("no hay página de procedencia: %v", err)
	}
	var pv Provenance
	if err := json.Unmarshal(pvBytes, &pv); err != nil || pv.Package != o.Package || pv.Version != o.Version {
		return no("provenance.json no corresponde a %s %s", o.Package, o.Version)
	}
	res.step("provenance.json de %s %s (sha256 %s)", o.Package, o.Version, fetch.SHA256(pvBytes))
	suite := o.Suite
	if suite == "" {
		for _, t := range pv.Tests {
			if t.Arch == o.Arch {
				suite = t.Suite
				break
			}
		}
	}
	if suite == "" {
		return no("la procedencia no tiene pruebas para %s", o.Arch)
	}

	// 2. InRelease firmado con el keyring publicado.
	keyring := o.Keyring
	if keyring == "" {
		if keyring, err = publishedKeyring(ctx, o.Client, site, work); err != nil {
			return no("keyring: %v", err)
		}
	}
	inrel, err := get(ctx, o.Client, site+"/dists/"+suite+"/InRelease")
	if err != nil {
		return no("InRelease de %s: %v", suite, err)
	}
	release, err := gpgvVerify(ctx, keyring, inrel, work)
	if err != nil {
		return no("la firma de InRelease no verifica: %v", err)
	}
	res.step("InRelease de %s firmado con el keyring del repositorio", suite)

	// 3. Packages (sha256 del InRelease) y el .deb (sha256 de Packages).
	pkgs, err := packagesIndex(ctx, o.Client, site, suite, o.Arch, release, work)
	if err != nil {
		return no("%v", err)
	}
	subject, digest := "", ""
	for _, a := range pv.Attestations {
		if a.Arch == o.Arch && (pv.Level != 2 || a.Suite == suite) {
			subject, digest = a.Subject, a.Digest
		}
	}
	filename, want := findDeb(pkgs, o.Package, digest)
	if filename == "" {
		var subj []string
		for _, a := range pv.Attestations {
			subj = append(subj, a.Subject)
		}
		return no("el índice de %s/%s no tiene el .deb de la procedencia (índice: %s; procedencia: %s)", suite, o.Arch,
			strings.Join(listDebs(pkgs, o.Package), ", "), strings.Join(subj, ", "))
	}
	deb := filepath.Join(work, subject)
	if err := download(ctx, o.Client, site+"/"+filename, deb); err != nil {
		return no("%s: %v", filename, err)
	}
	sha, err := fetch.SHA256File(deb)
	if err != nil {
		return nil, err
	}
	res.Deb, res.SHA256 = subject, sha
	if sha != want {
		return no("%s da %s y el índice firmado dice %s", res.Deb, sha, want)
	}
	if digest != sha {
		return no("%s da %s y la procedencia dice %q", res.Deb, sha, digest)
	}
	res.step("%s: sha256 %s igual en el índice firmado y en la procedencia", res.Deb, sha)

	// 4. Atestación del run que lo compiló (si se compiló en GitHub).
	if pv.Run != nil {
		attest := o.Attest
		if attest == nil {
			a := &supply.Attestor{List: (&fetch.GitHub{Client: o.Client}).Attestations}
			attest = a.VerifyRun
		}
		if err := attest(ctx, *pv.Run, deb, sha); err != nil {
			return no("atestación: %v", err)
		}
		res.step("atestación de %s (%s, commit %s)", pv.Run.Repo, pv.Run.Workflow, pv.Run.Commit)
	}

	// 5. Reconstrucción.
	if !o.NoRebuild {
		rebuild := o.Rebuild
		if rebuild == nil {
			rebuild = rebuildFromSource
		}
		got, err := rebuild(ctx, &pv, suite, o.Arch, work)
		if err != nil {
			return no("reconstrucción: %v", err)
		}
		if got != sha {
			return no("la reconstrucción da %s y el publicado %s", got, sha)
		}
		res.step("reconstruido desde %s@%s: mismo sha256", pv.Repository, pv.ManifestCommit)
	}
	res.Verified = true
	return res, nil
}

// publishedKeyring baja el keyring de site.json y comprueba que contiene las
// llaves maestras que site.json declara.
func publishedKeyring(ctx context.Context, c *http.Client, site, work string) (string, error) {
	b, err := get(ctx, c, site+"/site.json")
	if err != nil {
		return "", err
	}
	var sm struct {
		Repo struct {
			Keyring struct {
				Path         string   `json:"path"`
				Fingerprints []string `json:"fingerprints"`
			} `json:"keyring"`
		} `json:"repo"`
	}
	if err := json.Unmarshal(b, &sm); err != nil || sm.Repo.Keyring.Path == "" {
		return "", errors.New("site.json sin repo.keyring")
	}
	m := sm.Repo
	k := filepath.Join(work, "keyring.gpg")
	if err := download(ctx, c, site+"/"+strings.TrimPrefix(m.Keyring.Path, "/"), k); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "gpg", "--batch", "--with-colons", "--show-keys", k).Output()
	if err != nil {
		return "", fmt.Errorf("gpg --show-keys: %v", err)
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Split(l, ":"); len(f) > 9 && f[0] == "fpr" {
			have[f[9]] = true
		}
	}
	for _, f := range m.Keyring.Fingerprints {
		if !have[strings.ReplaceAll(f, " ", "")] {
			return "", fmt.Errorf("el keyring no contiene la llave %s", f)
		}
	}
	return k, nil
}

// gpgvVerify verifica un InRelease y devuelve el texto firmado.
func gpgvVerify(ctx context.Context, keyring string, inrelease []byte, work string) ([]byte, error) {
	in := filepath.Join(work, "InRelease")
	if err := os.WriteFile(in, inrelease, 0o644); err != nil {
		return nil, err
	}
	out := filepath.Join(work, "Release")
	cmd := exec.CommandContext(ctx, "gpgv", "--keyring", keyring, "--output", out, in)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%v: %s", err, bytes.TrimSpace(b))
	}
	return os.ReadFile(out)
}

// packagesIndex baja main/binary-<arch>/Packages (o .xz/.gz) y comprueba su
// sha256 contra el Release firmado.
func packagesIndex(ctx context.Context, c *http.Client, site, suite, arch string, release []byte, work string) ([]byte, error) {
	sums := map[string]string{}
	inSHA := false
	for _, l := range strings.Split(string(release), "\n") {
		if !strings.HasPrefix(l, " ") {
			inSHA = strings.HasPrefix(l, "SHA256:")
			continue
		}
		if f := strings.Fields(l); inSHA && len(f) == 3 {
			sums[f[2]] = f[0]
		}
	}
	for _, ext := range []string{"", ".xz", ".gz"} {
		name := "main/binary-" + arch + "/Packages" + ext
		want, ok := sums[name]
		if !ok {
			continue
		}
		b, err := get(ctx, c, site+"/dists/"+suite+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		if got := fetch.SHA256(b); got != want {
			return nil, fmt.Errorf("%s da %s y el Release firmado dice %s", name, got, want)
		}
		switch ext {
		case ".xz":
			return decompress(ctx, "xz", b)
		case ".gz":
			return decompress(ctx, "gzip", b)
		}
		return b, nil
	}
	return nil, fmt.Errorf("el Release de %s no lista main/binary-%s/Packages", suite, arch)
}

func decompress(ctx context.Context, tool string, b []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tool, "-dc")
	cmd.Stdin = bytes.NewReader(b)
	return cmd.Output()
}

// findDeb busca en Packages el .deb del paquete con el digest que registra
// la procedencia (el pool nombra los archivos por su contenido) y devuelve su
// Filename y su SHA256.
func findDeb(index []byte, pkg, digest string) (string, string) {
	sc := bufio.NewScanner(bytes.NewReader(index))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var name, file, sha string
	flush := func() (string, string) {
		if name == pkg && digest != "" && sha == digest {
			return file, sha
		}
		return "", ""
	}
	for sc.Scan() {
		l := sc.Text()
		if l == "" {
			if f, s := flush(); f != "" {
				return f, s
			}
			name, file, sha = "", "", ""
			continue
		}
		k, v, _ := strings.Cut(l, ": ")
		switch k {
		case "Package":
			name = v
		case "Filename":
			file = v
		case "SHA256":
			sha = v
		}
	}
	return flush()
}

func get(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "debfactory")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<30))
}

func download(ctx context.Context, c *http.Client, url, dst string) error {
	b, err := get(ctx, c, url)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func readJSONFile(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// listDebs son los Filename del paquete en un índice (para el mensaje de error).
func listDebs(index []byte, pkg string) []string {
	var out []string
	name := ""
	for _, l := range strings.Split(string(index), "\n") {
		k, v, _ := strings.Cut(l, ": ")
		switch k {
		case "Package":
			name = v
		case "Filename":
			if name == pkg {
				out = append(out, path.Base(v))
			}
		}
	}
	return out
}
