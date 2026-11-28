// Package srcpkg prepara el paquete fuente de un paquete de nivel 2
// (specs/008-source-builds, research D6): el tarball de upstream verificado
// como .orig, la carpeta debian/ del manifiesto y un debian/changelog por
// suite. El .dsc lo genera dpkg-source dentro del contenedor de la suite
// (internal/srcbuild), una sola vez por suite.
package srcpkg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/supply"
)

// Input describe el paquete fuente de una suite.
type Input struct {
	Name       string // paquete fuente y binario
	Upstream   string // versión upstream
	Version    string // versión Debian con sufijo: <upstream>-0~tepache<N>+<sufijo>
	Suite      string // distribución del changelog
	Maintainer string // config.maintainer
	Epoch      int64  // publicación del release upstream (lockfile)
	Orig       string // tarball de upstream verificado (host)
	OrigName   string // <name>_<upstream>.orig.tar.<ext>
	DebianDir  string // packages/<name>/debian (host)

	// Fase 009 (specs/009-hosted-hardened-builds, research D3).
	// Components son tarballs adicionales del paquete fuente ya preparados:
	// nombre del componente -> archivo (p. ej. "zigdeps" -> …orig-zigdeps.tar.xz).
	Components map[string]string
	// Toolchain es el contenido de debian/upstream/toolchain (vacío si no hay).
	Toolchain string
}

// ComponentName es el nombre del tarball de un componente:
// <name>_<upstream>.orig-<componente>.tar.xz.
func ComponentName(name, upstream, component string) string {
	return fmt.Sprintf("%s_%s.orig-%s.tar.xz", name, upstream, component)
}

// Changelog es el debian/changelog de una suite (FR-002, research D6). La
// fecha es la del release upstream: fija, alimenta SOURCE_DATE_EPOCH.
func Changelog(in Input) []byte {
	date := time.Unix(in.Epoch, 0).UTC().Format(time.RFC1123Z)
	return fmt.Appendf(nil, "%s (%s) %s; urgency=medium\n\n  * OpenTepache build of %s for %s.\n\n -- %s  %s\n",
		in.Name, in.Version, in.Suite, in.Upstream, in.Suite, in.Maintainer, date)
}

// TreeDir es el nombre del árbol del código fuente: <name>-<upstream>.
func (in Input) TreeDir() string { return in.Name + "-" + in.Upstream }

// Prepare llena dir con la entrada del contenedor que genera el .dsc:
//
//	dir/<orig name>          el tarball de upstream, byte a byte
//	dir/debian/              la carpeta del manifiesto + changelog (+ source/format)
//
// No sigue enlaces (CheckDebianDir ya los rechaza) y no copia changelog.
func Prepare(in Input, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := copyFile(in.Orig, filepath.Join(dir, in.OrigName), 0o644); err != nil {
		return err
	}
	deb := filepath.Join(dir, "debian")
	err := filepath.WalkDir(in.DebianDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(in.DebianDir, p)
		dst := filepath.Join(deb, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(dst, 0o755)
		case !d.Type().IsRegular():
			return fmt.Errorf("%s: solo se admiten archivos", p)
		case rel == "changelog":
			return fmt.Errorf("%s: el changelog lo genera la herramienta", p)
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if st.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		return copyFile(p, dst, mode)
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(deb, "changelog"), Changelog(in), 0o644); err != nil {
		return err
	}
	if in.Toolchain != "" {
		if err := os.MkdirAll(filepath.Join(deb, "upstream"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(deb, "upstream", "toolchain"), []byte(in.Toolchain), 0o644); err != nil {
			return err
		}
	}
	for comp, src := range in.Components {
		if err := copyFile(src, filepath.Join(dir, ComponentName(in.Name, in.Upstream, comp)), 0o644); err != nil {
			return err
		}
	}
	format := filepath.Join(deb, "source", "format")
	if _, err := os.Stat(format); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(format), 0o755); err != nil {
			return err
		}
		return os.WriteFile(format, []byte("3.0 (quilt)\n"), 0o644)
	} else if err != nil {
		return err
	}
	data, err := os.ReadFile(format)
	if err != nil {
		return err
	}
	if f := strings.TrimSpace(string(data)); f != "3.0 (quilt)" {
		return fmt.Errorf("debian/source/format: %q; el nivel 2 usa 3.0 (quilt)", f)
	}
	return nil
}

// Script es la orden (sh) que genera el paquete fuente dentro del
// contenedor, sin red: desempaqueta el .orig en <tree>, añade debian/ y
// ejecuta dpkg-source -b. Los valores van como argumentos posicionales:
// $1 = nombre del .orig, $2 = árbol, $3 = salida.
const Script = `set -eu
cd /build
mkdir "$2"
tar -xf "/in/$1" -C "$2" --strip-components=1
for c in /in/*.orig-*.tar.*; do
  [ -e "$c" ] || continue
  tar -xf "$c" -C "$2"
  cp "$c" .
done
cp -r /in/debian "$2/debian"
cp "/in/$1" .
dpkg-source -b "$2"
mkdir -p "$3"
cp ./*.dsc ./*.debian.tar.* ./*.orig*.tar.* "$3/"
`

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// BuildComponent escribe en dst el tarball determinista de un componente
// (research D3): el árbol de dir bajo <componente>/, orden fijo, dueño 0:0,
// mtime fijo y xz -9 de un solo hilo (xz de Debian), para que sea el mismo
// archivo en todas las suites y en cada ejecución.
func BuildComponent(ctx context.Context, dir, component string, mtime time.Time, dst string) error {
	var tarBuf bytes.Buffer
	if err := supply.WriteTree(&tarBuf, dir, component, mtime); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "xz", "-9", "-T1", "-c")
	cmd.Stdin = &tarBuf
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	cmd.Stdout = out
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		out.Close()
		return fmt.Errorf("xz: %v: %s", err, errb.String())
	}
	return out.Close()
}
