package repotest

import (
	"context"
	"fmt"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/container"
	"debfactory/internal/installtest"
)

// Client es un contenedor limpio de una suite y arquitectura en la red de
// pruebas, con salida a Internet (curl y dependencias vienen de los mirrors).
type Client struct {
	ID          string
	Suite, Arch string
}

// StartClient arranca debfactory-test/<suite>:<arch> (fase 001).
func StartClient(ctx context.Context, s config.Suite, arch string) (*Client, error) {
	if err := installtest.EnsureImage(ctx, s, arch); err != nil {
		return nil, err
	}
	if err := container.EnsureNetwork(ctx, Network); err != nil {
		return nil, err
	}
	id, err := container.Start(ctx, container.Opts{
		Image:    installtest.ImageTag(s.Name, arch),
		Platform: container.Platform(arch),
		Network:  Network,
	})
	if err != nil {
		return nil, err
	}
	return &Client{ID: id, Suite: s.Name, Arch: arch}, nil
}

// Close elimina el contenedor.
func (c *Client) Close() { container.Remove(c.ID) }

// Script ejecuta un bloque entero con sh -e como root (el único uso de
// shell: el texto sale de la guía del repositorio). Devuelve la salida
// combinada; un código distinto de 0 es un error.
func (c *Client) Script(ctx context.Context, script string) (string, error) {
	res, err := container.Exec(ctx, c.ID, []string{"DEBIAN_FRONTEND=noninteractive"},
		[]string{"sh", "-e", "-c", script})
	out := string(res.Stdout) + string(res.Stderr)
	if err != nil {
		return out, err
	}
	if res.ExitCode != 0 {
		return out, fmt.Errorf("%s/%s: código %d:\n%s", c.Suite, c.Arch, res.ExitCode,
			strings.Join(container.TailLines([]byte(out), 15), "\n"))
	}
	return out, nil
}

// Installed devuelve la versión instalada de cada paquete (vacía si no lo
// está).
func (c *Client) Installed(ctx context.Context, pkgs ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pkgs {
		res, err := container.Exec(ctx, c.ID, nil, []string{"dpkg-query", "-W", "-f", "${Status}|${Version}", p})
		if err != nil {
			return nil, err
		}
		status, version, _ := strings.Cut(string(res.Stdout), "|")
		if res.ExitCode == 0 && strings.HasSuffix(status, " installed") {
			out[p] = version
		} else {
			out[p] = ""
		}
	}
	return out, nil
}

// AptWarnings devuelve las líneas W:, E: o N: de una salida de apt que
// mencionan host.
func AptWarnings(out, host string) []string {
	var bad []string
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if (strings.HasPrefix(t, "W:") || strings.HasPrefix(t, "E:") || strings.HasPrefix(t, "N:")) &&
			strings.Contains(t, host) {
			bad = append(bad, t)
		}
	}
	return bad
}
