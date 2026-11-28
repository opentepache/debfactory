package ci

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"debfactory/internal/repo"
	"debfactory/internal/report"
	"debfactory/internal/version"
)

// AuditFile es el registro de auditoría en el repositorio (contracts/audit.md).
const AuditFile = "repo/audit.jsonl"

// Operaciones de la auditoría.
const (
	OpPublish = "publish"
	OpRetire  = "retire"
	OpImport  = "import"
	// OpExtend: una versión ya publicada llega a suites nuevas con los mismos
	// .deb (specs/007-configurable-suites, research D6).
	OpExtend = "extend"
)

// Entry es una línea de repo/audit.jsonl. El orden de los campos es el del
// contrato; Run es un puntero para emitir null en import.
type Entry struct {
	Seq     int       `json:"seq"`
	Time    time.Time `json:"time"`
	Op      string    `json:"op"`
	Package string    `json:"package"`
	Version string    `json:"version"`
	// Suites son las suites agregadas; solo en extend.
	Suites []string `json:"suites,omitempty"`
	// Arches son las arquitecturas agregadas al quitar una exclusión; solo en
	// extend (specs/009, data-model §5).
	Arches []string `json:"arches,omitempty"`
	Commit string   `json:"commit"`
	// Tool es el commit de la herramienta que produjo la entrada
	// (specs/004-split-repos, data-model.md §3). Las anteriores no lo tienen.
	Tool string            `json:"tool,omitempty"`
	Debs map[string]string `json:"debs,omitempty"`
	// Nivel 2 (specs/008-source-builds, data-model §3): el .orig común y
	// las compilaciones de las suites publicadas en esta operación (en
	// extend, solo las nuevas), en lugar de debs.
	Orig   *repo.Orig            `json:"orig,omitempty"`
	Builds map[string]repo.Build `json:"builds,omitempty"`
	// Toolchain de upstream con el que se compiló (nivel 2, specs/009).
	Toolchain *Toolchain `json:"toolchain,omitempty"`
	// Provenance es el sha256 de provenance.json de la versión (specs/009,
	// data-model §8): el archivo vive en <data> y en el sitio; su huella,
	// aquí, en la rama de estado.
	Provenance string  `json:"provenance_sha256,omitempty"`
	Serial     int     `json:"serial"`
	Run        *string `json:"run"`
	Reason     string  `json:"reason,omitempty"`
}

// Toolchain es el toolchain de upstream registrado en la auditoría.
type Toolchain struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	SHA256  map[string]string `json:"sha256"` // arch -> sha256
}

