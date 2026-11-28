package ci

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"debfactory/internal/build"
	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/repo"
	"debfactory/internal/report"
)

// distShas devuelve los sha256 de lo que build dejó en root/dist para m: en
// nivel 1, arch -> .deb; en nivel 2 (specs/008-source-builds),
// "<suite>/<arch>" -> .deb y "<suite>/dsc" -> .dsc de cada suite no excluida.
func distShas(root string, cfg *config.Config, m *manifest.Manifest) (map[string]string, error) {
	shas := map[string]string{}
	sha := func(key, file string) error {
		s, err := fetch.SHA256File(filepath.Join(root, "dist", file))
		if err != nil {
			return err
		}
		shas[key] = s
		return nil
	}
	if !m.Level2() {
		for _, arch := range m.Arches(cfg.Arches) {
			if err := sha(arch, m.DebBase(arch)+".deb"); err != nil {
				return nil, err
			}
		}
		return shas, nil
	}
	for _, su := range build.Level2Suites(cfg, m) {
		if err := sha(su.Name+"/dsc", repo.DscFile(m.Name, m.SuiteVersion(su.Level2Suffix))); err != nil {
			return nil, err
		}
		for _, arch := range m.Arches(cfg.Arches) {
			if err := sha(su.Name+"/"+arch, build.Level2Base(m, su, arch)+".deb"); err != nil {
				return nil, err
			}
		}
	}
	return shas, nil
}

// publishedShas son los sha256 publicados de p con las claves de distShas.
func publishedShas(p repo.Published, arches []string) map[string]string {
	shas := map[string]string{}
	if !p.Level2() {
		for _, arch := range arches {
			shas[arch] = p.Debs[arch]
		}
		return shas
	}
	for _, su := range p.Suites {
		b := p.Builds[su]
		shas[su+"/dsc"] = b.DSC
		for _, arch := range arches {
			shas[su+"/"+arch] = b.Debs[arch]
		}
	}
	return shas
}

// compareShas compara lo reconstruido con lo publicado: cada archivo
// publicado debe salir idéntico (en nivel 2, en cada suite ya publicada; las
// suites nuevas de una ampliación no se comparan). Devuelve un error por
// diferencia, con why como final del motivo.
func compareShas(pkg, version string, got map[string]string, pub repo.Published, arches []string, why string) report.Errors {
	want := publishedShas(pub, arches)
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var errs report.Errors
	for _, k := range keys {
		if got[k] == want[k] {
			continue
		}
		what := ".deb"
		if pub.Level2() && filepath.Base(k) == "dsc" {
			what = ".dsc"
		}
		errs = append(errs, &report.Error{Package: pkg, Version: version, Arch: k, Stage: report.Compare,
			Cause: fmt.Sprintf("el %s reconstruido (sha256 %s…) no es el publicado (%s…)%s", what, sha12(got[k]), sha12(want[k]), why)})
	}
	return errs
}

// extendEntry es la entrada de auditoría de una ampliación: en nivel 2 lleva
// el .orig y solo las compilaciones de las suites agregadas.
func extendEntry(e Entry, p repo.Published, added []string) Entry {
	if !p.Level2() {
		e.Debs = p.Debs
		if len(e.Arches) > 0 { // ampliación por arquitectura: solo los .deb nuevos
			e.Debs = map[string]string{}
			for _, a := range e.Arches {
				e.Debs[a] = p.Debs[a]
			}
		}
		return e
	}
	e.Orig = p.Orig
	e.Builds = map[string]repo.Build{}
	for _, su := range added {
		e.Builds[su] = p.Builds[su]
	}
	for _, su := range p.Suites { // arquitecturas nuevas en las suites que ya estaban
		if len(e.Arches) == 0 || slices.Contains(added, su) {
			continue
		}
		b := p.Builds[su]
		nb := repo.Build{Version: b.Version, DSC: b.DSC, DebianTar: b.DebianTar, Debs: map[string]string{}, Buildinfo: map[string]string{}}
		for _, a := range e.Arches {
			nb.Debs[a], nb.Buildinfo[a] = b.Debs[a], b.Buildinfo[a]
		}
		e.Builds[su] = nb
	}
	return e
}

// publishEntry completa los archivos de una entrada publish.
func publishEntry(e Entry, p repo.Published) Entry {
	if !p.Level2() {
		e.Debs = p.Debs
		return e
	}
	e.Orig, e.Builds = p.Orig, p.Builds
	return e
}
