package ci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"debfactory/internal/fetch"
	"debfactory/internal/installtest"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/site"
	"debfactory/internal/supply"
)

// Provenance es provenance.json de una versión publicada (specs/009,
// data-model §8, research D18): lo que cualquiera necesita para saber de
// dónde salió cada .deb y comprobarlo con `debfactory verify`.
type Provenance struct {
	Package        string       `json:"package"`
	Repository     string       `json:"repository"` // repositorio de paquetes (manifiesto y lockfile)
	Version        string       `json:"version"`
	Level          int          `json:"level"`
	Upstream       ProvUpstream `json:"upstream"`
	ManifestCommit string       `json:"manifest_commit"`
	RunURL         string       `json:"run_url,omitempty"`
	// Run es el run de GitHub Actions que compiló y atestó (modo hosted):
	// `debfactory verify` exige atestaciones de exactamente ese run.
	Run          *supply.Run       `json:"run,omitempty"`
	Attestations []ProvAttestation `json:"attestations"`
	Buildinfo    []string          `json:"buildinfo,omitempty"`
	Tests        []ProvTest        `json:"tests"`
	AuditSeq     int               `json:"audit_seq"`
	VerifiedByPC ProvVerified      `json:"verified_by_pc"`
	Supply       []supply.Report   `json:"supply,omitempty"`
}

// ProvUpstream son los archivos de upstream y su verificación.
type ProvUpstream struct {
	Files map[string]string `json:"files"` // URL -> sha256 (lockfile)
}

// ProvAttestation es un .deb publicado, su digest y dónde ver su atestación.
type ProvAttestation struct {
	Subject  string `json:"subject"`
	Digest   string `json:"digest"`
	RekorURL string `json:"rekor_url,omitempty"`
}

// ProvTest es el resultado de la prueba de una suite y arquitectura.
type ProvTest struct {
	Suite  string `json:"suite"`
	Arch   string `json:"arch"`
	Status string `json:"status"`
}

// ProvVerified dice qué comprobó la PC antes de firmar (research D17).
type ProvVerified struct {
	Mode          string `json:"mode"` // hosted (GitHub compila, la PC verifica) | local (todo en la PC)
	Attestations  bool   `json:"attestations"`
	Lintian       bool   `json:"lintian"`
	RebuildLevel1 bool   `json:"rebuild_level1,omitempty"`
	TwoVariants   bool   `json:"two_variants,omitempty"`
	SmokeAMD64    bool   `json:"smoke_amd64"`
}

// RekorURL es la búsqueda de un digest en el registro de transparencia.
func RekorURL(digest string) string { return "https://search.sigstore.dev/?hash=" + digest }

// ProvenancePath es la ruta de provenance.json de una versión en <data>.
func ProvenancePath(data, pkg, version string) string {
	return site.ProvenanceFile(data, pkg, version)
}

