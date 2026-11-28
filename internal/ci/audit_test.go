package ci

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
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

// TestAuditExtend: la entrada extend lleva las suites después de version y
// solo vale sobre una versión publicada con los mismos .deb
// (specs/007-configurable-suites, data-model §4).
func TestAuditExtend(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	debs := map[string]string{"amd64": sha1, "arm64": sha2}
	if _, err := AppendAudit(root,
		Entry{Time: ts, Op: OpPublish, Package: "bob", Version: "4.3.0-0~tepache1", Commit: c40, Debs: debs, Serial: 1, Run: runURL("u")},
		Entry{Time: ts.Add(time.Hour), Op: OpExtend, Package: "bob", Version: "4.3.0-0~tepache1", Suites: []string{"forky"},
			Commit: c40, Debs: debs, Serial: 2, Run: runURL("u")},
	); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, AuditFile))
	line := strings.Split(strings.TrimSpace(string(data)), "\n")[1]
	if !strings.Contains(line, `"op":"extend","package":"bob","version":"4.3.0-0~tepache1","suites":["forky"],"commit":"`) {
		t.Errorf("extend = %s", line)
	}
	if err := CheckAudit(data, nil, nil); err != nil {
		t.Errorf("auditoría válida: %v", err)
	}
	bad := strings.Replace(string(data), `"suites":["forky"],`, "", 1)
	if _, err := ParseAudit([]byte(bad)); err == nil || !strings.Contains(err.Error(), "extend sin suites") {
		t.Errorf("sin suites: %v", err)
	}
	other := strings.Replace(string(data), `"version":"4.3.0-0~tepache1","suites"`, `"version":"9.9-0~tepache1","suites"`, 1)
	if err := CheckAudit([]byte(other), nil, nil); err == nil || !strings.Contains(err.Error(), "extend de una versión nunca publicada") {
		t.Errorf("versión no publicada: %v", err)
	}
}

// TestAuditLevel2 (specs/008-source-builds, data-model §3): publish y extend
// de nivel 2 llevan orig y builds; CheckAudit cubre cada suite del estado.
func TestAuditLevel2(t *testing.T) {
	h := func(c string) string { return strings.Repeat(c, 64) }
	build := func(v, c string) repo.Build {
		return repo.Build{Version: v, DSC: h(c), DebianTar: h(c), Debs: map[string]string{"amd64": h(c), "arm64": h(c)},
			Buildinfo: map[string]string{"amd64": h(c), "arm64": h(c)}}
	}
	orig := &repo.Orig{File: "tepache-hello_1.0.0.orig.tar.gz", SHA256: h("0")}
	ts := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	_, err := AppendAudit(root,
		Entry{Time: ts, Op: OpPublish, Package: "tepache-hello", Version: "1.0.0-0~tepache1", Commit: c40, Orig: orig,
			Builds: map[string]repo.Build{"bookworm": build("1.0.0-0~tepache1+deb12", "a")}, Serial: 1, Run: runURL("u")},
		Entry{Time: ts.Add(time.Hour), Op: OpExtend, Package: "tepache-hello", Version: "1.0.0-0~tepache1", Commit: c40,
			Suites: []string{"trixie"}, Orig: orig,
			Builds: map[string]repo.Build{"trixie": build("1.0.0-0~tepache1+deb13", "b")}, Serial: 2, Run: runURL("u")})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, AuditFile))
	if !strings.Contains(string(data), `"orig":{"file":"tepache-hello_1.0.0.orig.tar.gz"`) ||
		!strings.Contains(string(data), `"builds":{"bookworm":{"version":"1.0.0-0~tepache1+deb12"`) {
		t.Fatalf("formato:\n%s", data)
	}
	st := repo.Empty()
	st.Packages["tepache-hello"] = []repo.Published{{Version: "1.0.0-0~tepache1", Level: 2, Orig: orig,
		Suites: []string{"bookworm", "trixie"},
		Builds: map[string]repo.Build{"bookworm": build("1.0.0-0~tepache1+deb12", "a"), "trixie": build("1.0.0-0~tepache1+deb13", "b")}}}
	if err := CheckAudit(data, nil, st); err != nil {
		t.Fatal(err)
	}
	// Una suite del estado sin publicación en la auditoría.
	st.Packages["tepache-hello"][0].Suites = append(st.Packages["tepache-hello"][0].Suites, "noble")
	st.Packages["tepache-hello"][0].Builds["noble"] = build("1.0.0-0~tepache1+u2404", "c")
	if err := CheckAudit(data, nil, st); err == nil || !strings.Contains(err.Error(), "en noble") {
		t.Fatalf("error = %v", err)
	}
	// extend que repite una suite ya publicada.
	bad := append(slices.Clone(data), MarshalEntries([]Entry{{Seq: 3, Time: ts, Op: OpExtend, Package: "tepache-hello",
		Version: "1.0.0-0~tepache1", Commit: c40, Suites: []string{"bookworm"}, Orig: orig,
		Builds: map[string]repo.Build{"bookworm": build("1.0.0-0~tepache1+deb12", "d")}, Serial: 3, Run: runURL("u")}})...)
	if err := CheckAudit(bad, nil, nil); err == nil || !strings.Contains(err.Error(), "suites ya publicadas") {
		t.Fatalf("error = %v", err)
	}
	// builds sin orig.
	noOrig := MarshalEntries([]Entry{{Seq: 1, Time: ts, Op: OpPublish, Package: "x", Version: "1-0~tepache1", Commit: c40,
		Builds: map[string]repo.Build{"bookworm": build("1-0~tepache1+deb12", "a")}, Serial: 1, Run: runURL("u")}})
	if _, err := ParseAudit(noOrig); err == nil || !strings.Contains(err.Error(), "orig") {
		t.Fatalf("error = %v", err)
	}
}

