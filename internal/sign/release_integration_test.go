//go:build integration

package sign

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"debfactory/internal/testutil"
)

// TestSignRelease: InRelease y Release.gpg verifican con gpgv y el keyring
// público, y conservan el Valid-Until.
func TestSignRelease(t *testing.T) {
	ctx := context.Background()
	home, master, sub := testutil.TestKey(t)
	dir := t.TempDir()
	orig, _ := os.ReadFile("../../testdata/repo/release/Release")
	os.WriteFile(filepath.Join(dir, "Release"), orig, 0o644)
	if err := AddValidUntil(filepath.Join(dir, "Release"), time.Now(), 30); err != nil {
		t.Fatal(err)
	}
	if err := SignRelease(ctx, home, sub, dir); err != nil {
		t.Fatal(err)
	}
	kr := filepath.Join(t.TempDir(), "k.gpg")
	if err := ExportKeyring(ctx, home, []string{master}, kr); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--keyring", kr, filepath.Join(dir, "InRelease")},
		{"--keyring", kr, filepath.Join(dir, "Release.gpg"), filepath.Join(dir, "Release")},
	} {
		if out, err := exec.Command("gpgv", args...).CombinedOutput(); err != nil {
			t.Errorf("gpgv %v: %v\n%s", args, err, out)
		}
	}
	in, _ := os.ReadFile(filepath.Join(dir, "InRelease"))
	if vu, err := ValidUntil(in); err != nil || vu.IsZero() {
		t.Errorf("InRelease sin Valid-Until: %v", err)
	}
}
