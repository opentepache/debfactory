package repotest

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"debfactory/internal/config"
	"debfactory/internal/container"
)

// Network es la red de Docker de las pruebas; los clientes alcanzan el
// servidor como http://repo/.
const Network = "debfactory-repotest"

// ServerImage es la imagen con Caddy (research D7).
const ServerImage = "debfactory-repo-server"

// TestURL es la URL del servidor vista desde los clientes.
const TestURL = "http://repo"

// EnsureServerImage construye debfactory-repo-server desde la imagen de
// lintian con Caddy de Debian; se reconstruye cada 7 días.
func EnsureServerImage(ctx context.Context, cfg *config.Config) error {
	dockerfile := "FROM " + cfg.Lintian.Image + "\n" +
		"RUN apt-get update && apt-get install -y --no-install-recommends caddy && rm -rf /var/lib/apt/lists/*\n"
	return container.EnsureImage(ctx, ServerImage, dockerfile, "", 7*24*time.Hour)
}

// Server es un Caddy sirviendo un directorio.
type Server struct{ ID string }

// StartServer sirve dir de solo lectura en la red de pruebas con el nombre
// "repo". Monta el directorio padre y sirve /srv/<base>, así que si dir es
// el enlace current, Caddy sigue la generación vigente tras cada publicación.
func StartServer(ctx context.Context, cfg *config.Config, dir string, port int) (*Server, error) {
	if err := EnsureServerImage(ctx, cfg); err != nil {
		return nil, err
	}
	if err := container.EnsureNetwork(ctx, Network); err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(dir))
	if err != nil {
		return nil, err
	}
	id, err := container.Start(ctx, container.Opts{
		Image:   ServerImage,
		Network: Network,
		Alias:   "repo",
		Ports:   ports(port),
		Mounts:  []container.Mount{{Src: parent, Dst: "/srv", ReadOnly: true}},
		Cmd:     []string{"caddy", "file-server", "--root", "/srv/" + filepath.Base(dir), "--listen", ":80"},
	})
	if err != nil {
		return nil, err
	}
	return &Server{ID: id}, nil
}

// Stop elimina el contenedor del servidor.
func (s *Server) Stop() { container.Remove(s.ID) }

func ports(port int) []string {
	if port <= 0 {
		return nil
	}
	return []string{fmt.Sprintf("127.0.0.1:%d:80", port)}
}
