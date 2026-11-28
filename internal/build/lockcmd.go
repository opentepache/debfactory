package build

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"debfactory/internal/assemble"
	"debfactory/internal/fetch"
	"debfactory/internal/license"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	aptrepo "debfactory/internal/repo"
	"debfactory/internal/report"
)

// download es un archivo descargado por lock, aún sin cachear.
type download struct {
	path, sha, url string
}

// Lock genera o actualiza la entrada del lockfile para la versión del
// manifiesto (contracts/cli.md). Orden: validar → descargar → checksums
// upstream → gate de licencias → fecha del release → escribir. Solo entra en
// la caché lo que pasó todas las comprobaciones.
func (e *Env) Lock(ctx context.Context, name string, force bool) error {
	m, err := manifest.Validate(e.Root, e.Cfg, name)
	if err != nil {
		return err
	}
	e = e.forManifest(m)
	fail := func(stage report.Stage, arch string, err error) error {
		return report.Wrap(err, stage, m.Name, m.Version, arch)
	}
	lk, err := lock.Read(e.Root, m.Name)
	if err != nil {
		return fail(report.Lock, "", err)
	}
	tag, err := m.RenderedTag()
	if err != nil {
		return fail(report.Validate, "", err)
	}

	var pending []download // temporales que hay que cachear o borrar
	defer func() {
		for _, d := range pending {
			fetch.Discard(d.path)
		}
	}()
	get := func(url, arch string) (download, error) {
		p, sha, err := e.Fetcher.Get(ctx, url, "")
		if err != nil {
			return download{}, downloadErr(err, m, arch, "lock")
		}
		d := download{p, sha, url}
		pending = append(pending, d)
		return d, nil
	}

	entry := &lock.Entry{Artifacts: map[string]string{}}
	artifacts := map[string]download{}
	if m.Level2() {
		// Nivel 2: un solo tarball del código fuente para todas las
		// arquitecturas y suites (FR-010); la licencia sale de él o del repo.
		url, err := m.RenderURL(m.Source.URL, "")
		if err != nil {
			return fail(report.Validate, "", err)
		}
		d, err := get(url, "")
		if err != nil {
			return err
		}
		entry.Artifacts = nil
		entry.SourceSHA256 = d.sha
		artifacts[""] = d
	}
	for _, arch := range e.Cfg.Arches {
		if m.Level2() {
			break
		}
		url, err := m.RenderURL(m.Artifact.URL, arch)
		if err != nil {
			return fail(report.Validate, arch, err)
		}
		d, err := get(url, arch)
		if err != nil {
			return err
		}
		artifacts[arch], entry.Artifacts[arch] = d, d.sha
	}

	if m.Checksums != nil {
		url, err := m.RenderURL(m.Checksums.URL, "")
		if err != nil {
			return fail(report.Validate, "", err)
		}
		d, err := get(url, "")
		if err != nil {
			return err
		}
		data, err := os.ReadFile(d.path)
		if err != nil {
			return fail(report.Fetch, "", err)
		}
		sums, err := fetch.ParseChecksums(data)
		if err != nil {
			return fail(report.Verify, "", fmt.Errorf("%s: %v", url, err))
		}
		for _, arch := range e.Cfg.Arches {
			a := artifacts[arch]
			if err := fetch.VerifyChecksum(sums, url, a.url, a.sha); err != nil {
				return downloadErr(err, m, arch, "lock")
			}
		}
		entry.ChecksumsSHA256 = d.sha
	}

	files := map[string]string{}
	for _, d := range artifacts {
		files[d.url] = d.path
	}
	if m.Checksums != nil {
		for _, d := range pending {
			files[d.url] = d.path
		}
	}
	if entry.Signatures, err = e.verifySignatures(ctx, m, e.Cfg.Arches, files, nil); err != nil {
		return err
	}
	text, err := e.lockLicense(ctx, m, tag, artifacts, get)
	if err != nil {
		return err
	}
	entry.LicenseSHA256 = fetch.SHA256(text)
	check := e.LicenseCheck
	if check == nil {
		check = license.Gate
	}
	if err := check(text, m.License.SPDX, e.Cfg.WithLicenses(m.PackageExceptionLicenses()...)); err != nil {
		return fail(report.License, "", err)
	}

	rel, err := e.GitHub.Release(ctx, m.Upstream, tag)
	if err != nil {
		return fail(report.Lock, "", err)
	}
	entry.SourceDateEpoch = rel.PublishedAt.Unix()
	e.warnUnpinned(m, rel)
	if m.Level2() {
		if err := e.lockLevel2(ctx, m, entry, artifacts[""].path, get); err != nil {
			return err
		}
	}

	if old, ok := lk.Versions[m.Version]; ok && !old.Equal(entry) && !force {
		return &report.Error{Package: m.Name, Version: m.Version, Stage: report.Lock,
			Cause: fmt.Sprintf("los hashes de %s cambiaron respecto al lockfile (usa --force si el cambio es legítimo)",
				m.Version),
			Detail: diff(old, entry)}
	}
	for _, d := range pending {
		if _, err := e.Fetcher.Commit(d.path, d.sha); err != nil {
			return fail(report.Fetch, "", err)
		}
	}
	pending = nil
	lk.Versions[m.Version] = entry
	if err := lk.Write(e.Root, m.Name); err != nil {
		return fail(report.Lock, "", err)
	}
	e.Out.Result(m.Name, m.Version, "", "lock", lock.PathIn(e.Root, m.Name))
	return nil
}

