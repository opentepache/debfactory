package repo

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"debfactory/internal/testutil"
)

var fprLine = regexp.MustCompile(`(?m)^Fingerprint: .*$`)

func guideRoot(t *testing.T, edit func(string) string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testutil.ProjectRoot(), GuideFile))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, GuideFile), []byte(edit(string(data))), 0o644)
	return root
}

func TestVerifyGuide(t *testing.T) {
	cfg := loadCfg(t)
	same := func(s string) string { return s }
	if err := VerifyGuide(guideRoot(t, same), cfg, Empty()); err != nil {
		t.Fatalf("guía real con estado vacío: %v", err)
	}

	fpr := strings.Repeat("ABCD", 10)
	cfg.Repo.Signing.Masters = []string{fpr}
	err := VerifyGuide(guideRoot(t, same), cfg, Empty())
	if err == nil || !strings.Contains(err.Error(), "falta el fingerprint") {
		t.Fatalf("sin fingerprint: %v", err)
	}
	withFpr := func(s string) string {
		return fprLine.ReplaceAllString(s, "Fingerprint: "+GroupFingerprint(fpr))
	}
	if err := VerifyGuide(guideRoot(t, withFpr), cfg, Empty()); err != nil {
		t.Fatalf("con fingerprint: %v", err)
	}

	url := cfg.Repo.URL
	cfg.Repo.URL = "https://apt.otro.example"
	if err := VerifyGuide(guideRoot(t, withFpr), cfg, Empty()); err == nil || !strings.Contains(err.Error(), "no contiene la URL") {
		t.Fatalf("URL: %v", err)
	}
	cfg.Repo.URL = url

	// Tabla de paquetes frente al estado.
	st := Empty()
	st.Serial = 1
	all := []string{"bookworm", "trixie", "sid", "noble", "resolute"}
	st.Packages["fnm"] = []Published{{Version: "1-0~tepache1", Suites: all}}
	st.Packages["bob"] = []Published{{Version: "1-0~tepache1", Suites: all[1:]}}
	if err := VerifyGuide(guideRoot(t, withFpr), cfg, st); err != nil {
		t.Fatalf("tabla coherente: %v", err)
	}
	st.Packages["bob"] = []Published{{Version: "1-0~tepache1", Suites: all}}
	err = VerifyGuide(guideRoot(t, withFpr), cfg, st)
	if err == nil || !strings.Contains(err.Error(), "la tabla dice que bob falta en [bookworm], pero el estado dice []") {
		t.Fatalf("tabla incoherente: %v", err)
	}
	st.Packages["zzz"] = []Published{{Version: "1-0~tepache1", Suites: all}}
	if err := VerifyGuide(guideRoot(t, withFpr), cfg, st); err == nil || !strings.Contains(err.Error(), "no incluye zzz") {
		t.Fatalf("paquete ausente: %v", err)
	}
}
