package repo

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/fetch"
	"debfactory/internal/report"
	"debfactory/internal/sign"
)

// VerifyTree comprueba una generación contra el estado: firmas de cada
// suite con el keyring publicado y la subllave configurada, índices contra
// sus hashes, paquetes y versiones exactos por suite y arquitectura con el
// sha256 del estado, Origin y Label configurados, y un Valid-Until posterior
// a now (FR-004; FR-030 de la fase 003, que sustituye a FR-004a).
func VerifyTree(ctx context.Context, cfg *config.Config, st *State, genDir string, now time.Time) error {
	var errs report.Errors
	fail := func(suite, format string, args ...any) {
		errs = append(errs, &report.Error{Suite: suite, Stage: report.VerifyRepo, Cause: fmt.Sprintf(format, args...)})
	}
	keyring := filepath.Join(genDir, "debfactory.gpg")
	if _, err := os.Stat(keyring); err != nil {
		fail("", "falta debfactory.gpg")
		return errs
	}
	poolSHA := map[string]string{} // ruta relativa -> sha256 (caché)
	shaOf := func(rel string) (string, error) {
		if s, ok := poolSHA[rel]; ok {
			return s, nil
		}
		s, err := fetch.SHA256File(filepath.Join(genDir, rel))
		poolSHA[rel] = s
		return s, err
	}
	for _, su := range cfg.Suites {
		dists := filepath.Join(genDir, "dists", su.Name)
		if err := gpgvSigner(ctx, keyring, cfg.Repo.Signing.Subkey, filepath.Join(dists, "InRelease")); err != nil {
			fail(su.Name, "InRelease: %v", err)
			continue
		}
		if err := gpgvSigner(ctx, keyring, cfg.Repo.Signing.Subkey, filepath.Join(dists, "Release.gpg"),
			filepath.Join(dists, "Release")); err != nil {
			fail(su.Name, "Release.gpg: %v", err)
		}
		rel, err := os.ReadFile(filepath.Join(dists, "Release"))
		if err != nil {
			fail(su.Name, "%v", err)
			continue
		}
		fields, sums := parseRelease(rel)
		switch vu, err := sign.ValidUntil(rel); {
		case err != nil:
			fail(su.Name, "Valid-Until ilegible: %v", err)
		case vu.IsZero():
			fail(su.Name, "Release sin Valid-Until (FR-030)")
		case !vu.After(now):
			fail(su.Name, "la generación caducó el %s (Valid-Until)", vu.Format(time.DateOnly))
		}
		if fields["Acquire-By-Hash"] != "yes" {
			fail(su.Name, "Release sin Acquire-By-Hash: yes")
		}
		if fields["Origin"] != cfg.Repo.Origin || fields["Label"] != cfg.Repo.Label {
			fail(su.Name, "Release con Origin %q y Label %q; se esperaba %q (constitución, alcance)",
				fields["Origin"], fields["Label"], cfg.Repo.Origin)
		}
		for path, sha := range sums {
			got, err := fetch.SHA256File(filepath.Join(dists, path))
			switch {
			case err == nil && got != sha:
				fail(su.Name, "el índice %s no coincide con Release", path)
			case err != nil && !hasCompressed(sums, dists, path):
				// Release lista también la variante sin comprimir; basta con
				// que exista una comprimida, que es la que pide apt.
				fail(su.Name, "falta el índice %s", path)
			}
		}
		for _, arch := range cfg.Arches {
			want := map[string]string{} // "pkg version" -> sha
			for _, name := range st.PackageNames() {
				for _, p := range st.Packages[name] {
					if contains(p.Suites, su.Name) && contains(p.ArchList(cfg.Arches), arch) {
						want[name+" "+p.SuiteVersion(su.Name)] = p.DebSHA(su.Name, arch)
					}
				}
			}
			data, err := os.ReadFile(filepath.Join(dists, cfg.Repo.Component, "binary-"+arch, "Packages"))
			if err != nil {
				fail(su.Name, "falta Packages de %s", arch)
				continue
			}
			got := map[string]bool{}
			for _, stz := range parseStanzas(data) {
				key := stz["Package"] + " " + stz["Version"]
				got[key] = true
				sha, ok := want[key]
				if !ok {
					fail(su.Name, "%s %s: %s no está en el estado", arch, stz["Package"], stz["Version"])
					continue
				}
				if stz["SHA256"] != sha {
					fail(su.Name, "%s %s: sha256 de Packages no coincide con el estado", arch, key)
				}
				if s, err := shaOf(stz["Filename"]); err != nil || s != sha {
					fail(su.Name, "%s %s: %s falta o no coincide con el estado", arch, key, stz["Filename"])
				}
			}
			var missing []string
			for k := range want {
				if !got[k] {
					missing = append(missing, k)
				}
			}
			sort.Strings(missing)
			for _, k := range missing {
				fail(su.Name, "%s: falta %s en Packages", arch, k)
			}
		}
		if st.schemaFor() == 2 {
			for _, msg := range verifySources(cfg, st, su.Name, dists, genDir, shaOf) {
				fail(su.Name, "%s", msg)
			}
		}
	}
	return errs.Err()
}

