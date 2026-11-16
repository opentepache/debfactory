//go:build integration

package repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"debfactory/internal/sign"
)

// TestExpiryWarning: aviso a menos de warn_days y fallo si ya caducó
// (FR-023). La llave de prueba caduca en 2 años; se mueve el reloj.
func TestExpiryWarning(t *testing.T) {
	ctx := context.Background()
	e, out := testEnv(t)
	exp, err := sign.Expiry(ctx, sign.Home(e.DataDir), e.Cfg.Repo.Signing.Subkey)
	if err != nil || exp.IsZero() {
		t.Fatalf("expiry: %v %v", exp, err)
	}
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.0.0-0~tepache1"))
	e.Now = func() time.Time { return exp.Add(-30 * 24 * time.Hour) }
	if err := e.Publish(ctx, []string{"foo"}, map[string]string{"foo": "1.0.0-0~tepache1"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "aviso: la subllave de firma caduca el "+exp.UTC().Format(time.DateOnly)+" (en 29 días)") &&
		!strings.Contains(out.String(), "(en 30 días)") {
		t.Errorf("sin aviso:\n%s", out)
	}
	e.Now = func() time.Time { return exp.Add(time.Hour) }
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.1.0-0~tepache1"))
	err = e.Publish(ctx, []string{"foo"}, map[string]string{"foo": "1.1.0-0~tepache1"}, false)
	if err == nil || !strings.Contains(err.Error(), "sign: la subllave") || !strings.Contains(err.Error(), "caducó el") {
		t.Fatalf("subllave caducada: %v", err)
	}
}

// TestValidUntil: cada Release lleva Valid-Until = firma + repo.validity.days,
// verify falla si ya caducó y avisa (evento expiry) si quedan menos de
// repo.validity.warn_days (FR-030 y FR-032 de la fase 003).
func TestValidUntil(t *testing.T) {
	ctx := context.Background()
	e, out := testEnv(t)
	signed := time.Now().UTC().Truncate(time.Second)
	e.Now = func() time.Time { return signed }
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.0.0-0~tepache1"))
	if err := e.Publish(ctx, []string{"foo"}, map[string]string{"foo": "1.0.0-0~tepache1"}, false); err != nil {
		t.Fatal(err)
	}
	cur, err := Current(e.DataDir)
	if err != nil || cur == nil {
		t.Fatalf("current: %v", err)
	}
	vu, err := TreeValidUntil(e.Cfg, cur.Dir(e.DataDir))
	if want := signed.AddDate(0, 0, e.Cfg.Repo.Validity.Days); err != nil || !vu.Equal(want) {
		t.Fatalf("Valid-Until = %v (%v); se esperaba %v", vu, err, want)
	}

	st, err := ReadState(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(now time.Time) error {
		out.Reset()
		if err := VerifyTree(ctx, e.Cfg, st, cur.Dir(e.DataDir), now); err != nil {
			return err
		}
		WarnValidity(e.Out, e.Cfg, "la generación local "+cur.Name, vu, now)
		return nil
	}
	if err := verify(signed); err != nil || strings.Contains(out.String(), "caduca el") {
		t.Errorf("con margen: %v\n%s", err, out)
	}
	days := e.Cfg.Repo.Validity.Days
	if err := verify(signed.AddDate(0, 0, days-6).Add(-time.Hour)); err != nil ||
		!strings.Contains(out.String(), "la generación local "+cur.Name+" caduca el "+vu.Format(time.DateOnly)+" (en 6 días)") {
		t.Errorf("aviso a 6 días: %v\n%s", err, out)
	}
	err = verify(signed.AddDate(0, 0, days+1))
	if err == nil || !strings.Contains(err.Error(), "la generación caducó el "+vu.Format(time.DateOnly)) {
		t.Errorf("caducada: err = %v", err)
	}
}
