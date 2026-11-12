package installtest

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/container"
)

// Stages de una suite, en orden.
const (
	StageInstall   = "install"
	StageRun       = "run"
	StageVersion   = "version"
	StagePurge     = "purge"
	StageLeftovers = "leftovers"
)

// suiteTimeout limita cada suite (arm64 va por emulación).
const suiteTimeout = 15 * time.Minute

// testEnv es el entorno literal de la orden de comprobación (sin shell).
var testEnv = []string{
	"HOME=/tmp/debfactory-home",
	"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
}

// Deb es el paquete a probar.
type Deb struct {
	Path        string // ruta del .deb en el host
	Package     string // nombre del paquete
	Arch        string
	TestCommand string // orden y argumentos separados por espacios
	Expect      string // subcadena que debe aparecer en la salida
}

// RunSuite prueba el .deb en un contenedor limpio de la suite: instala con
// apt (resuelve dependencias), corta la red, ejecuta la orden de comprobación
// sin shell, comprueba la salida, purga y verifica que no queda ningún
// archivo del paquete. El contenedor se borra siempre.
func RunSuite(ctx context.Context, s config.Suite, d Deb) Result {
	start := time.Now()
	r := Result{Suite: s.Name, Status: "pass"}
	fail := func(stage, reason string) Result {
		r.Status, r.Stage, r.Reason = "fail", stage, reason
		r.Seconds = time.Since(start).Seconds()
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, suiteTimeout)
	defer cancel()

	if err := EnsureImage(ctx, s, d.Arch); err != nil {
		return fail(StageInstall, "imagen de prueba: "+err.Error())
	}
	abs, err := filepath.Abs(d.Path)
	if err != nil {
		return fail(StageInstall, err.Error())
	}
	id, err := container.Start(ctx, container.Opts{
		Image:    ImageTag(s.Name, d.Arch),
		Platform: container.Platform(d.Arch),
		Network:  "bridge",
		Mounts:   []container.Mount{{Src: filepath.Dir(abs), Dst: "/dist", ReadOnly: true}},
	})
	if err != nil {
		return fail(StageInstall, err.Error())
	}
	defer container.Remove(id)

	apt := []string{"DEBIAN_FRONTEND=noninteractive"}
	res, err := container.Exec(ctx, id, apt, []string{"apt-get", "install", "-y", "/dist/" + filepath.Base(abs)})
	if reason, bad := failed(res, err); bad {
		return fail(StageInstall, reason)
	}
	res, err = container.Exec(ctx, id, nil, []string{"dpkg", "-L", d.Package})
	if reason, bad := failed(res, err); bad {
		return fail(StageInstall, "dpkg -L: "+reason)
	}
	files, err := nonDirectories(ctx, id, strings.Fields(string(res.Stdout)))
	if err != nil {
		return fail(StageInstall, err.Error())
	}

	if err := container.DisconnectNetwork(ctx, id); err != nil {
		return fail(StageRun, err.Error())
	}
	if res, err := container.Exec(ctx, id, nil, []string{"mkdir", "-p", "/tmp/debfactory-home"}); err != nil || res.ExitCode != 0 {
		return fail(StageRun, "no se pudo crear HOME")
	}
	res, err = container.Exec(ctx, id, testEnv, strings.Fields(d.TestCommand))
	if reason, bad := failed(res, err); bad {
		return fail(StageRun, d.TestCommand+": "+reason)
	}
	output := string(res.Stdout) + string(res.Stderr)
	if !strings.Contains(output, d.Expect) {
		return fail(StageVersion, fmt.Sprintf("la salida de %q no contiene %q: %s", d.TestCommand, d.Expect,
			strings.Join(container.TailLines([]byte(output), 3), " | ")))
	}

	res, err = container.Exec(ctx, id, apt, []string{"apt-get", "purge", "-y", d.Package})
	if reason, bad := failed(res, err); bad {
		return fail(StagePurge, reason)
	}
	left, err := existing(ctx, id, files)
	if err != nil {
		return fail(StageLeftovers, err.Error())
	}
	if len(left) > 0 {
		return fail(StageLeftovers, "quedan archivos tras purgar: "+strings.Join(left, ", "))
	}
	r.Seconds = time.Since(start).Seconds()
	return r
}

// failed resume una orden fallida con las últimas líneas de su salida.
func failed(res container.Result, err error) (string, bool) {
	if err != nil {
		return err.Error(), true
	}
	if res.ExitCode != 0 {
		out := append(append([]byte{}, res.Stdout...), res.Stderr...)
		return fmt.Sprintf("código %d: %s", res.ExitCode, strings.Join(container.TailLines(out, 5), " | ")), true
	}
	return "", false
}

// Las dos órdenes siguientes son de debfactory, no del manifiesto: reciben
// las rutas como argumentos posicionales, nunca dentro del script.
const (
	nonDirScript   = `for p; do [ -d "$p" ] || printf '%s\n' "$p"; done`
	existingScript = `for p; do if [ -e "$p" ] || [ -L "$p" ]; then printf '%s\n' "$p"; fi; done`
)

// nonDirectories filtra de la lista de dpkg -L los directorios (los
// comparten otros paquetes y pueden quedar tras purgar).
func nonDirectories(ctx context.Context, id string, paths []string) ([]string, error) {
	return script(ctx, id, nonDirScript, paths)
}

// existing devuelve las rutas que siguen existiendo.
func existing(ctx context.Context, id string, paths []string) ([]string, error) {
	return script(ctx, id, existingScript, paths)
}

func script(ctx context.Context, id, body string, args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	res, err := container.Exec(ctx, id, nil, append([]string{"sh", "-c", body, "sh"}, args...))
	if reason, bad := failed(res, err); bad {
		return nil, fmt.Errorf("sh: %s", reason)
	}
	return strings.Fields(string(res.Stdout)), nil
}