// TestAuditArchesToolchain (specs/009, data-model §5): extend con arches y
// toolchain en publish.
func TestAuditArchesToolchain(t *testing.T) {
	ts := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	tc := &Toolchain{Name: "zig", Version: "0.17.0", SHA256: map[string]string{"amd64": sha1, "arm64": sha2}}
	_, err := AppendAudit(root,
		Entry{Time: ts, Op: OpPublish, Package: "fnm", Version: "1.0-0~tepache1", Commit: c40,
			Debs: map[string]string{"amd64": sha1}, Serial: 1, Run: runURL("u")},
		Entry{Time: ts, Op: OpExtend, Package: "fnm", Version: "1.0-0~tepache1", Commit: c40, Arches: []string{"arm64"},
			Debs: map[string]string{"arm64": sha2}, Serial: 2, Run: runURL("u")},
		Entry{Time: ts, Op: OpPublish, Package: "x", Version: "1-0~tepache1", Commit: c40, Debs: map[string]string{"amd64": strings.Repeat("3", 64)},
			Toolchain: tc, Serial: 3, Run: runURL("u")})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, AuditFile))
	if !strings.Contains(string(data), `"arches":["arm64"]`) || !strings.Contains(string(data), `"toolchain":{"name":"zig"`) {
		t.Fatalf("formato:\n%s", data)
	}
	st := repo.Empty()
	st.Packages["fnm"] = []repo.Published{{Version: "1.0-0~tepache1", Suites: []string{"trixie"}, Debs: map[string]string{"amd64": sha1, "arm64": sha2}}}
	st.Packages["x"] = []repo.Published{{Version: "1-0~tepache1", Suites: []string{"trixie"}, Arches: []string{"amd64"}, Debs: map[string]string{"amd64": strings.Repeat("3", 64)}}}
	if err := CheckAudit(data, nil, st); err != nil {
		t.Fatal(err)
	}
	// extend que cambia un .deb ya publicado.
	bad := append(slices.Clone(data), MarshalEntries([]Entry{{Seq: 4, Time: ts, Op: OpExtend, Package: "fnm", Version: "1.0-0~tepache1",
		Commit: c40, Arches: []string{"arm64"}, Debs: map[string]string{"amd64": sha2}, Serial: 4, Run: runURL("u")}})...)
	if err := CheckAudit(bad, nil, nil); err == nil || !strings.Contains(err.Error(), "cambia un .deb publicado") {
		t.Fatalf("error = %v", err)
	}
}
