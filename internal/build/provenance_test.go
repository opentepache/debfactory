package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	"debfactory/internal/lock"
	"debfactory/internal/supply"
)

// fakeProvenance simula las atestaciones de upstream: present indica si hay
// procedencia SLSA y ok si verifica con repositorio y tag.
func fakeProvenance(present, ok *bool) func(context.Context, string, string, string, string) (*supply.Provenance, error) {
	return func(_ context.Context, repo, tag, file, _ string) (*supply.Provenance, error) {
		p := &supply.Provenance{Present: *present, OK: *present && *ok}
		if *present {
			p.Count = 1
		}
		if *present && !*ok {
			return p, &supply.ProvenanceError{Repo: repo, Tag: tag, File: file}
		}
		return p, nil
	}
}

// TestProvenanceFlow (FR-016, SC-003): lock registra la procedencia
// verificada; un build en el que desaparece falla en la etapa provenance; una
// procedencia que no verifica hace fallar lock; nada llega a dist/.
func TestProvenanceFlow(t *testing.T) {
	ctx := context.Background()
	present, ok := true, true
	h := newHarness(t)
	h.loadOK()
	h.env.Provenance = fakeProvenance(&present, &ok)
	if err := h.env.Lock(ctx, "example", true); err != nil {
		t.Fatalf("%v\n%s", err, h.errb.String())
	}
	lk, _ := lock.Read(h.root, "example")
	if n := len(lk.Versions["1.0.0"].Provenance); n != 2 {
		t.Fatalf("provenance = %v", lk.Versions["1.0.0"].Provenance)
	}

	// La atestación desaparece: el build no la puede perder.
	present = false
	err := h.env.Build(ctx, "example", Options{})
	if err == nil || !strings.Contains(err.Error(), ": provenance:") || !strings.Contains(err.Error(), "ya no la tiene") {
		t.Fatalf("sin atestación: %v", err)
	}
	if d := h.distFiles(); len(d) != 0 {
		t.Errorf("dist/ = %v", d)
	}

	// Procedencia de otro repositorio u otro tag: lock falla.
	other := newHarness(t)
	other.loadOK()
	present, ok = true, false
	other.env.Provenance = fakeProvenance(&present, &ok)
	err = other.env.Lock(ctx, "example", true)
	if err == nil || !strings.Contains(err.Error(), ": provenance:") {
		t.Fatalf("ajena: %v", err)
	}
}

// TestProvenanceAPIUnavailable: si la API de GitHub no responde (límite de
// peticiones), un build sigue con un aviso cuando el lockfile no exige la
// procedencia, y falla cuando la exige; lock falla siempre.
func TestProvenanceAPIUnavailable(t *testing.T) {
	ctx := context.Background()
	down := func(context.Context, string, string, string, string) (*supply.Provenance, error) {
		return nil, errors.New("API de GitHub: atestaciones de example/example: HTTP 403")
	}
	h := newHarness(t)
	h.loadOK()
	if err := h.env.Lock(ctx, "example", true); err != nil {
		t.Fatal(err)
	}
	h.env.Provenance = down
	if err := h.env.Build(ctx, "example", Options{}); err != nil {
		t.Fatalf("build sin procedencia exigida: %v", err)
	}
	if !strings.Contains(h.out.String(), "no se pudo consultar la procedencia") {
		t.Errorf("sin aviso:\n%s", h.out.String())
	}
	if err := h.env.Lock(ctx, "example", true); err == nil || !strings.Contains(err.Error(), ": provenance:") {
		t.Errorf("lock con la API caída: %v", err)
	}

	// Con procedencia verificada en el lockfile: el build falla.
	v := newHarness(t)
	v.loadOK()
	present, ok := true, true
	v.env.Provenance = fakeProvenance(&present, &ok)
	if err := v.env.Lock(ctx, "example", true); err != nil {
		t.Fatal(err)
	}
	v.env.Provenance = down
	if err := v.env.Build(ctx, "example", Options{}); err == nil || !strings.Contains(err.Error(), ": provenance:") {
		t.Errorf("build con procedencia exigida: %v", err)
	}
}