// lockLicense obtiene el texto de licencia de la versión exacta: del repo al
// tag (se cachea con el resto) o de dentro del artefacto, que ya está
// verificado; en ese caso el texto debe ser idéntico en todas las
// arquitecturas.
func (e *Env) lockLicense(ctx context.Context, m *manifest.Manifest, tag string,
	artifacts map[string]download, get func(url, arch string) (download, error)) ([]byte, error) {
	fail := func(err error) error { return report.Wrap(err, report.License, m.Name, m.Version, "") }
	if m.License.File.Repo != "" {
		vars, err := m.Vars("")
		if err != nil {
			return nil, fail(err)
		}
		p, err := manifest.Render(m.License.File.Repo, vars)
		if err != nil {
			return nil, fail(err)
		}
		d, err := get(fetch.LicenseURL(m.Upstream, tag, p), "")
		if err != nil {
			return nil, err
		}
		return os.ReadFile(d.path)
	}
	if m.Level2() {
		vars, err := m.Vars("")
		if err != nil {
			return nil, fail(err)
		}
		p, err := manifest.Render(m.License.File.Archive, vars)
		if err != nil {
			return nil, fail(err)
		}
		t, err := assemble.ExtractFile(artifacts[""].path, m.Source.Format, p)
		if err != nil {
			return nil, fail(fmt.Errorf("license.file.archive: %v", err))
		}
		return t, nil
	}
	var text []byte
	for _, arch := range e.Cfg.Arches {
		vars, err := m.Vars(arch)
		if err != nil {
			return nil, fail(err)
		}
		p, err := manifest.Render(m.License.File.Archive, vars)
		if err != nil {
			return nil, fail(err)
		}
		t, err := assemble.ExtractFile(artifacts[arch].path, m.Artifact.Format, p)
		if err != nil {
			return nil, fail(fmt.Errorf("license.file.archive: %v", err))
		}
		if text != nil && string(t) != string(text) {
			return nil, fail(fmt.Errorf("el texto de %s difiere entre arquitecturas", p))
		}
		text = t
	}
	return text, nil
}

// diff lista los campos que cambian entre dos entradas del lockfile.
func diff(old, new *lock.Entry) []string {
	var out []string
	add := func(field, a, b string) {
		if a != b {
			out = append(out, fmt.Sprintf("%s: %s → %s", field, a, b))
		}
	}
	add("source_date_epoch", fmt.Sprint(old.SourceDateEpoch), fmt.Sprint(new.SourceDateEpoch))
	add("license_sha256", old.LicenseSHA256, new.LicenseSHA256)
	add("checksums_sha256", old.ChecksumsSHA256, new.ChecksumsSHA256)
	add("source_sha256", old.SourceSHA256, new.SourceSHA256)
	add("tag_archive_sha256", old.TagArchiveSHA256, new.TagArchiveSHA256)
	add("zigdeps_sha256", old.ZigdepsSHA256, new.ZigdepsSHA256)
	for _, k := range unionKeys(old.Toolchain, new.Toolchain) {
		add("toolchain."+k, old.Toolchain[k], new.Toolchain[k])
	}
	for _, k := range unionKeys(old.ExtraFiles, new.ExtraFiles) {
		add("extra_files."+k, old.ExtraFiles[k], new.ExtraFiles[k])
	}
	if !slices.Equal(old.Deps, new.Deps) {
		out = append(out, fmt.Sprintf("deps: %d → %d dependencias o hashes distintos", len(old.Deps), len(new.Deps)))
	}
	arches := map[string]bool{}
	for a := range old.Artifacts {
		arches[a] = true
	}
	for a := range new.Artifacts {
		arches[a] = true
	}
	keys := make([]string, 0, len(arches))
	for a := range arches {
		keys = append(keys, a)
	}
	sort.Strings(keys)
	for _, a := range keys {
		add("artifacts."+a, old.Artifacts[a], new.Artifacts[a])
	}
	return out
}

// Prune borra del lockfile todas las versiones salvo la del manifiesto.
func (e *Env) Prune(name string) error {
	m, err := manifest.Validate(e.Root, e.Cfg, name)
	if err != nil {
		return err
	}
	lk, err := lock.Read(e.Root, m.Name)
	if err != nil {
		return report.Wrap(err, report.Lock, m.Name, m.Version, "")
	}
	st, err := aptrepo.ReadState(e.Root)
	if err != nil {
		return report.Wrap(err, report.Lock, m.Name, m.Version, "")
	}
	var published []string
	for v := range lk.Versions {
		if v == m.Version {
			continue
		}
		for _, p := range st.Packages[m.Name] {
			if strings.HasPrefix(p.Version, v+"-0~tepache") {
				published = append(published, v)
				break
			}
		}
	}
	if len(published) > 0 {
		sort.Strings(published)
		return &report.Error{Package: m.Name, Version: m.Version, Stage: report.Lock,
			Cause: fmt.Sprintf("%s sigue publicada; retírala o déjala caer por retención antes de --prune",
				strings.Join(published, ", "))}
	}
	removed := 0
	for v := range lk.Versions {
		if v != m.Version {
			delete(lk.Versions, v)
			removed++
		}
	}
	if err := lk.Write(e.Root, m.Name); err != nil {
		return report.Wrap(err, report.Lock, m.Name, m.Version, "")
	}
	e.Out.Result(m.Name, m.Version, "", "prune", fmt.Sprintf("versiones borradas: %d", removed))
	return nil
}

// unionKeys son las claves de a y b, ordenadas.
func unionKeys(a, b map[string]string) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