// verifySources comprueba el nivel 2 de una suite (research D10): el índice
// Sources lista exactamente las versiones de nivel 2 del estado, con el
// .dsc, el .debian.tar.xz y el .orig del estado, que están en el pool con
// ese sha256, y cada .buildinfo está junto a su .deb.
func verifySources(cfg *config.Config, st *State, suite, dists, genDir string, shaOf func(string) (string, error)) []string {
	var msgs []string
	data, err := readIndex(filepath.Join(dists, cfg.Repo.Component, "source", "Sources"))
	if err != nil {
		return []string{"falta el índice Sources: " + err.Error()}
	}
	type src struct {
		dir   string
		files map[string]string // nombre -> sha256
	}
	got := map[string]src{}
	for _, stz := range parseStanzas(data) {
		files := map[string]string{}
		for _, line := range strings.Split(stz["Checksums-Sha256"], "\n") {
			if f := strings.Fields(line); len(f) == 3 {
				files[f[2]] = f[0]
			}
		}
		got[stz["Package"]+" "+stz["Version"]] = src{dir: stz["Directory"], files: files}
	}
	want := 0
	for _, name := range st.PackageNames() {
		for _, p := range st.Packages[name] {
			if !p.Level2() || !contains(p.Suites, suite) {
				continue
			}
			want++
			b := p.Builds[suite]
			key := name + " " + b.Version
			g, ok := got[key]
			if !ok {
				msgs = append(msgs, "falta "+key+" en Sources")
				continue
			}
			for _, a := range p.Artifacts(name, cfg.Arches) {
				if a.Suite != "" && a.Suite != suite {
					continue
				}
				switch a.Kind {
				case "dsc", "debian_tar", "orig":
					if g.files[a.Name] != a.SHA256 {
						msgs = append(msgs, fmt.Sprintf("%s: %s no coincide con el estado en Sources", key, a.Name))
					}
					if s, err := shaOf(filepath.Join(g.dir, a.Name)); err != nil || s != a.SHA256 {
						msgs = append(msgs, fmt.Sprintf("%s: %s falta en el pool o no coincide", key, a.Name))
					}
				case "buildinfo":
					if s, err := shaOf(filepath.Join(g.dir, a.Name)); err != nil || s != a.SHA256 {
						msgs = append(msgs, fmt.Sprintf("%s: %s falta en el pool o no coincide", key, a.Name))
					}
				}
			}
		}
	}
	if len(got) != want {
		msgs = append(msgs, fmt.Sprintf("Sources tiene %d paquetes fuente; el estado, %d", len(got), want))
	}
	return msgs
}

// readIndex lee un índice sin comprimir o, si no está, su variante .gz.
func readIndex(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	fh, err := os.Open(path + ".gz")
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}

// gpgvSigner verifica sig (y data, si es firma separada) con keyring y
// exige que la firma sea de la subllave subkey.
func gpgvSigner(ctx context.Context, keyring, subkey string, files ...string) error {
	args := append([]string{"--status-fd", "1", "--keyring", keyring}, files...)
	cmd := exec.CommandContext(ctx, "gpgv", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("la firma no verifica con debfactory.gpg")
	}
	var signers []string
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) > 2 && f[1] == "VALIDSIG" {
			signers = append(signers, f[2])
		}
	}
	for _, s := range signers {
		if s == subkey {
			return nil
		}
	}
	return fmt.Errorf("firmado por %v, no por la subllave configurada %s", signers, subkey)
}

// parseRelease devuelve los campos de un Release y su sección SHA256.
func parseRelease(data []byte) (map[string]string, map[string]string) {
	fields, sums := map[string]string{}, map[string]string{}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' {
			if section == "SHA256" {
				if f := strings.Fields(line); len(f) == 3 {
					sums[f[2]] = f[0]
				}
			}
			continue
		}
		k, v, _ := strings.Cut(line, ":")
		section = k
		fields[k] = strings.TrimSpace(v)
	}
	return fields, sums
}

// parseStanzas lee un archivo de control deb822 (Packages).
func parseStanzas(data []byte) []map[string]string {
	var out []map[string]string
	cur := map[string]string{}
	last := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = map[string]string{}
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			// Continuación (Checksums-Sha256 de Sources): una línea por archivo.
			if last != "" {
				cur[last] += "\n" + strings.TrimSpace(line)
			}
			continue
		}
		k, v, _ := strings.Cut(line, ":")
		cur[k] = strings.TrimSpace(v)
		last = k
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// hasCompressed indica si existe alguna variante comprimida de path listada
// en Release.
func hasCompressed(sums map[string]string, dists, path string) bool {
	for _, ext := range []string{".gz", ".bz2", ".xz"} {
		if _, ok := sums[path+ext]; ok {
			if _, err := os.Stat(filepath.Join(dists, path+ext)); err == nil {
				return true
			}
		}
	}
	return false
}

// TreeValidUntil devuelve el Valid-Until más temprano de las suites de una
// generación.
func TreeValidUntil(cfg *config.Config, genDir string) (time.Time, error) {
	var first time.Time
	for _, su := range cfg.Suites {
		rel, err := os.ReadFile(filepath.Join(genDir, "dists", su.Name, "Release"))
		if err != nil {
			return time.Time{}, err
		}
		vu, err := sign.ValidUntil(rel)
		if err != nil {
			return time.Time{}, err
		}
		if first.IsZero() || (!vu.IsZero() && vu.Before(first)) {
			first = vu
		}
	}
	return first, nil
}

// WarnValidity emite el evento "expiry" si a validUntil le quedan menos de
// repo.validity.warn_days días (FR-032 de la fase 003). No hace fallar: la
// caducidad ya vencida la rechaza VerifyTree.
func WarnValidity(out *report.Printer, cfg *config.Config, where string, validUntil, now time.Time) {
	if validUntil.IsZero() {
		return
	}
	days := int(validUntil.Sub(now).Hours() / 24)
	date := validUntil.UTC().Format(time.DateOnly)
	if days < cfg.Repo.Validity.WarnDays {
		out.Event("expiry", map[string]any{"where": where, "valid_until": date, "days": days},
			fmt.Sprintf("aviso: %s caduca el %s (en %d días); ejecuta `debfactory ci refresh` o `repo rebuild`", where, date, days))
	}
}
