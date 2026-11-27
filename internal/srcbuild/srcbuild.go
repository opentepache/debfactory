// Package srcbuild compila paquetes de nivel 2 en un contenedor limpio de
// cada suite y arquitectura (specs/008-source-builds, research D1′): las
// dependencias de compilación se instalan con red desde los repos oficiales
// de la imagen, la red se desconecta y dpkg-buildpackage compila sin red en
// un tmpfs, como usuario sin privilegios. El contenedor no monta <data> ni
// credenciales: solo la entrada (de lectura) y la salida sale con docker cp.
package srcbuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/container"
	"debfactory/internal/srcpkg"
)

// imageMaxAge: las imágenes de construcción se renuevan cada semana
// (research D16); los índices de APT se actualizan en cada compilación.
const imageMaxAge = 7 * 24 * time.Hour

// buildTimeout limita una compilación (arm64 va por emulación).
const buildTimeout = 90 * time.Minute

// BuildPath es la ruta fija de compilación, la de los buildd de Debian: quien
// recompile usa la misma (research D8, FR-013).
const BuildPath = "/build/reproducible-path"

// Compressor es la compresión de los .deb de nivel 2 en todas las suites.
const Compressor = "xz"

// builder es el usuario sin privilegios que compila.
const builder = "debfactory"

// ImageTag es la imagen de construcción de una suite y arquitectura.
func ImageTag(suite, arch string) string { return "debfactory-build/" + suite + ":" + arch }

func dockerfile(base string) string {
	return "FROM " + base + "\n" +
		"RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends " +
		"build-essential fakeroot dpkg-dev debhelper xz-utils && rm -rf /var/lib/apt/lists/* && " +
		"useradd --create-home --shell /bin/sh " + builder + "\n"
}

// EnsureImage prepara debfactory-build/<suite>:<arch> (research D16): la
// imagen oficial de la suite con las herramientas de compilación precargadas
// (en arm64 emulado instalarlas es lo más lento).
func EnsureImage(ctx context.Context, s config.Suite, arch string) error {
	return container.EnsureImage(ctx, ImageTag(s.Name, arch), dockerfile(s.Image), container.Platform(arch), imageMaxAge)
}

// HostArch es la arquitectura Debian del host.
func HostArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

// Source es el paquete fuente generado para una suite.
type Source struct {
	Dir       string // directorio con los tres archivos
	DSC       string // <name>_<version>.dsc
	DebianTar string // <name>_<version>.debian.tar.xz
	Orig      string // <name>_<upstream>.orig.tar.<ext>
}

// GenerateSource genera el paquete fuente de una suite una sola vez
// (research D6), con dpkg-source de esa suite, sin red y con
// SOURCE_DATE_EPOCH del changelog. Las compilaciones consumen este .dsc y es
// el que se publica (constitución II). work debe estar vacío.
func GenerateSource(ctx context.Context, s config.Suite, in srcpkg.Input, work string) (*Source, error) {
	inDir := filepath.Join(work, "in")
	if err := srcpkg.Prepare(in, inDir); err != nil {
		return nil, err
	}
	arch := HostArch()
	if err := EnsureImage(ctx, s, arch); err != nil {
		return nil, fmt.Errorf("imagen de construcción: %v", err)
	}
	id, err := container.Start(ctx, container.Opts{
		Image:    ImageTag(s.Name, arch),
		Platform: container.Platform(arch),
		Network:  "none",
		Tmpfs:    []string{"/build:exec"},
		Mounts:   []container.Mount{{Src: inDir, Dst: "/in", ReadOnly: true}},
	})
	if err != nil {
		return nil, err
	}
	defer container.Remove(id)
	env := []string{"SOURCE_DATE_EPOCH=" + strconv.FormatInt(in.Epoch, 10), "LC_ALL=C.UTF-8", "TZ=UTC"}
	res, err := container.Exec(ctx, id, env, []string{"sh", "-c", srcpkg.Script, "sh", in.OrigName, in.TreeDir(), "/build/out"})
	if err := failure("dpkg-source -b", res, err); err != nil {
		return nil, err
	}
	out := filepath.Join(work, "source")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	if err := container.CopyOut(ctx, id, "/build/out", out); err != nil {
		return nil, err
	}
	src := &Source{Dir: out, DSC: in.Name + "_" + in.Version + ".dsc",
		DebianTar: in.Name + "_" + in.Version + ".debian.tar.xz", Orig: in.OrigName}
	for _, f := range []string{src.DSC, src.DebianTar, src.Orig} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			return nil, fmt.Errorf("dpkg-source -b no produjo %s", f)
		}
	}
	return src, nil
}

// Spec describe una compilación.
type Spec struct {
	Suite   config.Suite
	Arch    string
	Source  *Source
	Tree    string // <name>-<upstream>
	Package string
	Version string // con sufijo
	Epoch   int64
	Tmpfs   string // tamaño del tmpfs, p. ej. 6g
	Variant int    // 0 = entorno normal; 1 = otro entorno (doble compilación, research D8)
}

// Binary es el resultado de una compilación.
type Binary struct {
	Deb       string // ruta en el host
	Buildinfo string
}