// buildProvenance reúne provenance.json de una versión ya publicada desde el
// manifiesto, el lockfile, el estado y los informes de dist/.
func buildProvenance(root string, m *manifest.Manifest, lk *lock.File, p repo.Published, commit, runURL string,
	seq int, run *supply.Run) (*Provenance, error) {
	hosted := run != nil
	entry := lk.Versions[m.Version]
	if entry == nil {
		return nil, fmt.Errorf("el lockfile no tiene %s", m.Version)
	}
	pv := &Provenance{Package: m.Name, Version: p.Version, Level: 1, ManifestCommit: commit, RunURL: runURL,
		AuditSeq: seq, Upstream: ProvUpstream{Files: map[string]string{}}, Attestations: []ProvAttestation{}, Tests: []ProvTest{}}
	pv.VerifiedByPC = ProvVerified{Mode: "local", Lintian: true, SmokeAMD64: true}
	if hosted {
		pv.VerifiedByPC.Mode, pv.VerifiedByPC.Attestations = "hosted", true
		pv.Run = run
	}
	dist := filepath.Join(root, "dist")
	att := func(file, digest string) {
		a := ProvAttestation{Subject: file, Digest: digest}
		if hosted {
			a.RekorURL = RekorURL(digest)
		}
		pv.Attestations = append(pv.Attestations, a)
	}
	tests := func(base string) {
		b, err := os.ReadFile(filepath.Join(dist, base+".test.json"))
		if err != nil {
			return
		}
		var tr installtest.Report
		if json.Unmarshal(b, &tr) != nil {
			return
		}
		for _, r := range tr.Results {
			pv.Tests = append(pv.Tests, ProvTest{Suite: r.Suite, Arch: tr.Arch, Status: r.Status})
		}
	}
	readSupply := func(name string) {
		var r supply.Report
		if b, err := os.ReadFile(filepath.Join(dist, name)); err == nil && json.Unmarshal(b, &r) == nil {
			pv.Supply = append(pv.Supply, r)
		}
	}
	if !p.Level2() {
		pv.VerifiedByPC.RebuildLevel1 = hosted
		for arch, sha := range p.Debs {
			if u, err := m.RenderURL(m.Artifact.URL, arch); err == nil {
				pv.Upstream.Files[u] = entry.Artifacts[arch]
			}
			att(m.DebBase(arch)+".deb", sha)
			tests(m.DebBase(arch))
			readSupply(m.DebBase(arch) + ".supply.json")
		}
	} else {
		pv.Level, pv.VerifiedByPC.TwoVariants = 2, true
		if u, err := m.RenderURL(m.Source.URL, ""); err == nil {
			pv.Upstream.Files[u] = entry.SourceSHA256
		}
		for _, su := range p.Suites {
			b := p.Builds[su]
			for arch, sha := range b.Debs {
				base := fmt.Sprintf("%s_%s_%s", m.Name, b.Version, arch)
				att(base+".deb", sha)
				pv.Buildinfo = append(pv.Buildinfo, base+".buildinfo")
				tests(base)
			}
		}
		readSupply(m.Name + "_" + m.Version + ".supply.json")
	}
	sort.Slice(pv.Attestations, func(i, j int) bool { return pv.Attestations[i].Subject < pv.Attestations[j].Subject })
	sort.Strings(pv.Buildinfo)
	sort.Slice(pv.Tests, func(i, j int) bool {
		if pv.Tests[i].Suite != pv.Tests[j].Suite {
			return pv.Tests[i].Suite < pv.Tests[j].Suite
		}
		return pv.Tests[i].Arch < pv.Tests[j].Arch
	})
	return pv, nil
}

// writeProvenance guarda provenance.json en <data> y devuelve su sha256, que
// queda en la entrada de auditoría (versionada en la rama de estado).
func writeProvenance(data string, pv *Provenance) (string, error) {
	b, err := json.MarshalIndent(pv, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	path := ProvenancePath(data, pv.Package, pv.Version)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return fetch.SHA256(b), nil
}

// provenance genera provenance.json de cada versión publicada o ampliada en
// esta ejecución y deja su sha256 en la entrada de auditoría, con el número
// de entrada que le dará AppendAudit.
func (x *run) provenance(entries []Entry, st *repo.State, commit string) error {
	if x.Repo == nil || x.Repo.DataDir == "" {
		return nil
	}
	_, old, err := ReadAudit(x.Root)
	if err != nil {
		return err
	}
	for i := range entries {
		e := &entries[i]
		p, ok := st.Find(e.Package, e.Version)
		if !ok {
			continue
		}
		m, err := manifest.Validate(x.Root, x.Cfg, e.Package)
		if err != nil {
			return err
		}
		lk, err := lock.Read(x.Root, e.Package)
		if err != nil {
			return err
		}
		run := ""
		if e.Run != nil {
			run = *e.Run
		}
		var hostedRun *supply.Run
		if x.accept != nil {
			r := x.accept.Run
			hostedRun = &r
		}
		pv, err := buildProvenance(x.Root, m, lk, p, e.Commit, run, len(old)+i+1, hostedRun)
		if err != nil {
			return err
		}
		pv.Repository = x.Cfg.CI.Repository
		if e.Provenance, err = writeProvenance(x.Repo.DataDir, pv); err != nil {
			return err
		}
	}
	return nil
}
