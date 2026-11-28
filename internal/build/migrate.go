package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"debfactory/internal/lock"
	"debfactory/internal/manifest"
	"debfactory/internal/report"
)

// MigrateLayout pasa cada paquete de packages/<p>.yaml y packages/<p>.lock.yaml
// a la ubicación autocontenida packages/<p>/ (specs/009, research D21). En un
// repositorio git usa git mv para conservar la historia; los paquetes ya
// migrados no se tocan.
func MigrateLayout(ctx context.Context, root string, out *report.Printer) error {
	names, err := ListPackages(root)
	if err != nil {
		return err
	}
	inGit := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--is-inside-work-tree").Run() == nil
	moved := 0
	for _, n := range names {
		for _, pair := range [][2]string{{manifest.Path(n), manifest.DirPath(n)}, {lock.Path(n), lock.DirPath(n)}} {
			from, to := filepath.Join(root, pair[0]), filepath.Join(root, pair[1])
			if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
				continue
			}
			if _, err := os.Stat(to); err == nil {
				return fmt.Errorf("%s y %s existen a la vez", pair[0], pair[1])
			}
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			if inGit {
				cmd := exec.CommandContext(ctx, "git", "-C", root, "mv", pair[0], pair[1])
				if b, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("git mv %s: %v: %s", pair[0], err, b)
				}
			} else if err := os.Rename(from, to); err != nil {
				return err
			}
			moved++
		}
	}
	out.Line("migrate layout: %d archivos movidos a packages/<paquete>/", moved)
	return nil
}
