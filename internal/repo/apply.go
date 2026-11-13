package repo

import (
	"fmt"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/report"
)

// Kind es el tipo de cambio que produce una operación.
type Kind string

// Tipos de cambio (FR-010).
const (
	KindAdd    Kind = "add"
	KindRemove Kind = "remove"
	KindKeep   Kind = "keep"
)

// Change es un cambio del estado, para el informe de publish y retire.
type Change struct {
	Kind    Kind
	Package string
	Version string
	Suites  []string
	Reason  string
}

// Op es una operación sobre el estado: Add o Retire.
type Op interface{ op() }

// Add publica una versión.
type Add struct {
	Package   string
	Published Published
}

// Retire retira una versión de todas las suites.
type Retire struct {
	Package, Version, Reason string
	Last                     bool // confirma retirar la única versión publicada
}

func (Add) op()    {}
func (Retire) op() {}

// Apply devuelve el estado resultante de aplicar ops y los cambios. Si
// ninguna operación cambia nada, el estado devuelto es igual al original y
// la lista de cambios está vacía; si alguna falla, no se aplica ninguna
// (US3-4) y se devuelven todos los errores.
func (s *State) Apply(cfg *config.Config, now time.Time, ops ...Op) (*State, []Change, error) {
	next := s.Clone()
	var changes []Change
	var errs report.Errors
	fail := func(pkg, version, format string, args ...any) {
		errs = append(errs, &report.Error{Package: pkg, Version: version, Stage: report.State,
			Cause: fmt.Sprintf(format, args...)})
	}
	for _, o := range ops {
		switch o := o.(type) {
		case Add:
			p := o.Published
			if r, ok := next.FindRetired(o.Package, p.Version); ok {
				fail(o.Package, p.Version, "versión retirada el %s (las versiones no se reutilizan; sube revision)",
					r.Date.UTC().Format(time.DateOnly))
				continue
			}
			if old, ok := next.Find(o.Package, p.Version); ok {
				same := true
				for arch, sha := range p.Debs {
					if old.Debs[arch] != sha {
						same = false
						errs = append(errs, &report.Error{Package: o.Package, Version: p.Version, Arch: arch,
							Stage: report.State, Cause: "ya publicada con otro sha256 (las versiones no se reescriben; sube revision)"})
					}
				}
				if same {
					continue // sin cambios
				}
				continue
			}
			p.Published = now.UTC().Truncate(time.Second)
			next.Packages[o.Package] = append(next.Packages[o.Package], p)
			changes = append(changes, Change{Kind: KindAdd, Package: o.Package, Version: p.Version, Suites: p.Suites})
			rc, err := next.retain(cfg, o.Package, p)
			if err != nil {
				fail(o.Package, p.Version, "%v", err)
				continue
			}
			changes = append(changes, rc...)
		case Retire:
			changes = append(changes, next.retire(o, now, fail)...)
		}
	}
	if len(errs) > 0 {
		return s, nil, errs
	}
	if len(changes) == 0 {
		return s, nil, nil
	}
	next.Serial++
	return next, changes, nil
}

// retain aplica la retención (FR-013) al paquete tras agregar added: en
// cada suite se quedan las cfg.Repo.Keep versiones más altas (todas las
// arquitecturas juntas). Una versión que sale de todas sus suites se quita
// del estado. Avisa si added excluye una suite en la que estaba la versión
// vigente (Edge Cases, exclusión nueva).
func (s *State) retain(cfg *config.Config, pkg string, added Published) ([]Change, error) {
	var changes []Change
	versions := s.Packages[pkg]
	if err := sortVersions(versions); err != nil {
		return nil, err
	}
	// Aviso de exclusión nueva: la versión vigente anterior en cada suite.
	for _, su := range cfg.Suites {
		if contains(added.Suites, su.Name) {
			continue
		}
		for _, p := range versions {
			if p.Version != added.Version && contains(p.Suites, su.Name) {
				changes = append(changes, Change{Kind: KindKeep, Package: pkg, Version: added.Version,
					Suites: []string{su.Name}, Reason: fmt.Sprintf("excluida en %s; los usuarios de %s se quedan en %s",
						added.Version, su.Name, p.Version)})
				break
			}
		}
	}
	lost := map[string][]string{} // versión -> suites que pierde
	for _, su := range cfg.Suites {
		n := 0
		for i := range versions {
			if !contains(versions[i].Suites, su.Name) {
				continue
			}
			n++
			if n > cfg.Repo.Keep {
				versions[i].Suites = without(versions[i].Suites, su.Name)
				lost[versions[i].Version] = append(lost[versions[i].Version], su.Name)
			}
		}
	}
	kept := versions[:0]
	for _, p := range versions {
		if l := lost[p.Version]; len(l) > 0 {
			changes = append(changes, Change{Kind: KindRemove, Package: pkg, Version: p.Version, Suites: l,
				Reason: fmt.Sprintf("retención (keep %d)", cfg.Repo.Keep)})
		}
		if len(p.Suites) > 0 {
			kept = append(kept, p)
		}
	}
	s.Packages[pkg] = kept
	return changes, nil
}

// retire quita una versión de todas las suites y la registra como retirada
// (FR-014–FR-016).
func (s *State) retire(o Retire, now time.Time, fail func(pkg, version, format string, args ...any)) []Change {
	p, ok := s.Find(o.Package, o.Version)
	if !ok {
		if r, was := s.FindRetired(o.Package, o.Version); was {
			fail(o.Package, o.Version, "ya está retirada desde el %s", r.Date.UTC().Format(time.DateOnly))
		} else {
			fail(o.Package, o.Version, "no está publicada")
		}
		return nil
	}
	if len(s.Packages[o.Package]) == 1 && !o.Last {
		fail(o.Package, o.Version, "es la única versión publicada de %s; usa --last para retirarla", o.Package)
		return nil
	}
	if strings.TrimSpace(o.Reason) == "" || strings.ContainsAny(o.Reason, "\n\r") {
		fail(o.Package, o.Version, "el motivo del retiro debe ser una línea no vacía")
		return nil
	}
	var rest []Published
	for _, q := range s.Packages[o.Package] {
		if q.Version != o.Version {
			rest = append(rest, q)
		}
	}
	if len(rest) == 0 {
		delete(s.Packages, o.Package)
	} else {
		s.Packages[o.Package] = rest
	}
	s.Retired = append(s.Retired, Retired{Package: o.Package, Version: o.Version,
		Date: now.UTC().Truncate(time.Second), Reason: o.Reason})
	return []Change{{Kind: KindRemove, Package: o.Package, Version: o.Version, Suites: p.Suites,
		Reason: "retiro (" + o.Reason + ")"}}
}

func without(list []string, x string) []string {
	var out []string
	for _, v := range list {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}
