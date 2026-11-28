package supply

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SLSAProvenanceV1 es el tipo de predicado de procedencia que se verifica.
const SLSAProvenanceV1 = "https://slsa.dev/provenance/v1"

// githubIssuer emite los certificados de los workflows de GitHub Actions.
const githubIssuer = "https://token.actions.githubusercontent.com"

// Provenance es el resultado de comprobar la procedencia de upstream.
type Provenance struct {
	Present bool `json:"present"` // upstream publica procedencia SLSA para el archivo
	OK      bool `json:"ok"`      // al menos una verificó con repositorio y tag correctos
	Count   int  `json:"count"`   // atestaciones SLSA encontradas
}

// ProvenanceError: upstream publica procedencia para el archivo pero ninguna
// verifica con su repositorio y tag (otra fuente, otro tag, firma inválida).
type ProvenanceError struct {
	Repo, Tag, File string
	Detail          []string
}

func (e *ProvenanceError) Error() string {
	return fmt.Sprintf("procedencia de %s: ninguna atestación SLSA verifica con %s y refs/tags/%s",
		filepath.Base(e.File), e.Repo, e.Tag)
}

// Attestor consulta y verifica la procedencia de upstream (FR-016,
// research D11).
type Attestor struct {
	// List devuelve los bundles de las atestaciones del repositorio para el
	// digest (fetch.GitHub.Attestations).
	List func(ctx context.Context, repo, sha256 string) ([]json.RawMessage, error)
	// Cosign verifica un bundle con estos argumentos (por defecto, el cosign
	// de Debian en un contenedor).
	Cosign func(ctx context.Context, file, bundle string, args []string) error
}

// Check consulta las atestaciones de file (con ese sha256) y verifica las de
// tipo SLSA v1 exigiendo el emisor de GitHub Actions, el repositorio de origen
// y el ref del tag. La identidad del firmante se acepta de cualquier workflow
// de GitHub: proyectos como aqua firman desde un workflow reutilizable de otro
// repositorio, así que lo que se exige son las extensiones de origen del
// certificado. Sin atestaciones SLSA: Present=false, sin error.
func (a *Attestor) Check(ctx context.Context, repo, tag, file, sha256 string) (*Provenance, error) {
	bundles, err := a.List(ctx, repo, sha256)
	if err != nil {
		return nil, err
	}
	p := &Provenance{}
	cosign := a.Cosign
	if cosign == nil {
		cosign = runCosign
	}
	var detail []string
	for _, b := range bundles {
		if predicateType(b) != SLSAProvenanceV1 {
			continue
		}
		p.Present = true
		p.Count++
		f, err := os.CreateTemp("", "debfactory-attestation-*.json")
		if err != nil {
			return nil, err
		}
		_, werr := f.Write(b)
		f.Close()
		if werr != nil {
			os.Remove(f.Name())
			return nil, werr
		}
		err = cosign(ctx, file, f.Name(), []string{"verify-blob-attestation", "--new-bundle-format",
			"--type", "slsaprovenance1",
			"--certificate-oidc-issuer", githubIssuer,
			"--certificate-identity-regexp", "^https://github.com/",
			"--certificate-github-workflow-repository", repo,
			"--certificate-github-workflow-ref", "refs/tags/" + tag})
		os.Remove(f.Name())
		if err == nil {
			p.OK = true
			return p, nil
		}
		detail = append(detail, err.Error())
	}
	if p.Present {
		return p, &ProvenanceError{Repo: repo, Tag: tag, File: file, Detail: detail}
	}
	return p, nil
}

// predicateType lee el tipo de predicado del sobre DSSE de un bundle.
func predicateType(bundle []byte) string {
	var b struct {
		DSSE struct {
			Payload string `json:"payload"`
		} `json:"dsseEnvelope"`
	}
	if json.Unmarshal(bundle, &b) != nil {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(b.DSSE.Payload)
	if err != nil {
		return ""
	}
	var st struct {
		PredicateType string `json:"predicateType"`
	}
	if json.Unmarshal(raw, &st) != nil {
		return ""
	}
	return strings.TrimSpace(st.PredicateType)
}
