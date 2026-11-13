// Package container envuelve la orden docker. Solo usa info, build, image
// inspect, run, exec, network create/inspect/disconnect y rm (plan.md,
// constitución IX), así que vale tanto docker.io de Debian como docker-ce.
package container

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"debfactory/internal/report"
)

// Mount es un bind mount.
type Mount struct {
	Src, Dst string
	ReadOnly bool
}

// Opts describe un contenedor.
type Opts struct {
	Image    string
	Platform string   // p. ej. "linux/arm64"; vacío = la del host
	Network  string   // "none", "bridge", una red creada o vacío (la de docker)
	Alias    string   // --network-alias (nombre en la red)
	Name     string   // --name
	Ports    []string // --publish, p. ej. "127.0.0.1:8080:80"
	ReadOnly bool     // --read-only
	Tmpfs    []string // --tmpfs, p. ej. "/tmp"
	Mounts   []Mount
	Env      []string // K=V
	Workdir  string
	Cmd      []string
}

// Result es la salida de una orden dentro de un contenedor.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
}

// Platform devuelve la plataforma de docker de una arquitectura Debian.
func Platform(arch string) string { return "linux/" + arch }

func docker(ctx context.Context, stdin io.Reader, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr, cmd.Stdin = &out, &errb, stdin
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ctx.Err() == nil {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("docker %s: %w", args[0], ctx.Err())
	}
	return res, err
}

func check(res Result, err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s: %s", what, strings.Join(TailLines(res.Stderr, 15), "\n"))
	}
	return nil
}

// dockerfileLabel guarda en cada imagen el hash del Dockerfile que la creó,
// para reconstruirla si cambia (p. ej. otra imagen base en la config).
const dockerfileLabel = "debfactory.dockerfile"

func dockerfileHash(dockerfile string) string {
	h := sha256.Sum256([]byte(dockerfile))
	return hex.EncodeToString(h[:8])
}

// Build construye una imagen desde un Dockerfile sin contexto (por stdin),
// actualizando la imagen base (--pull) y sin la caché de capas (--no-cache):
// con caché, "RUN apt-get update" no se repetiría y la imagen conservaría la
// fecha de creación vieja, así que EnsureImage la reconstruiría en cada
// llamada sin refrescar nunca los índices.
func Build(ctx context.Context, tag, dockerfile, platform string) error {
	args := []string{"build", "--pull", "--no-cache", "--label", dockerfileLabel + "=" + dockerfileHash(dockerfile), "-t", tag}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, "-")
	res, err := docker(ctx, strings.NewReader(dockerfile), args...)
	return check(res, err, "docker build "+tag)
}

// EnsureImage construye la imagen si no existe, si tiene más de maxAge o si
// se creó con otro Dockerfile.
// Con tareas en paralelo, un candado por etiqueta evita construir la misma
// imagen dos veces a la vez.
func EnsureImage(ctx context.Context, tag, dockerfile, platform string, maxAge time.Duration) error {
	mu, _ := imageLocks.LoadOrStore(tag, new(sync.Mutex))
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	res, err := docker(ctx, nil, "image", "inspect", "--format",
		`{{.Created}} {{index .Config.Labels "`+dockerfileLabel+`"}}`, tag)
	if err != nil {
		return err
	}
	if res.ExitCode == 0 {
		created, label, _ := strings.Cut(strings.TrimSpace(string(res.Stdout)), " ")
		t, err := time.Parse(time.RFC3339Nano, created)
		if err == nil && label == dockerfileHash(dockerfile) && time.Since(t) < maxAge {
			return nil
		}
	}
	return Build(ctx, tag, dockerfile, platform)
}

var imageLocks sync.Map // etiqueta → *sync.Mutex

