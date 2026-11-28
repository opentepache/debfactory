package build

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"debfactory/internal/fetch"
	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
	"debfactory/internal/supply"
)

// sigSuffixes son las extensiones de firma: lo firmado es la URL sin ella.
var sigSuffixes = []string{".minisig", ".sigstore.json", ".bundle", ".sig", ".asc"}

// SignedTarget es la URL que firma sigURL: la misma sin su extensión de firma.
func SignedTarget(sigURL string) (string, bool) {
	for _, s := range sigSuffixes {
		if t, ok := strings.CutSuffix(sigURL, s); ok {
			return t, true
		}
	}
	return "", false
}

// signatureURLs son las URLs de firma del manifiesto: una por arquitectura si
// la plantilla usa {{arch}}, si no una sola.
func signatureURLs(m *manifest.Manifest, arches []string) ([]string, error) {
	tmpl := m.Signing.URL()
	if !strings.Contains(tmpl, "{{arch}}") || m.Level2() {
		u, err := m.RenderURL(tmpl, "")
		return []string{u}, err
	}
	var out []string
	for _, a := range arches {
		u, err := m.RenderURL(tmpl, a)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// verifySignatures comprueba la firma fijada de upstream (FR-015) sobre los
// archivos ya descargados (URL -> ruta). expected son los sha256 de las
// firmas en el lockfile (nil en lock, que los calcula). Devuelve los sha256
// de las firmas para el lockfile.
func (e *Env) verifySignatures(ctx context.Context, m *manifest.Manifest, arches []string, files map[string]string,
	expected map[string]string) (map[string]string, error) {
	if m.Signing == nil {
		return nil, nil
	}
	fail := func(err error) error { return report.Wrap(err, report.Stage("signature"), m.Name, m.Version, "") }
	urls, err := signatureURLs(m, arches)
	if err != nil {
		return nil, fail(err)
	}
	v := &supply.Verifier{Root: e.Root, Pkg: m.Name, Sigstore: e.Sigstore}
	out := map[string]string{}
	for _, u := range urls {
		target, ok := SignedTarget(u)
		if !ok {
			return nil, fail(fmt.Errorf("%s: extensión de firma desconocida (%s)", u, strings.Join(sigSuffixes, ", ")))
		}
		file, ok := files[target]
		if !ok {
			return nil, fail(fmt.Errorf("la firma %s no corresponde a ningún archivo descargado (%s)", u, target))
		}
		want := ""
		if expected != nil {
			if want = expected[u]; want == "" {
				return nil, fail(fmt.Errorf("falta signatures.%s en %s; ejecuta `debfactory lock %s`", u, lock.PathIn(e.Root, m.Name), m.Name))
			}
		}
		sigPath, sha, err := e.Fetcher.Get(ctx, u, want)
		if err != nil {
			return nil, downloadErr(err, m, "", "lock")
		}
		if err := v.Verify(ctx, m.Signing, file, sigPath); err != nil {
			if want == "" {
				fetch.Discard(sigPath)
			}
			return nil, fail(err)
		}
		if want == "" { // lock: la firma verificada entra en la caché
			if _, err := e.Fetcher.Commit(sigPath, sha); err != nil {
				return nil, fail(err)
			}
		}
		out[u] = sha
	}
	return out, nil
}

// warnUnpinned avisa si el release de upstream trae archivos de firma y el
// manifiesto no fija la llave (FR-015).
func (e *Env) warnUnpinned(m *manifest.Manifest, rel *fetch.Release) {
	if m.Signing != nil || rel == nil {
		return
	}
	var sigs []string
	for _, a := range rel.Assets {
		if slices.ContainsFunc(sigSuffixes, func(s string) bool { return strings.HasSuffix(a, s) }) {
			sigs = append(sigs, a)
		}
	}
	if len(sigs) > 0 {
		e.Out.Result(m.Name, m.Version, "", "warning",
			fmt.Sprintf("upstream firma (%s) y el manifiesto no fija signing (constitución III)", strings.Join(sigs, ", ")))
	}
}
