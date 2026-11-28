package supply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"debfactory/internal/container"
	"debfactory/internal/lock"
	"debfactory/internal/zon"
)

// ZigFetcher obtiene una dependencia con `zig fetch` y devuelve el hash que
// calcula Zig y la ruta del paquete en su caché (p/<hash>.tar.gz).
type ZigFetcher interface {
	Fetch(ctx context.Context, url string) (hash, cached string, err error)
}

// HashMismatchError es una dependencia cuyo contenido no da el hash que
// declara upstream (FR-002).
type HashMismatchError struct {
	Name, URL, Declared, Got string
}

func (e *HashMismatchError) Error() string {
	return fmt.Sprintf("dependencia %s (%s): upstream declara %s y el contenido da %s", e.Name, e.URL, e.Declared, e.Got)
}

// ResolveZigDeps resuelve el cierre transitivo de las dependencias de Zig
// (research D2): lee los build.zig.zon del árbol del fuente (salvo exclude),
// obtiene cada dependencia remota por separado con f, exige el hash declarado
// por upstream, la extrae en out/<hash>/ y sigue con su build.zig.zon y los
// de sus dependencias locales, en rondas, hasta que no aparece nada nuevo.
// Devuelve las dependencias ordenadas por hash con el sha256 de su árbol.
//
// skip son nombres de dependencias que el build del paquete no usa
// (deps.skip): no se bajan ni se recorren, y cada nombre MUST aparecer en
// algún build.zig.zon (si no, el manifiesto está desactualizado). Si alguna
// hacía falta, la compilación sin red falla (falla cerrado).
//
// store (opcional) guarda cada dependencia verificada y, si store.Expected
// trae el sha256 del árbol (del lockfile), reutiliza la copia guardada solo
// cuando su árbol da exactamente ese sha256; si no, la baja como siempre.
func ResolveZigDeps(ctx context.Context, src string, exclude []string, skip map[string]string, f ZigFetcher, out string, store *DepStore) ([]lock.Dep, error) {
	skipSeen := map[string]bool{}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	var queue []string
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() && rel != "." {
			for _, ex := range exclude {
				if strings.HasPrefix(rel+"/", ex) {
					return filepath.SkipDir
				}
			}
		}
		if !d.IsDir() && d.Name() == "build.zig.zon" {
			queue = append(queue, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Dependencias locales omitidas (p. ej. pkg/libintl): su carpeta no se
	// recorre, así que tampoco sus descargas.
	var skippedDirs []string
	for _, z := range queue {
		data, err := os.ReadFile(z)
		if err != nil {
			return nil, err
		}
		deps, err := zon.Dependencies(data)
		if err != nil {
			continue // se informa al recorrerlo
		}
		for _, d := range deps {
			if _, ok := skip[d.Name]; ok && d.Path != "" {
				skipSeen[d.Name] = true
				skippedDirs = append(skippedDirs, filepath.Join(filepath.Dir(z), filepath.FromSlash(d.Path))+string(filepath.Separator))
			}
		}
	}
	queue = slices.DeleteFunc(queue, func(z string) bool {
		for _, d := range skippedDirs {
			if strings.HasPrefix(z, d) {
				return true
			}
		}
		return false
	})
	sort.Strings(queue)
	seenZon := map[string]bool{}
	done := map[string]lock.Dep{}
	for len(queue) > 0 {
		var next []string
		for _, z := range queue {
			if seenZon[z] {
				continue
			}
			seenZon[z] = true
			data, err := os.ReadFile(z)
			if err != nil {
				return nil, err
			}
			deps, err := zon.Dependencies(data)
			if err != nil {
				rel, _ := filepath.Rel(src, z)
				return nil, fmt.Errorf("%s: %v", rel, err)
			}
			for _, d := range deps {
				if _, ok := skip[d.Name]; ok && d.Path != "" {
					continue
				}
				if d.Path != "" {
					// Dependencia local: su build.zig.zon también cuenta.
					local := filepath.Join(filepath.Dir(z), filepath.FromSlash(d.Path), "build.zig.zon")
					if _, err := os.Stat(local); err == nil && !seenZon[local] {
						next = append(next, local)
					}
					continue
				}
				if _, ok := skip[d.Name]; ok {
					skipSeen[d.Name] = true
					continue
				}
				if _, ok := done[d.Hash]; ok {
					continue
				}
				dir := filepath.Join(out, d.Hash)
				sum, reused, err := store.reuse(d.Hash, dir)
				if err != nil {
					return nil, err
				}
				if !reused {
					got, cached, err := f.Fetch(ctx, d.URL)
					if err != nil {
						return nil, fmt.Errorf("dependencia %s (%s): %v", d.Name, d.URL, err)
					}
					if got != d.Hash {
						return nil, &HashMismatchError{Name: d.Name, URL: d.URL, Declared: d.Hash, Got: got}
					}
					if err := ExtractZigCache(cached, dir); err != nil {
						return nil, err
					}
					if sum, err = TreeSHA256(dir); err != nil {
						return nil, err
					}
					store.save(d.Hash, dir)
				}
				done[d.Hash] = lock.Dep{Name: d.Name, URL: d.URL, Hash: d.Hash, SHA256: sum}
				if root := filepath.Join(dir, "build.zig.zon"); fileExists(root) {
					next = append(next, root)
				}
			}
		}
		sort.Strings(next)
		queue = slices.Compact(next)
	}
	var unknown []string
	for n := range skip {
		if !skipSeen[n] {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("deps.skip: %s no aparece en ningún build.zig.zon", strings.Join(unknown, ", "))
	}
	var list []lock.Dep
	for _, d := range done {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Hash < list[j].Hash })
	return list, nil
}

// VerifyZigDeps comprueba que out/<hash>/ tiene exactamente las dependencias
// del lockfile con su sha256 (cada compilación vuelve a resolverlas con red y
// las compara; FR-003).
func VerifyZigDeps(out string, want []lock.Dep) error {
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, e := range entries {
		have[e.Name()] = true
	}
	var errs []string
	for _, d := range want {
		if !have[d.Hash] {
			errs = append(errs, "falta "+d.Hash)
			continue
		}
		got, err := TreeSHA256(filepath.Join(out, d.Hash))
		if err != nil {
			return err
		}
		if got != d.SHA256 {
			errs = append(errs, fmt.Sprintf("%s: sha256 %s, el lockfile dice %s", d.Hash, got, d.SHA256))
		}
		delete(have, d.Hash)
	}
	for h := range have {
		errs = append(errs, "sobra "+h)
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New("dependencias de Zig distintas del lockfile: " + strings.Join(errs, "; "))
	}
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ContainerZigFetcher ejecuta `zig fetch` del toolchain verificado dentro de un
// contenedor de Debian con red, como el usuario del host (ningún binario de
// upstream corre en el host). Close lo elimina.
type ContainerZigFetcher struct {
	id    string
	cache string
}

// zigFetchImage es la imagen con certificados para zig fetch.
const zigFetchImage = "debfactory-zigfetch"

// NewContainerZigFetcher arranca el contenedor con el toolchain (directorio
// del host con el binario zig, solo lectura) y la caché de Zig en cache.
func NewContainerZigFetcher(ctx context.Context, toolchain, cache string) (*ContainerZigFetcher, error) {
	if err := container.EnsureImage(ctx, zigFetchImage,
		"FROM debian:trixie\nRUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*\n",
		"", imageMaxAge); err != nil {
		return nil, fmt.Errorf("imagen para zig fetch: %v", err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return nil, err
	}
	id, err := container.Start(ctx, container.Opts{
		Image:   zigFetchImage,
		Network: "bridge",
		User:    fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		Env:     []string{"HOME=/tmp", "ZIG_GLOBAL_CACHE_DIR=/cache"},
		Mounts: []container.Mount{{Src: toolchain, Dst: "/opt/toolchain", ReadOnly: true},
			{Src: cache, Dst: "/cache"}},
	})
	if err != nil {
		return nil, err
	}
	return &ContainerZigFetcher{id: id, cache: cache}, nil
}

// Fetch implementa ZigFetcher: un zig fetch por dependencia, sin paralelismo
// (research D2: en paralelo Zig da hashes distintos).
func (c *ContainerZigFetcher) Fetch(ctx context.Context, url string) (string, string, error) {
	res, err := container.Exec(ctx, c.id, nil, []string{"/opt/toolchain/zig", "fetch", url})
	if err != nil {
		return "", "", err
	}
	if res.ExitCode != 0 {
		return "", "", errors.New(strings.Join(container.TailLines(append(res.Stdout, res.Stderr...), 3), " | "))
	}
	lines := container.TailLines(res.Stdout, 1)
	if len(lines) == 0 {
		return "", "", errors.New("zig fetch no devolvió un hash")
	}
	hash := strings.TrimSpace(lines[0])
	cached := filepath.Join(c.cache, "p", hash+".tar.gz")
	if !fileExists(cached) {
		return "", "", fmt.Errorf("zig fetch no dejó %s en la caché", filepath.Base(cached))
	}
	return hash, cached, nil
}

// Close elimina el contenedor.
func (c *ContainerZigFetcher) Close() { container.Remove(c.id) }

// DepStore guarda dependencias de Zig ya verificadas entre ejecuciones.
type DepStore struct {
	Dir      string            // <caché>/zigdeps
	Expected map[string]string // hash -> sha256 del árbol (lockfile); nil = no reutilizar
}

// reuse copia la dependencia guardada a dir si su árbol da el sha256
// esperado. Una copia que no coincide se descarta.
func (s *DepStore) reuse(hash, dir string) (string, bool, error) {
	if s == nil || s.Expected[hash] == "" {
		return "", false, nil
	}
	saved := filepath.Join(s.Dir, hash)
	if _, err := os.Stat(saved); err != nil {
		return "", false, nil
	}
	sum, err := TreeSHA256(saved)
	if err != nil || sum != s.Expected[hash] {
		os.RemoveAll(saved)
		return "", false, nil
	}
	if err := copyTree(saved, dir); err != nil {
		os.RemoveAll(dir)
		return "", false, nil
	}
	// La copia también debe dar el sha256 esperado.
	if got, err := TreeSHA256(dir); err != nil || got != sum {
		os.RemoveAll(dir)
		return "", false, nil
	}
	return sum, true, nil
}

// save guarda una copia de la dependencia recién verificada (si falla, no
// pasa nada: la próxima vez se vuelve a bajar).
func (s *DepStore) save(hash, dir string) {
	if s == nil || s.Dir == "" {
		return
	}
	dst := filepath.Join(s.Dir, hash)
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return
	}
	tmp, err := os.MkdirTemp(s.Dir, ".tmp-")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmp)
	if copyTree(dir, filepath.Join(tmp, "d")) == nil {
		os.Rename(filepath.Join(tmp, "d"), dst)
	}
}

// copyTree copia src a dst (que no existe) conservando enlaces y permisos.
func copyTree(src, dst string) error {
	if out, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("cp -a: %v: %s", err, out)
	}
	return nil
}
