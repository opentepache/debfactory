package supply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Run identifica el run de GitHub Actions cuyos artefactos acepta la PC
// (specs/009, research D6 y D17).
type Run struct {
	Repo     string `json:"repo"`     // opentepache/packages
	Workflow string `json:"workflow"` // .github/workflows/main.yml (o pr.yml)
	Ref      string `json:"ref"`      // refs/heads/main
	Commit   string `json:"commit"`   // sha del commit construido
}

// Identity es la identidad exacta del certificado de ese workflow.
func (r Run) Identity() string {
	return "https://github.com/" + r.Repo + "/" + r.Workflow + "@" + r.Ref
}

// AttestError: un artefacto sin atestación válida del run esperado.
type AttestError struct {
	File   string
	Reason string
}

func (e *AttestError) Error() string {
	return fmt.Sprintf("atestación de %s: %s", filepath.Base(e.File), e.Reason)
}

// VerifyRun exige que file (con ese sha256) tenga una atestación SLSA v1 del
// run: emisor de GitHub Actions, identidad exacta del workflow y ref, mismo
// repositorio y el mismo commit (sourceRepositoryDigest). Sin atestación, o
// si ninguna verifica, falla.
func (a *Attestor) VerifyRun(ctx context.Context, run Run, file, sha256 string) error {
	for _, v := range []string{run.Repo, run.Workflow, run.Ref, run.Commit} {
		if v == "" {
			return &AttestError{File: file, Reason: "run incompleto (repositorio, workflow, ref y commit)"}
		}
	}
	bundles, err := a.List(ctx, run.Repo, sha256)
	if err != nil {
		return err
	}
	cosign := a.Cosign
	if cosign == nil {
		cosign = runCosign
	}
	var reasons []string
	found := false
	for _, b := range bundles {
		if predicateType(b) != SLSAProvenanceV1 {
			continue
		}
		found = true
		f, err := os.CreateTemp("", "debfactory-attestation-*.json")
		if err != nil {
			return err
		}
		_, werr := f.Write(b)
		f.Close()
		if werr != nil {
			os.Remove(f.Name())
			return werr
		}
		err = cosign(ctx, file, f.Name(), RunArgs(run))
		os.Remove(f.Name())
		if err == nil {
			return nil
		}
		reasons = append(reasons, err.Error())
	}
	if !found {
		return &AttestError{File: file, Reason: "no tiene atestación SLSA en " + run.Repo}
	}
	return &AttestError{File: file, Reason: "ninguna atestación es de " + run.Identity() + " en el commit " +
		run.Commit + " (" + strings.Join(reasons, "; ") + ")"}
}

// RunArgs son los argumentos de cosign para una atestación de nuestro run.
func RunArgs(run Run) []string {
	return []string{"verify-blob-attestation", "--new-bundle-format", "--type", "slsaprovenance1",
		"--certificate-oidc-issuer", githubIssuer,
		"--certificate-identity", run.Identity(),
		"--certificate-github-workflow-repository", run.Repo,
		"--certificate-github-workflow-ref", run.Ref,
		"--certificate-github-workflow-sha", run.Commit}
}
