// Package lint pasa lintian dentro de un contenedor, con la versión de
// lintian que fija la imagen (research D9).
package lint

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/container"
	"debfactory/internal/report"
)

// Image es la imagen local con lintian.
const Image = "debfactory-lintian"

// imageMaxAge: la imagen se reconstruye (con lintian actualizado) cada semana.
const imageMaxAge = 7 * 24 * time.Hour

// dep5File es la revisión de lintian que parsea los .xml del fuente.
const dep5File = "/usr/share/lintian/lib/Lintian/Check/Debian/Copyright/Dep5.pm"

// dep5Patch corrige un fallo de lintian (specs/009): la revisión
// debian/copyright/dep5 parsea cada .xml del fuente con XML::LibXML
// expandiendo entidades, y un XML malicioso ("billion laughs", p. ej.
// test/recurse/lol_ep.xml de libxml2) la deja colgada para siempre. Solo lee
// /component/metadata_license, así que no necesita expandir entidades ni
// cargar DTD externos. La revisión sigue corriendo y no se silencia ninguna
// etiqueta (constitución VII). Si la línea ya no existe (lintian cambió), la
// imagen no se construye y hay que revisar el parche.
const dep5Patch = `RUN grep -q 'my $parser = XML::LibXML->new;' ` + dep5File + ` \
 && sed -i 's/my $parser = XML::LibXML->new;/my $parser = XML::LibXML->new(expand_entities => 0, load_ext_dtd => 0);/' ` + dep5File + ` \
 && grep -q 'expand_entities => 0, load_ext_dtd => 0' ` + dep5File + "\n"

func dockerfile(base string) string {
	return "FROM " + base + "\n" +
		"RUN apt-get update && apt-get install -y lintian && rm -rf /var/lib/apt/lists/*\n" +
		dep5Patch
}

// Result son las etiquetas que informó lintian.
type Result struct {
	Errors   []string // líneas "E: …"
	Warnings []string // líneas "W: …"
}

// Run pasa lintian a un .deb, sin red y con el .deb en solo lectura. Los
// overrides declarados ya van dentro del paquete; falla si lintian sale con
// error según config.lintian.fail_on.
func Run(ctx context.Context, cfg *config.Config, deb string) (*Result, error) {
	if err := container.EnsureImage(ctx, Image, dockerfile(cfg.Lintian.Image), "", imageMaxAge); err != nil {
		return nil, report.Newf(report.Lintian, "no se pudo preparar la imagen de lintian: %v", err)
	}
	abs, err := filepath.Abs(deb)
	if err != nil {
		return nil, err
	}
	limit := cfg.Lintian.TimeoutOrDefault()
	res, err := container.Run(ctx, container.Opts{
		Image:    Image,
		Network:  "none",
		ReadOnly: true,
		Tmpfs:    []string{"/tmp"},
		Env:      []string{"HOME=/tmp", "TMPDIR=/tmp"},
		Mounts:   []container.Mount{{Src: filepath.Dir(abs), Dst: "/work", ReadOnly: true}},
		// El límite va dentro del contenedor: vale aunque este proceso muera.
		Cmd: []string{"timeout", "--kill-after=30s", fmt.Sprintf("%ds", int(limit.Seconds())),
			"lintian", "--fail-on", cfg.Lintian.FailOn, "--no-cfg", "/work/" + filepath.Base(abs)},
	})
	if err != nil {
		return nil, report.Newf(report.Lintian, "%v", err)
	}
	if res.ExitCode == 124 || res.ExitCode == 137 {
		return nil, report.Newf(report.Lintian, "lintian superó el límite de %s (lintian.timeout) con %s",
			limit, filepath.Base(abs))
	}
	r := parse(res.Stdout)
	if res.ExitCode == 0 {
		return r, nil
	}
	detail := append(append([]string{}, r.Errors...), r.Warnings...)
	cause := "lintian encontró errores no cubiertos por overrides"
	if len(r.Errors) == 0 && cfg.Lintian.FailOn == "error" {
		cause = "lintian falló"
		detail = container.TailLines(append(res.Stdout, res.Stderr...), 15)
	}
	return r, &report.Error{Stage: report.Lintian, Cause: cause, Detail: detail}
}

func parse(out []byte) *Result {
	r := &Result{}
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "E: "):
			r.Errors = append(r.Errors, l)
		case strings.HasPrefix(l, "W: "):
			r.Warnings = append(r.Warnings, l)
		}
	}
	return r
}