// ImageID devuelve el id (sha256:…) de una imagen local.
func ImageID(ctx context.Context, tag string) (string, error) {
	res, err := docker(ctx, nil, "image", "inspect", "--format", "{{.Id}}", tag)
	if err := check(res, err, "docker image inspect "+tag); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func runArgs(o Opts) ([]string, error) {
	var a []string
	if o.Platform != "" {
		a = append(a, "--platform", o.Platform)
	}
	if o.Network != "" {
		a = append(a, "--network", o.Network)
	}
	if o.Alias != "" {
		a = append(a, "--network-alias", o.Alias)
	}
	if o.Name != "" {
		a = append(a, "--name", o.Name)
	}
	for _, p := range o.Ports {
		a = append(a, "--publish", p)
	}
	if o.ReadOnly {
		a = append(a, "--read-only")
	}
	for _, t := range o.Tmpfs {
		a = append(a, "--tmpfs", t)
	}
	for _, m := range o.Mounts {
		if strings.ContainsAny(m.Src+m.Dst, ",=") {
			return nil, fmt.Errorf("ruta no admitida en un montaje: %q", m.Src)
		}
		v := "type=bind,source=" + m.Src + ",target=" + m.Dst
		if m.ReadOnly {
			v += ",readonly"
		}
		a = append(a, "--mount", v)
	}
	for _, e := range o.Env {
		a = append(a, "-e", e)
	}
	if o.Workdir != "" {
		a = append(a, "-w", o.Workdir)
	}
	return a, nil
}

// Run ejecuta Cmd en un contenedor efímero. Un código distinto de 0 de la
// orden va en Result.ExitCode; err solo indica un fallo de docker.
func Run(ctx context.Context, o Opts) (Result, error) {
	extra, err := runArgs(o)
	if err != nil {
		return Result{}, err
	}
	args := append([]string{"run", "--rm", "--pull", "never"}, extra...)
	args = append(append(args, o.Image), o.Cmd...)
	res, err := docker(ctx, nil, args...)
	if err == nil && res.ExitCode == 125 {
		err = fmt.Errorf("docker run %s: %s", o.Image, strings.Join(TailLines(res.Stderr, 15), "\n"))
	}
	return res, err
}

// Start arranca un contenedor desacoplado que ejecuta Cmd, o que espera
// (sleep infinity) si Cmd está vacío, y devuelve su id. Hay que llamar a
// Remove al terminar.
func Start(ctx context.Context, o Opts) (string, error) {
	extra, err := runArgs(o)
	if err != nil {
		return "", err
	}
	args := append([]string{"run", "-d", "--pull", "never"}, extra...)
	cmd := o.Cmd
	if len(cmd) == 0 {
		cmd = []string{"sleep", "infinity"}
	}
	args = append(append(args, o.Image), cmd...)
	res, err := docker(ctx, nil, args...)
	if err := check(res, err, "docker run "+o.Image); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// Exec ejecuta cmd (sin shell) en el contenedor.
func Exec(ctx context.Context, id string, env, cmd []string) (Result, error) {
	args := []string{"exec"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(append(args, id), cmd...)
	return docker(ctx, nil, args...)
}

// DisconnectNetwork deja el contenedor sin red.
func DisconnectNetwork(ctx context.Context, id string) error {
	res, err := docker(ctx, nil, "network", "disconnect", "bridge", id)
	return check(res, err, "docker network disconnect")
}

// EnsureNetwork crea una red bridge con ese nombre si no existe.
func EnsureNetwork(ctx context.Context, name string) error {
	if res, err := docker(ctx, nil, "network", "inspect", name); err == nil && res.ExitCode == 0 {
		return nil
	}
	res, err := docker(ctx, nil, "network", "create", name)
	return check(res, err, "docker network create "+name)
}

// Remove elimina el contenedor. Usa su propio contexto para limpiar aunque el
// del llamador se haya cancelado.
func Remove(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res, err := docker(ctx, nil, "rm", "-f", id)
	return check(res, err, "docker rm")
}

var binfmt = map[string]string{"amd64": "qemu-x86_64", "arm64": "qemu-aarch64"}

// Preflight comprueba que docker responde y que hay emulación binfmt para
// cada arquitectura distinta de la del host. Devuelve *report.EnvError.
func Preflight(ctx context.Context, arches []string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return &report.EnvError{Cause: "no se encuentra la orden docker; instala docker.io o docker-ce"}
	}
	res, err := docker(ctx, nil, "info", "--format", "{{.ServerVersion}}")
	if err != nil || res.ExitCode != 0 {
		return &report.EnvError{Cause: "docker no responde (¿está el usuario en el grupo docker?): " +
			strings.Join(TailLines(res.Stderr, 3), " ")}
	}
	for _, a := range arches {
		if a == runtime.GOARCH {
			continue
		}
		name, ok := binfmt[a]
		if !ok {
			return &report.EnvError{Cause: "no se conoce la emulación de " + a}
		}
		if _, err := os.Stat("/proc/sys/fs/binfmt_misc/" + name); err != nil {
			return &report.EnvError{Cause: fmt.Sprintf(
				"no hay binfmt para %s (/proc/sys/fs/binfmt_misc/%s); instala qemu-user-binfmt", a, name)}
		}
	}
	return nil
}

// TailLines devuelve las últimas n líneas no vacías de b.
func TailLines(b []byte, n int) []string {
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimRight(l, "\r \t"); strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
