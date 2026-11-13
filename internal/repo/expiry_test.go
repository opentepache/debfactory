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
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.0.0-0~pav1"))
	e.Now = func() time.Time { return exp.Add(-30 * 24 * time.Hour) }
	if err := e.Publish(ctx, []string{"foo"}, map[string]string{"foo": "1.0.0-0~pav1"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "aviso: la subllave de firma caduca el "+exp.UTC().Format(time.DateOnly)+" (en 29 días)") &&
		!strings.Contains(out.String(), "(en 30 días)") {
		t.Errorf("sin aviso:\n%s", out)
	}
	e.Now = func() time.Time { return exp.Add(time.Hour) }
	writeRelease(t, e.Root, e.suites(), fakeRelease(t, "foo", "1.1.0-0~pav1"))
	err = e.Publish(ctx, []string{"foo"}, map[string]string{"foo": "1.1.0-0~pav1"}, false)
	if err == nil || !strings.Contains(err.Error(), "sign: la subllave") || !strings.Contains(err.Error(), "caducó el") {
		t.Fatalf("subllave caducada: %v", err)
	}
}
