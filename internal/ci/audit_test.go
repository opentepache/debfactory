package ci

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/repo"
	"debfactory/internal/testutil"
	"debfactory/internal/version"
)

var (
	sha1 = strings.Repeat("1", 64)
	sha2 = strings.Repeat("2", 64)
	c40  = strings.Repeat("c", 40)
)

func runURL(s string) *string { return &s }

// TestAuditRoundTrip: orden de claves del contrato y bytes idénticos al releer.
func TestAuditRoundTrip(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 11, 13, 15, 47, 39, 0, time.UTC)
	got, err := AppendAudit(root,
		Entry{Time: ts, Op: OpImport, Package: "aqua", Version: "2.66.1-0~tepache1", Commit: c40,
			Debs: map[string]string{"arm64": sha2, "amd64": sha1}, Serial: 1},
		Entry{Time: ts.Add(time.Hour), Op: OpPublish, Package: "fnm", Version: "1.40.0-0~tepache1", Commit: c40,
			Debs: map[string]string{"amd64": sha1, "arm64": sha2}, Serial: 2, Run: runURL("https://github.com/o/r/actions/runs/1")},
	)
	if err != nil || got[1].Seq != 2 {
		t.Fatalf("append: %v %+v", err, got)
	}
	if _, err := AppendAudit(root, Entry{Time: ts.Add(2 * time.Hour), Op: OpRetire, Package: "fnm",
		Version: "1.40.0-0~tepache1", Commit: c40, Serial: 3, Run: runURL("u"), Reason: "segfault"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, AuditFile))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want0 := `{"seq":1,"time":"2026-11-13T15:47:39Z","op":"import","package":"aqua","version":"2.66.1-0~tepache1","commit":"` + c40 +
		`","debs":{"amd64":"` + sha1 + `","arm64":"` + sha2 + `"},"serial":1,"run":null}`
	if lines[0] != want0 {
		t.Errorf("línea 1 =\n%s\nse esperaba\n%s", lines[0], want0)
	}
	if !strings.HasSuffix(lines[2], `"serial":3,"run":"u","reason":"segfault"}`) {
		t.Errorf("línea 3 = %s", lines[2])
	}
	entries, err := ParseAudit(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(MarshalEntries(entries), data) {
		t.Error("ida y vuelta no es idéntica byte a byte")
	}
}

// TestCheckAudit: las 5 reglas de contracts/audit.md § Reglas.
func TestCheckAudit(t *testing.T) {
	ts := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	base := MarshalEntries([]Entry{
		{Seq: 1, Time: ts, Op: OpImport, Package: "fnm", Version: "1.0-0~tepache1", Commit: c40,
			Debs: map[string]string{"amd64": sha1, "arm64": sha2}, Serial: 1},
	})
	st := repo.Empty()
	st.Serial = 1
	st.Packages["fnm"] = []repo.Published{{Version: "1.0-0~tepache1", Debs: map[string]string{"amd64": sha1, "arm64": sha2}}}
	if err := CheckAudit(base, nil, st); err != nil {
		t.Fatalf("registro válido: %v", err)
	}
	line := func(s string) []byte { return append(append([]byte{}, base...), s+"\n"...) }
	cases := map[string]struct {
		data, against []byte
		st            *repo.State
		want          string
	}{
		"clave desconocida": {line(`{"seq":2,"time":"2026-11-13T00:00:00Z","op":"retire","package":"fnm","version":"1.0-0~tepache1","commit":"` + c40 + `","serial":2,"run":"u","reason":"x","note":"y"}`), nil, nil, `repo/audit.jsonl:2: clave desconocida "note"`},
		"seq":               {line(`{"seq":9,"time":"2026-11-13T00:00:00Z","op":"retire","package":"fnm","version":"1.0-0~tepache1","commit":"` + c40 + `","serial":2,"run":"u","reason":"x"}`), nil, nil, "repo/audit.jsonl:2: seq 9 tras 1"},
		"no es prefijo":     {base, bytes.Replace(base, []byte(`"serial":1`), []byte(`"serial":7`), 1), nil, "la línea 1 difiere de la referencia (solo se permite añadir)"},
		"falta publicación": {base, nil, func() *repo.State {
			s := st.Clone()
			s.Packages["bob"] = []repo.Published{{Version: "2.0-0~tepache1", Debs: map[string]string{"arm64": sha1}}}
			return s
		}(), "falta la publicación de bob 2.0-0~tepache1 arm64"},
		"retiro sin publicación": {line(`{"seq":2,"time":"2026-11-13T00:00:00Z","op":"retire","package":"bob","version":"9-0~tepache1","commit":"` + c40 + `","serial":2,"run":"u","reason":"x"}`), nil, nil, "repo/audit.jsonl:2: retiro de una versión nunca publicada"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := CheckAudit(c.data, c.against, c.st)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v; se esperaba %q", err, c.want)
			}
			if err != nil && !strings.Contains(err.Error(), ": audit: ") {
				t.Errorf("etapa: %v", err)
			}
		})
	}
}

// TestImportEntries: con el estado real (4 paquetes, serial 1) salen 4
// entradas import con el commit dado y que cubren state.yaml.
func TestImportEntries(t *testing.T) {
	st, err := repo.ReadState(testutil.ProjectRoot())
	if err != nil {
		t.Fatal(err)
	}
	entries := ImportEntries(st, c40)
	if len(entries) != 4 || entries[0].Package != "aqua" || entries[3].Package != "fnm" || entries[0].Run != nil {
		t.Fatalf("entries = %+v", entries)
	}
	for i := range entries {
		entries[i].Seq = i + 1
	}
	if err := CheckAudit(MarshalEntries(entries), nil, st); err != nil {
		t.Errorf("las entradas import no cubren el estado: %v", err)
	}
}

// TestAuditTool: campo tool de la fase 004 (data-model.md §3): va justo
// después de commit, AppendAudit lo rellena con la versión instalada, las
// entradas antiguas sin él siguen siendo válidas y uno inválido se rechaza.
func TestAuditTool(t *testing.T) {
	old := version.Commit
	t.Cleanup(func() { version.Commit = old })
	ts := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	tool := strings.Repeat("d", 40)

	root := t.TempDir()
	version.Commit = "dev"
	if _, err := AppendAudit(root, Entry{Time: ts, Op: OpImport, Package: "aqua", Version: "2.66.1-0~tepache1",
		Commit: c40, Debs: map[string]string{"amd64": sha1}, Serial: 1}); err != nil {
		t.Fatal(err)
	}
	version.Commit = tool
	if _, err := AppendAudit(root, Entry{Time: ts, Op: OpRetire, Package: "aqua", Version: "2.66.1-0~tepache1",
		Commit: c40, Serial: 2, Run: runURL("u"), Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, AuditFile))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if strings.Contains(lines[0], `"tool"`) {
		t.Errorf("una compilación dev no registra tool: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"commit":"`+c40+`","tool":"`+tool+`","serial"`) {
		t.Errorf("tool no va justo después de commit: %s", lines[1])
	}
	entries, err := ParseAudit(data)
	if err != nil || entries[1].Tool != tool {
		t.Fatalf("parse: %v %+v", err, entries)
	}
	bad := strings.Replace(lines[1], tool, "v1.2.3", 1)
	if _, err := ParseAudit([]byte(lines[0] + "\n" + bad + "\n")); err == nil || !strings.Contains(err.Error(), "tool") {
		t.Errorf("tool inválido aceptado: %v", err)
	}
}
