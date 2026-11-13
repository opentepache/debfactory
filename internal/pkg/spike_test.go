package pkg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goreleaser/nfpm/v2"
	"github.com/goreleaser/nfpm/v2/deb"
	"github.com/goreleaser/nfpm/v2/files"
)

// TestNfpmReproducible es el spike de research.md §3 riesgo 1: nfpm produce el
// mismo .deb byte a byte desde directorios de trabajo distintos, y la versión
// sale como <upstream>-0~tepache<N>.
func TestNfpmReproducible(t *testing.T) {
	mtime := time.Unix(1772810627, 0).UTC()
	build := func() []byte {
		dir := t.TempDir()
		bin := filepath.Join(dir, "tool")
		doc := filepath.Join(dir, "README")
		os.WriteFile(bin, []byte("#!/bin/sh\necho 1.0.0\n"), 0o700)
		os.WriteFile(doc, []byte("hola\n"), 0o600)
		// mtimes de origen distintos en cada build: no deben importar.
		os.Chtimes(bin, time.Now(), time.Now())
		info := &nfpm.Info{
			Name: "tool", Arch: "amd64", Platform: "linux",
			Version: "1.0.0", VersionSchema: "none", Release: "0~tepache1",
			Section: "devel", Priority: "optional",
			Maintainer:      "Test <test@example.com>",
			Description:     "test tool\nLonger description.",
			MTime:           mtime,
			DisableGlobbing: true,
			Overridables: nfpm.Overridables{Contents: files.Contents{
				{Source: bin, Destination: "/usr/bin/tool", Type: files.TypeFile,
					FileInfo: &files.ContentFileInfo{Owner: "root", Group: "root", Mode: 0o755, MTime: mtime}},
				{Source: doc, Destination: "/usr/share/doc/tool/README", Type: files.TypeFile,
					FileInfo: &files.ContentFileInfo{Owner: "root", Group: "root", Mode: 0o644, MTime: mtime}},
			}},
		}
		var buf bytes.Buffer
		if err := deb.Default.Package(info, &buf); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	a, b := build(), build()
	if sum(a) != sum(b) {
		t.Fatalf("no reproducible: %s != %s", sum(a), sum(b))
	}
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("sin dpkg-deb")
	}
	path := filepath.Join(t.TempDir(), "tool.deb")
	os.WriteFile(path, a, 0o644)
	out, err := exec.Command("dpkg-deb", "-f", path, "Version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if v := strings.TrimSpace(string(out)); v != "1.0.0-0~tepache1" {
		t.Errorf("Version = %q", v)
	}
	cmd := exec.Command("dpkg-deb", "-c", path)
	cmd.Env = append(os.Environ(), "TZ=UTC")
	list, _ := cmd.Output()
	t.Logf("sha256 %s\n%s", sum(a), list)
	for _, want := range []string{"-rwxr-xr-x root/root", "-rw-r--r-- root/root", "2026-03-06 15:23 ./usr/bin/tool"} {
		if !strings.Contains(string(list), want) {
			t.Errorf("dpkg-deb -c no contiene %q", want)
		}
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
