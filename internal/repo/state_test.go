package repo

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"debfactory/internal/config"
	"debfactory/internal/testutil"
)

func loadCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// stateRoot copia un fixture a <tmp>/repo/state.yaml.
func stateRoot(t *testing.T, fixture string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../testdata/repo/state", fixture))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, StateFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func want(t *testing.T, fixture string) string {
	t.Helper()
	f, err := os.Open(filepath.Join("../../testdata/repo/state", fixture))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan()
	w, ok := strings.CutPrefix(sc.Text(), "# want: ")
	if !ok {
		t.Fatalf("%s: falta la cabecera # want:", fixture)
	}
	return w
}

func TestStateInvalid(t *testing.T) {
	cfg := loadCfg(t)
	files, _ := filepath.Glob("../../testdata/repo/state/*.yaml")
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasPrefix(name, "ok") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			st, err := ReadState(stateRoot(t, name))
			if err == nil {
				err = st.Validate(cfg)
			}
			if err == nil {
				t.Fatal("se esperaba un error")
			}
			if w := want(t, name); !strings.Contains(err.Error(), w) {
				t.Errorf("error = %q\nwant  %q", err, w)
			}
		})
	}
}

func TestStateRoundTrip(t *testing.T) {
	for _, f := range []string{"ok.yaml", "ok-level2.yaml", "ok-arches.yaml", "ok-components.yaml"} {
		t.Run(f, func(t *testing.T) { roundTrip(t, f) })
	}
}

// TestStateSchemaFollowsLevel: un estado sin nivel 2 se escribe con schema 1
// (FR-014) y uno con nivel 2 con schema 2.
func TestStateSchemaFollowsLevel(t *testing.T) {
	st, err := ReadState(stateRoot(t, "ok-level2.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := st.Marshal()
	if !strings.Contains(string(data), "\nschema: 2\n") {
		t.Fatalf("con nivel 2 se esperaba schema 2:\n%s", data)
	}
	delete(st.Packages, "tepache-hello")
	data, _ = st.Marshal()
	if !strings.Contains(string(data), "\nschema: 1\n") {
		t.Fatalf("sin nivel 2 se esperaba schema 1:\n%s", data)
	}
	c := st.Clone()
	if c.schemaFor() != 1 {
		t.Fatal("Clone cambió el schema")
	}
}

func roundTrip(t *testing.T, fixture string) {
	cfg := loadCfg(t)
	root := stateRoot(t, fixture)
	st, err := ReadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Validate(cfg); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(filepath.Join(root, StateFile))
	if err := st.Write(root); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, StateFile))
	if string(got) != string(orig) {
		t.Errorf("la escritura no es idéntica:\n%s\n--- original ---\n%s", got, orig)
	}
}

func TestEmptyStateMatchesRepo(t *testing.T) {
	data, err := Empty().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	st, err := ReadState(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	if st.Serial == 0 {
		orig, _ := os.ReadFile(filepath.Join(testutil.ProjectRoot(), StateFile))
		if string(orig) != string(data) {
			t.Errorf("repo/state.yaml vacío no coincide con Empty():\n%s", data)
		}
	}
}
