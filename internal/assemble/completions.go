package assemble

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"debfactory/internal/container"
	"debfactory/internal/manifest"
)

// completionPath son las ubicaciones fijas (contracts/config.md); %s es el
// nombre del paquete.
var completionPath = map[string]string{
	"bash": "/usr/share/bash-completion/completions/%s",
	"zsh":  "/usr/share/zsh/vendor-completions/_%s",
	"fish": "/usr/share/fish/vendor_completions.d/%s.fish",
}

// Generator ejecuta argv (sin shell) con el árbol del paquete montado en
// /stage y devuelve su salida estándar.
type Generator func(ctx context.Context, arch, stage string, argv []string) ([]byte, error)

// Completions resuelve completions.<shell>: file (ruta dentro del artefacto)
// o generate (orden que se ejecuta con gen sobre los archivos ya instalados).
// Se instalan con modo 0644.
func Completions(ctx context.Context, m *manifest.Manifest, arch, root, outDir string, installed []File, gen Generator) ([]File, error) {
	list := m.Completions.List()
	if len(list) == 0 {
		return nil, nil
	}
	vars, err := m.Vars(arch)
	if err != nil {
		return nil, err
	}
	stage := ""
	var out []File
	for _, sc := range list {
		field := "completions." + sc.Shell
		dst := fmt.Sprintf(completionPath[sc.Shell], m.Name)
		if sc.File != "" {
			rel, err := manifest.Render(sc.File, vars)
			if err != nil {
				return nil, assembleErr("%s.file: %v", field, err)
			}
			full, err := within(root, rel)
			if err != nil {
				return nil, assembleErr("%s.file: %v", field, err)
			}
			if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
				return nil, assembleErr("%s.file: no existe %q en el artefacto", field, rel)
			}
			out = append(out, File{Dst: dst, Src: full, Mode: 0o644})
			continue
		}
		if stage == "" {
			stage = filepath.Join(outDir, "stage")
			if err := prepareStage(installed, stage); err != nil {
				return nil, err
			}
		}
		data, err := gen(ctx, arch, stage, strings.Fields(sc.Generate))
		if err != nil {
			return nil, assembleErr("%s.generate: %v", field, err)
		}
		p := filepath.Join(outDir, sc.Shell)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
		out = append(out, File{Dst: dst, Src: p, Mode: 0o644})
	}
	return out, nil
}

// prepareStage copia los archivos instalados a stage/<dst> con su modo final.
func prepareStage(files []File, stage string) error {
	for _, f := range files {
		if err := copyFile(f.Src, filepath.Join(stage, filepath.FromSlash(f.Dst)), f.Mode); err != nil {
			return err
		}
	}
	return nil
}

// ContainerGenerator genera completions en un contenedor efímero de la
// arquitectura objetivo (arm64 por qemu), sin red, con la raíz en solo
// lectura y un /tmp en memoria como HOME (research D11 y §3 riesgo 3). La
// imagen es image + ca-certificates, porque algunas herramientas crean un
// cliente TLS al arrancar.
func ContainerGenerator(image string) Generator {
	dockerfile := "FROM " + image + "\n" +
		"RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*\n"
	return func(ctx context.Context, arch, stage string, argv []string) ([]byte, error) {
		tag := "debfactory-completions:" + arch
		if err := container.EnsureImage(ctx, tag, dockerfile, container.Platform(arch), 7*24*time.Hour); err != nil {
			return nil, err
		}
		res, err := container.Run(ctx, container.Opts{
			Image:    tag,
			Platform: container.Platform(arch),
			Network:  "none",
			ReadOnly: true,
			Tmpfs:    []string{"/tmp"},
			Env:      []string{"HOME=/tmp", "PATH=/stage/usr/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			Mounts:   []container.Mount{{Src: stage, Dst: "/stage", ReadOnly: true}},
			Cmd:      argv,
		})
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 || len(bytes.TrimSpace(res.Stdout)) == 0 {
			return nil, fmt.Errorf("código %d y %d bytes de salida: %s", res.ExitCode, len(res.Stdout),
				strings.Join(container.TailLines(res.Stderr, 5), " | "))
		}
		return res.Stdout, nil
	}
}