// MarshalJSON escribe la entrada con las claves en orden fijo, la fecha sin
// fracciones y debs con amd64 antes que arm64.
func (e Entry) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	field := func(k string, v any) {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(v)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	field("seq", e.Seq)
	field("time", e.Time.UTC().Truncate(time.Second).Format(time.RFC3339))
	field("op", e.Op)
	field("package", e.Package)
	field("version", e.Version)
	if len(e.Suites) > 0 {
		field("suites", e.Suites)
	}
	if len(e.Arches) > 0 {
		field("arches", e.Arches)
	}
	field("commit", e.Commit)
	if e.Tool != "" {
		field("tool", e.Tool)
	}
	if len(e.Debs) > 0 {
		var d bytes.Buffer
		keys := make([]string, 0, len(e.Debs))
		for k := range e.Debs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		d.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				d.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			vb, _ := json.Marshal(e.Debs[k])
			d.Write(kb)
			d.WriteByte(':')
			d.Write(vb)
		}
		d.WriteByte('}')
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"debs":`)
		b.Write(d.Bytes())
	}
	if e.Orig != nil {
		field("orig", e.Orig)
	}
	if len(e.Builds) > 0 {
		field("builds", e.Builds) // encoding/json ordena las claves de los mapas
	}
	if e.Provenance != "" {
		field("provenance_sha256", e.Provenance)
	}
	if e.Toolchain != nil {
		field("toolchain", e.Toolchain)
	}
	field("serial", e.Serial)
	field("run", e.Run)
	if e.Reason != "" {
		field("reason", e.Reason)
	}
	return []byte("{" + b.String() + "}"), nil
}

var (
	hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ParseAudit lee un registro completo, estricto: JSON válido, sin claves
// desconocidas, campos del contrato y seq consecutivo desde 1.
func ParseAudit(data []byte) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Bytes()
		fail := func(format string, args ...any) error {
			return report.Newf(report.Audit, "%s:%d: %s", AuditFile, n, fmt.Sprintf(format, args...))
		}
		if len(line) == 0 {
			return nil, fail("línea vacía")
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		var e Entry
		if err := dec.Decode(&e); err != nil {
			var msg = err.Error()
			if m := regexp.MustCompile(`unknown field "([^"]+)"`).FindStringSubmatch(msg); m != nil {
				return nil, fail("clave desconocida %q", m[1])
			}
			return nil, fail("JSON inválido: %v", err)
		}
		if want := len(out) + 1; e.Seq != want {
			prev := 0
			if len(out) > 0 {
				prev = out[len(out)-1].Seq
			}
			return nil, fail("seq %d tras %d", e.Seq, prev)
		}
		switch e.Op {
		case OpPublish, OpImport, OpExtend:
			switch {
			case len(e.Builds) > 0:
				if err := level2Entry(e); err != "" {
					return nil, fail("%s", err)
				}
			case len(e.Debs) == 0:
				return nil, fail("%s sin debs", e.Op)
			case e.Orig != nil:
				return nil, fail("orig sin builds")
			}
			for arch, sha := range e.Debs {
				if !hex64.MatchString(sha) {
					return nil, fail("debs.%s: sha256 inválido", arch)
				}
			}
			if (e.Op == OpPublish || e.Op == OpExtend) && e.Run == nil {
				return nil, fail("%s sin run", e.Op)
			}
			if e.Op == OpExtend && len(e.Suites) == 0 && len(e.Arches) == 0 {
				return nil, fail("extend sin suites ni arches")
			}
			if t := e.Toolchain; t != nil {
				if t.Name == "" || t.Version == "" || len(t.SHA256) == 0 {
					return nil, fail("toolchain incompleto")
				}
				for a, h := range t.SHA256 {
					if !hex64.MatchString(h) {
						return nil, fail("toolchain.sha256.%s: sha256 inválido", a)
					}
				}
			}
		case OpRetire:
			if e.Reason == "" {
				return nil, fail("retire sin reason")
			}
		default:
			return nil, fail("op %q desconocida", e.Op)
		}
		if e.Package == "" || e.Version == "" || !hex40.MatchString(e.Commit) || e.Serial < 1 {
			return nil, fail("faltan package, version, commit (40 hex) o serial")
		}
		if e.Tool != "" && !hex40.MatchString(e.Tool) {
			return nil, fail("tool: no es un commit (40 hex)")
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// level2Entry valida los campos de nivel 2 de una entrada.
func level2Entry(e Entry) string {
	if len(e.Debs) > 0 {
		return "debs y builds a la vez"
	}
	if e.Orig == nil || e.Orig.File == "" || !hex64.MatchString(e.Orig.SHA256) {
		return "builds sin orig válido"
	}
	for su, b := range e.Builds {
		if b.Version == "" || !hex64.MatchString(b.DSC) || !hex64.MatchString(b.DebianTar) || len(b.Debs) == 0 ||
			len(b.Debs) != len(b.Buildinfo) {
			return "builds." + su + ": incompleto"
		}
		for arch, sha := range b.Debs {
			if !hex64.MatchString(sha) || !hex64.MatchString(b.Buildinfo[arch]) {
				return "builds." + su + "." + arch + ": sha256 inválido"
			}
		}
	}
	return ""
}

// ReadAudit lee root/repo/audit.jsonl; si no existe devuelve nil sin error.
func ReadAudit(root string) ([]byte, []Entry, error) {
	data, err := os.ReadFile(filepath.Join(root, AuditFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	entries, err := ParseAudit(data)
	return data, entries, err
}

// MarshalEntries serializa entradas, una por línea.
func MarshalEntries(entries []Entry) []byte {
	var b bytes.Buffer
	for _, e := range entries {
		line, _ := e.MarshalJSON()
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// AppendAudit añade entradas a root/repo/audit.jsonl con seq consecutivo y
// escritura atómica. Devuelve las entradas tal como quedaron escritas.
func AppendAudit(root string, entries ...Entry) ([]Entry, error) {
	data, old, err := ReadAudit(root)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Seq = len(old) + i + 1
		// Toda entrada nueva lleva la herramienta que la produjo (FR-014 de
		// 004). Una compilación "dev" (fuera de tool-install) no la registra.
		if entries[i].Tool == "" && hex40.MatchString(version.Commit) {
			entries[i].Tool = version.Commit
		}
	}
	out := append(slices.Clone(data), MarshalEntries(entries)...)
	if _, err := ParseAudit(out); err != nil {
		return nil, err
	}
	path := filepath.Join(root, AuditFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return nil, err
	}
	return entries, os.Rename(tmp, path)
}

// IsPrefix indica si el registro a es prefijo exacto, byte a byte, de b.
func IsPrefix(a, b []byte) bool { return bytes.HasPrefix(b, a) }

// CheckAudit aplica las reglas de contracts/audit.md § Reglas:
//   - el registro de referencia (against) es prefijo exacto de data;
//   - todo .deb de st tiene una entrada publish o import con su sha256;
//   - todo retire se refiere a una versión publicada o importada antes.
//
// Los errores de formato y de seq los da ParseAudit.
func CheckAudit(data, against []byte, st *repo.State) error {
	entries, err := ParseAudit(data)
	if err != nil {
		return err
	}
	if against != nil && !IsPrefix(against, data) {
		line := 1 + bytes.Count(commonPrefix(against, data), []byte("\n"))
		return report.Newf(report.Audit, "%s: la línea %d difiere de la referencia (solo se permite añadir)", AuditFile, line)
	}
	published := map[string]map[string]string{} // "pkg version" -> arch -> sha
	level2 := map[string]*Entry{}               // "pkg version" -> orig y builds acumulados
	var errs report.Errors
	for _, e := range entries {
		key := e.Package + " " + e.Version
		switch e.Op {
		case OpPublish, OpImport:
			published[key] = e.Debs
			if len(e.Builds) > 0 {
				acc := e
				acc.Builds = maps.Clone(e.Builds)
				level2[key] = &acc
			}
		case OpExtend:
			// Amplía una versión ya publicada: no cambia qué versiones existen.
			if acc, ok := level2[key]; ok && len(e.Builds) > 0 {
				// Nivel 2: el mismo .orig; suites que no tenía (FR-015) o, al
				// agregar arquitecturas, solo los archivos de esas
				// arquitecturas en las suites ya publicadas (specs/009).
				bad := !repo.EqualOrig(acc.Orig, e.Orig)
				for su, b := range e.Builds {
					old, dup := acc.Builds[su]
					switch {
					case !dup:
						acc.Builds[su] = b
					case len(e.Arches) == 0 || old.Version != b.Version || old.DSC != b.DSC || old.DebianTar != b.DebianTar:
						bad = true
					default:
						merged := old
						merged.Debs, merged.Buildinfo = maps.Clone(old.Debs), maps.Clone(old.Buildinfo)
						for a, h := range b.Debs {
							if _, had := old.Debs[a]; had && old.Debs[a] != h || !slices.Contains(e.Arches, a) && !had {
								bad = true
							}
							merged.Debs[a], merged.Buildinfo[a] = h, b.Buildinfo[a]
						}
						acc.Builds[su] = merged
					}
				}
				if bad {
					errs = append(errs, report.Newf(report.Audit, "%s:%d: extend de nivel 2 con otro .orig, suites ya publicadas u otros archivos", AuditFile, e.Seq))
				}
				continue
			}
			pub, ok := published[key]
			switch {
			case !ok || len(e.Builds) > 0:
				errs = append(errs, report.Newf(report.Audit, "%s:%d: extend de una versión nunca publicada o con otros .deb", AuditFile, e.Seq))
			case len(e.Arches) > 0:
				// Nivel 1: agrega los .deb de las arquitecturas nuevas.
				merged := maps.Clone(pub)
				for a, h := range e.Debs {
					if old, had := pub[a]; had && old != h || !slices.Contains(e.Arches, a) && !had {
						errs = append(errs, report.Newf(report.Audit, "%s:%d: extend con arquitecturas que cambia un .deb publicado", AuditFile, e.Seq))
					}
					merged[a] = h
				}
				published[key] = merged
			case !maps.Equal(pub, e.Debs):
				errs = append(errs, report.Newf(report.Audit, "%s:%d: extend de una versión nunca publicada o con otros .deb", AuditFile, e.Seq))
			}
		case OpRetire:
			if _, ok := published[key]; !ok {
				errs = append(errs, report.Newf(report.Audit, "%s:%d: retiro de una versión nunca publicada", AuditFile, e.Seq))
			}
		}
	}
	if st != nil {
		for _, name := range st.PackageNames() {
			for _, p := range st.Packages[name] {
				if p.Level2() {
					acc, ok := level2[name+" "+p.Version]
					for _, su := range p.Suites {
						if !ok || !repo.EqualOrig(acc.Orig, p.Orig) || !acc.Builds[su].Equal(p.Builds[su]) {
							errs = append(errs, report.Newf(report.Audit, "%s: falta la publicación de %s %s en %s", AuditFile, name, p.Version, su))
						}
					}
					continue
				}
				for arch, sha := range p.Debs {
					if published[name+" "+p.Version][arch] != sha {
						errs = append(errs, report.Newf(report.Audit, "%s: falta la publicación de %s %s %s", AuditFile, name, p.Version, arch))
					}
				}
			}
		}
	}
	return errs.Err()
}

func commonPrefix(a, b []byte) []byte {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			// hasta el inicio de la línea que difiere
			return a[:bytes.LastIndexByte(a[:i], '\n')+1]
		}
	}
	return a[:n]
}

// ImportEntries crea una entrada import por cada versión ya publicada en st
// (contracts/audit.md § Primera ejecución), en orden de paquete y de versión
// ascendente, con el commit que dejó ese estado.
func ImportEntries(st *repo.State, commit string) []Entry {
	var out []Entry
	for _, name := range st.PackageNames() {
		pubs := st.Packages[name]
		for i := len(pubs) - 1; i >= 0; i-- { // state.yaml va de mayor a menor
			p := pubs[i]
			e := Entry{Time: p.Published, Op: OpImport, Package: name, Version: p.Version,
				Commit: commit, Debs: p.Debs, Serial: st.Serial}
			if p.Level2() {
				e.Debs, e.Orig, e.Builds = nil, p.Orig, p.Builds
			}
			out = append(out, e)
		}
	}
	return out
}
