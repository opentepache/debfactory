// Package sync sube el repositorio generado a un destino (directorio local,
// S3 como R2, o un VPS) con rclone, en un orden que nunca deja índices
// apuntando a archivos que faltan, y con borrado diferido una
// sincronización (specs/002-signed-apt-repo, research D6, FR-025–FR-029).
package sync

import (
	"sort"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/repo"
)

// Fases de subida, en orden.
const (
	PhasePool      = "pool"
	PhaseByHash    = "by-hash"
	PhaseIndices   = "indices"
	PhaseRelease   = "release"
	PhaseInRelease = "inrelease"
	PhaseDelete    = "delete"
)

// Order es el orden de las fases (research D6).
var Order = []string{PhasePool, PhaseByHash, PhaseIndices, PhaseRelease, PhaseInRelease, PhaseDelete}

// Record es lo último sincronizado a un destino (<data>/sync/<dest>.json).
type Record struct {
	Serial        int               `json:"serial"`
	Generation    string            `json:"generation"`
	Files         map[string]string `json:"files"`             // generación sincronizada (N-1)
	PreviousFiles map[string]string `json:"previous_files"`    // la anterior (N-2)
	Pending       []string          `json:"pending,omitempty"` // borrados calculados aún sin hacer
}

// Batch son archivos que se suben con el mismo Cache-Control.
type Batch struct {
	CacheControl string
	Files        []string
}

// Phase es una fase de la sincronización.
type Phase struct {
	Name    string
	Batches []Batch
	Changed int // archivos nuevos o cambiados respecto al registro
}

// Files devuelve todos los archivos de la fase.
func (p Phase) Files() []string {
	var out []string
	for _, b := range p.Batches {
		out = append(out, b.Files...)
	}
	return out
}

// Classify devuelve la fase y si el archivo es inmutable (pool/ y by-hash/).
func Classify(p string) (phase string, immutable bool) {
	parts := strings.Split(p, "/")
	switch {
	case parts[0] == "pool":
		return PhasePool, true
	case parts[0] != "dists":
		return PhasePool, false // debfactory.gpg y otros archivos de la raíz
	case strings.Contains(p, "/by-hash/"):
		return PhaseByHash, true
	case len(parts) == 3 && parts[2] == "InRelease":
		return PhaseInRelease, false
	case len(parts) == 3 && (parts[2] == "Release" || parts[2] == "Release.gpg"):
		return PhaseRelease, false
	}
	return PhaseIndices, false
}

// Plan calcula las fases para subir cur sabiendo lo último sincronizado
// (last, nil si nunca). Cada fase incluye todos sus archivos (rclone omite
// los que ya están iguales en el destino, así que repetir es seguro); delete
// borra previous_files − (actual ∪ files): lo de hace dos sincronizaciones
// que ya no está ni en la actual ni en la última (research D6).
func Plan(cfg *config.Config, dest config.Destination, cur *repo.GenInfo, last *Record) []Phase {
	byPhase := map[string]map[string][]string{} // fase -> cache-control -> archivos
	changed := map[string]int{}
	var paths []string
	for p := range cur.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		ph, imm := Classify(p)
		cc := ""
		if dest.CacheControl {
			cc = cfg.Repo.CacheControl.Mutable
			if imm {
				cc = cfg.Repo.CacheControl.Immutable
			}
		}
		if byPhase[ph] == nil {
			byPhase[ph] = map[string][]string{}
		}
		byPhase[ph][cc] = append(byPhase[ph][cc], p)
		if last == nil || last.Files[p] != cur.Files[p] {
			changed[ph]++
		}
	}
	var phases []Phase
	for _, name := range Order[:len(Order)-1] {
		ph := Phase{Name: name, Changed: changed[name]}
		var ccs []string
		for cc := range byPhase[name] {
			ccs = append(ccs, cc)
		}
		sort.Strings(ccs)
		for _, cc := range ccs {
			ph.Batches = append(ph.Batches, Batch{CacheControl: cc, Files: byPhase[name][cc]})
		}
		phases = append(phases, ph)
	}
	del := Phase{Name: PhaseDelete}
	if gone := Deletions(cur, last); len(gone) > 0 {
		del.Batches = []Batch{{Files: gone}}
		del.Changed = len(gone)
	}
	return append(phases, del)
}

// Deletions calcula qué borrar. Si la generación avanzó respecto a last:
// (previous_files ∪ pending) − (actual ∪ files). Si es la misma generación
// (repetir sync), solo lo que quedó pendiente.
func Deletions(cur *repo.GenInfo, last *Record) []string {
	if last == nil {
		return nil
	}
	cand := map[string]bool{}
	for _, p := range last.Pending {
		cand[p] = true
	}
	if last.Generation != cur.Name {
		for p := range last.PreviousFiles {
			cand[p] = true
		}
	}
	var gone []string
	for p := range cand {
		if _, ok := cur.Files[p]; ok {
			continue
		}
		if _, ok := last.Files[p]; ok && last.Generation != cur.Name {
			continue
		}
		gone = append(gone, p)
	}
	sort.Strings(gone)
	return gone
}

// Unchanged indica si el plan no tiene nada que subir ni borrar.
func Unchanged(phases []Phase) bool {
	for _, p := range phases {
		if p.Changed > 0 {
			return false
		}
	}
	return true
}