// variants son los entornos de la doble compilación: misma ruta, distinto
// umask, zona horaria, idioma y hostname (probado en el spike, research D1).
var variants = []struct{ umask, tz, lang, hostname string }{
	{"022", "UTC", "C.UTF-8", "debfactory-build"},
	{"077", "Asia/Tokyo", "C", "otro-host"},
}

// buildScript compila como el usuario sin privilegios; $1 = .dsc, $2 = árbol,
// $3 = umask.
const buildScript = `set -eu
umask "$3"
cd ` + BuildPath + `
dpkg-source -x "/build/in/$1" "$2"
cd "$2"
dpkg-buildpackage -b -us -uc
`

// collectScript reúne la salida como root; $1 = paquete.
const collectScript = `set -eu
mkdir -p /build/out
cd ` + BuildPath + `
cp ./*.deb ./*.buildinfo /build/out/
`

// Build compila el .dsc de spec.Source en un contenedor nuevo y deja el .deb
// y el .buildinfo en out.
func Build(ctx context.Context, spec Spec, out string) (*Binary, error) {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	v := variants[spec.Variant%len(variants)]
	if err := EnsureImage(ctx, spec.Suite, spec.Arch); err != nil {
		return nil, fmt.Errorf("imagen de construcción: %v", err)
	}
	id, err := container.Start(ctx, container.Opts{
		Image:    ImageTag(spec.Suite.Name, spec.Arch),
		Platform: container.Platform(spec.Arch),
		Network:  "bridge",
		Hostname: v.hostname,
		Tmpfs:    []string{"/build:size=" + spec.Tmpfs + ",exec"},
		Mounts:   []container.Mount{{Src: spec.Source.Dir, Dst: "/in", ReadOnly: true}},
	})
	if err != nil {
		return nil, err
	}
	defer container.Remove(id)

	// Con red: copiar la entrada (el usuario que compila no lee el montaje
	// en Docker rootless) e instalar las Build-Depends del .dsc desde los
	// repos oficiales de la imagen.
	apt := []string{"DEBIAN_FRONTEND=noninteractive"}
	setup := `set -eu
cp -r /in /build/in
mkdir -p ` + BuildPath + `
chown -R ` + builder + ` /build/in ` + BuildPath + `
apt-get update
apt-get build-dep -y --no-install-recommends "/build/in/$1"
`
	res, err := container.Exec(ctx, id, apt, []string{"sh", "-c", setup, "sh", spec.Source.DSC})
	if err := failure("dependencias de compilación", res, err); err != nil {
		return nil, err
	}

	// Sin red: el build no puede descargar nada (FR-005, research D5).
	if err := container.DisconnectNetwork(ctx, id); err != nil {
		return nil, err
	}
	if res, err := container.Exec(ctx, id, nil, []string{"getent", "hosts", "deb.debian.org"}); err == nil && res.ExitCode == 0 {
		return nil, errors.New("el contenedor sigue con red tras desconectarlo")
	}
	// xz en todas las suites: el dpkg de Ubuntu comprime con zstd por
	// defecto y lintian (y Debian) lo rechazan como malformed-deb-archive.
	// La guía de recompilación fija lo mismo (FR-013).
	env := []string{"SOURCE_DATE_EPOCH=" + strconv.FormatInt(spec.Epoch, 10), "TZ=" + v.tz, "LANG=" + v.lang,
		"HOME=/home/" + builder, "DPKG_DEB_COMPRESSOR_TYPE=" + Compressor}
	res, err = container.ExecAs(ctx, id, builder, env, []string{"sh", "-c", buildScript, "sh", spec.Source.DSC, spec.Tree, v.umask})
	if err := failure("dpkg-buildpackage", res, err); err != nil {
		return nil, err
	}
	res, err = container.Exec(ctx, id, nil, []string{"sh", "-c", collectScript, "sh", spec.Package})
	if err := failure("salida", res, err); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	if err := container.CopyOut(ctx, id, "/build/out", out); err != nil {
		return nil, err
	}
	b := &Binary{
		Deb:       filepath.Join(out, fmt.Sprintf("%s_%s_%s.deb", spec.Package, spec.Version, spec.Arch)),
		Buildinfo: filepath.Join(out, fmt.Sprintf("%s_%s_%s.buildinfo", spec.Package, spec.Version, spec.Arch)),
	}
	for _, f := range []string{b.Deb, b.Buildinfo} {
		if _, err := os.Stat(f); err != nil {
			return nil, fmt.Errorf("dpkg-buildpackage no produjo %s (¿Package: distinto del fuente?)", filepath.Base(f))
		}
	}
	return b, nil
}

// failure resume una orden fallida con el final de su salida.
func failure(what string, res container.Result, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %v", what, err)
	}
	if res.ExitCode != 0 {
		out := append(append([]byte{}, res.Stdout...), res.Stderr...)
		return &Failure{What: what, Code: res.ExitCode, Tail: container.TailLines(out, 25)}
	}
	return nil
}

// Failure es una orden que falló dentro del contenedor, con el final del log.
type Failure struct {
	What string
	Code int
	Tail []string
}

func (f *Failure) Error() string {
	return fmt.Sprintf("%s: código %d: %s", f.What, f.Code, strings.Join(lastN(f.Tail, 3), " | "))
}

func lastN(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
