package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"debfactory/internal/fetch"
	"debfactory/internal/report"
)

// GenInfo describe una generación (<data>/public/gen-<N>.json).
type GenInfo struct {
	Name          string            `json:"name"`
	Serial        int               `json:"serial"`
	StateSHA256   string            `json:"state_sha256"`
	Files         map[string]string `json:"files"` // ruta relativa -> sha256 (sin enlaces)
	SigningSubkey string            `json:"signing_subkey"`
	Created       time.Time         `json:"created"`
}

// Dir es el directorio de la generación.
func (g *GenInfo) Dir(dataDir string) string { return filepath.Join(PublicDir(dataDir), g.Name) }

// TreeFiles lista los archivos regulares de dir (sin enlaces simbólicos)
// con su sha256.
func TreeFiles(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		sha, err := fetch.SHA256File(p)
		files[filepath.ToSlash(rel)] = sha
		return err
	})
	return files, err
}

// Commit expone genDir como la generación vigente y escribe el estado, en
// este orden (research D3): gen-<N>.json, state.yaml temporal, enlace
// current (rename atómico), state.yaml definitivo y limpieza de
// generaciones anteriores a la previa.
func Commit(root, dataDir string, st *State, genDir, subkey string) (*GenInfo, error) {
	fail := func(err error) error { return report.Newf(report.Swap, "%v", err) }
	pub := PublicDir(dataDir)
	data, err := st.Marshal()
	if err != nil {
		return nil, fail(err)
	}
	files, err := TreeFiles(genDir)
	if err != nil {
		return nil, fail(err)
	}
	info := &GenInfo{Name: filepath.Base(genDir), Serial: st.Serial, StateSHA256: fetch.SHA256(data),
		Files: files, SigningSubkey: subkey, Created: time.Now().UTC().Truncate(time.Second)}
	ij, _ := json.MarshalIndent(info, "", "  ")
	if err := writeAtomic(genDir+".json", append(ij, '\n')); err != nil {
		return nil, fail(err)
	}
	statePath := filepath.Join(root, StateFile)
	tmpState := filepath.Join(filepath.Dir(statePath), ".state.yaml.commit")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return nil, fail(err)
	}
	if err := os.WriteFile(tmpState, data, 0o644); err != nil {
		return nil, fail(err)
	}
	previous, _ := os.Readlink(filepath.Join(pub, "current"))
	tmpLink := filepath.Join(pub, "current.tmp")
	os.Remove(tmpLink)
	if err := os.Symlink(info.Name, tmpLink); err != nil {
		return nil, fail(err)
	}
	if err := os.Rename(tmpLink, filepath.Join(pub, "current")); err != nil {
		return nil, fail(err)
	}
	if err := os.Rename(tmpState, statePath); err != nil {
		return nil, fail(err)
	}
	cleanup(pub, info.Name, previous)
	return info, nil
}

// cleanup borra las generaciones que no son la actual ni la previa.
func cleanup(pub, current, previous string) {
	entries, _ := os.ReadDir(pub)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if !strings.HasPrefix(name, "gen-") || name == current || name == previous {
			continue
		}
		os.RemoveAll(filepath.Join(pub, e.Name()))
	}
}

// Current devuelve la generación vigente, o nil si no hay ninguna.
func Current(dataDir string) (*GenInfo, error) {
	pub := PublicDir(dataDir)
	name, err := os.Readlink(filepath.Join(pub, "current"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readGen(pub, name)
}

// Generations devuelve las generaciones registradas, de la más nueva a la
// más vieja.
func Generations(dataDir string) ([]*GenInfo, error) {
	pub := PublicDir(dataDir)
	matches, _ := filepath.Glob(filepath.Join(pub, "gen-*.json"))
	var out []*GenInfo
	for _, m := range matches {
		g, err := readGen(pub, strings.TrimSuffix(filepath.Base(m), ".json"))
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

func readGen(pub, name string) (*GenInfo, error) {
	data, err := os.ReadFile(filepath.Join(pub, name+".json"))
	if err != nil {
		return nil, err
	}
	var g GenInfo
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("%s.json: %w", name, err)
	}
	return &g, nil
}

// CheckConsistent comprueba que la generación vigente corresponde a
// repo/state.yaml (FR-037).
func CheckConsistent(root, dataDir string, st *State) error {
	cur, err := Current(dataDir)
	if err != nil {
		return report.Newf(report.VerifyRepo, "%v", err)
	}
	if cur == nil {
		if st.Serial == 0 {
			return nil
		}
		return report.Newf(report.VerifyRepo,
			"no hay generación publicada pero repo/state.yaml tiene serial %d; ejecuta `debfactory repo rebuild`", st.Serial)
	}
	sha, err := st.SHA256()
	if err != nil {
		return report.Newf(report.VerifyRepo, "%v", err)
	}
	if cur.StateSHA256 != sha {
		return report.Newf(report.VerifyRepo,
			"current (serial %d) no corresponde a repo/state.yaml (serial %d); ejecuta `debfactory repo rebuild`",
			cur.Serial, st.Serial)
	}
	return nil
}

// removeGen borra una generación no expuesta.
func removeGen(genDir string) {
	os.RemoveAll(genDir)
	os.Remove(genDir + ".json")
}

// Verify comprueba el árbol local (repo verify sin --dest).
func (e *Env) Verify(ctx context.Context) error {
	st, err := ReadState(e.Root)
	if err != nil {
		return err
	}
	if err := st.Validate(e.Cfg); err != nil {
		return err
	}
	if err := CheckConsistent(e.Root, e.DataDir, st); err != nil {
		return err
	}
	if err := VerifyGuide(e.Root, e.Cfg, st); err != nil {
		return err
	}
	if e.Cfg.Repo.Signing.Subkey != "" {
		if err := e.checkExpiry(ctx); err != nil {
			return err
		}
	}
	cur, err := Current(e.DataDir)
	if err != nil {
		return report.Newf(report.VerifyRepo, "%v", err)
	}
	if cur == nil {
		e.Out.Event("verify", map[string]any{"serial": 0}, "repo: vacío (serial 0); nada que verificar")
		return nil
	}
	if err := VerifyTree(ctx, e.Cfg, st, cur.Dir(e.DataDir)); err != nil {
		return err
	}
	e.Out.Event("verify", map[string]any{"serial": st.Serial, "generation": cur.Name},
		fmt.Sprintf("repo: serial %d (%s) verificado", st.Serial, cur.Name))
	return nil
}
