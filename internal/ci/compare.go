package ci

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"debfactory/internal/build"
	"debfactory/internal/fetch"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
)

// CompareArtifacts compara los artefactos de los jobs hospedados de un PR
// con lo publicado en la base (specs/009): cada paquete de plan.Compare
// (cambio de herramienta, o de manifiesto o lockfile sin subir revision)
// MUST dar exactamente los sha256 publicados; en nivel 2, además, las dos
// variantes iguales. Es el equivalente hospedado de la comparación de
// ci pr de la fase 003.
func (r *Runner) CompareArtifacts(ctx context.Context, o Options) error {
	if o.Base == "" || o.Artifacts == "" {
		return &InputError{"ci compare necesita --base REF y --artifacts DIR"}
	}
	st, err := stateAt(ctx, r.Root, o.Base)
	if err != nil {
		return err
	}
	p, err := PlanPR(ctx, r.Root, r.Cfg, o.Base, st)
	if err != nil {
		return err
	}
	var errs report.Errors
	for _, c := range p.Compare {
		pub, ok := st.Find(c.Package, c.Version)
		if !ok {
			continue
		}
		m, err := manifest.Validate(r.Root, r.Cfg, c.Package)
		if err != nil {
			errs = append(errs, &report.Error{Package: c.Package, Version: c.Version, Stage: report.Validate, Cause: err.Error()})
			continue
		}
		got, missing := artifactShas(o.Artifacts, m, pub.Suites, pub.ArchList(r.Cfg.Arches))
		for _, miss := range missing {
			errs = append(errs, &report.Error{Package: c.Package, Version: c.Version, Arch: miss.where, Stage: miss.stage, Cause: miss.cause})
		}
		if len(missing) > 0 {
			continue
		}
		errs = append(errs, compareShas(c.Package, c.Version, got, pub, pub.ArchList(r.Cfg.Arches), " (compilado en el PR)")...)
		if len(errs) == 0 {
			r.Out.Line("compare: %s %s: igual que lo publicado", c.Package, c.Version)
		}
	}
	return errs.Err()
}

type missingArtifact struct {
	where, cause string
	stage        report.Stage
}

// artifactShas lee los sha256 de los artefactos con las claves de distShas.
func artifactShas(dir string, m *manifest.Manifest, suites, arches []string) (map[string]string, []missingArtifact) {
	got := map[string]string{}
	var missing []missingArtifact
	if !m.Level2() {
		for _, arch := range arches {
			sha, err := fetch.SHA256File(filepath.Join(dir, l1Artifact(m.Name, arch), m.DebBase(arch)+".deb"))
			if err != nil {
				missing = append(missing, missingArtifact{arch, "falta el artefacto " + l1Artifact(m.Name, arch), report.Inputs})
				continue
			}
			got[arch] = sha
		}
		return got, missing
	}
	var sr build.SourceResult
	if b, err := os.ReadFile(filepath.Join(dir, sourceArtifact(m.Name), "source.json")); err != nil || json.Unmarshal(b, &sr) != nil {
		return nil, []missingArtifact{{"", "falta source-" + m.Name + "/source.json", report.Inputs}}
	}
	for _, su := range suites {
		if ss, ok := sr.Suites[su]; ok {
			got[su+"/dsc"] = ss.DSCSHA256
		}
		for _, arch := range arches {
			var hb [2]build.HostedBuild
			ok := true
			for v := range 2 {
				b, err := os.ReadFile(filepath.Join(dir, buildArtifact(m.Name, su, arch, v), "build.json"))
				if err != nil || json.Unmarshal(b, &hb[v]) != nil {
					missing = append(missing, missingArtifact{su + "/" + arch, "falta " + buildArtifact(m.Name, su, arch, v), report.Inputs})
					ok = false
					continue
				}
				// El build.json debe corresponder al .deb del artefacto.
				if sha, err := fetch.SHA256File(filepath.Join(dir, buildArtifact(m.Name, su, arch, v), hb[v].Deb)); err != nil || sha != hb[v].SHA256 {
					missing = append(missing, missingArtifact{su + "/" + arch, fmt.Sprintf("%s: el .deb no es el de build.json", buildArtifact(m.Name, su, arch, v)), report.Inputs})
					ok = false
				}
			}
			if !ok {
				continue
			}
			if hb[0].SHA256 != hb[1].SHA256 {
				missing = append(missing, missingArtifact{su + "/" + arch, "no reproducible: las dos variantes dan " + hb[0].SHA256 + " y " + hb[1].SHA256, report.Compare})
				continue
			}
			got[su+"/"+arch] = hb[0].SHA256
		}
	}
	return got, missing
}
