package sync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"debfactory/internal/config"
	"debfactory/internal/redact"
	"debfactory/internal/report"
)

// Hook, si no es nil, se llama tras cada fase; solo lo usan los tests para
// simular una interrupción (T047).
var Hook func(phase string) error

// runBatch sube (o borra, en la fase delete) un lote con rclone. La salida
// se filtra con redact: nunca se muestran credenciales.
func runBatch(ctx context.Context, destName string, dest config.Destination, srcDir, phase string, b Batch) error {
	list, err := os.CreateTemp("", "debfactory-sync-*")
	if err != nil {
		return err
	}
	defer os.Remove(list.Name())
	list.WriteString(strings.Join(b.Files, "\n") + "\n")
	list.Close()
	var args []string
	if phase == PhaseDelete {
		args = []string{"delete", "--files-from", list.Name(), dest.Remote}
	} else {
		args = []string{"copy", "--files-from", list.Name(), "--skip-links", "--checksum", "--no-traverse"}
		if b.CacheControl != "" {
			args = append(args, "--header-upload", "Cache-Control: "+b.CacheControl)
		}
		args = append(args, srcDir, dest.Remote)
	}
	cmd := exec.CommandContext(ctx, "rclone", args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		lines := redact.Lines(out.Bytes())
		if len(lines) == 0 {
			lines = []string{err.Error()}
		}
		return report.Newf(report.Sync, "%s: rclone: %s", destName, strings.Join(lines, "; "))
	}
	return nil
}

// checkRclone devuelve un error de entorno si falta rclone (código 3).
func checkRclone() error {
	if _, err := exec.LookPath("rclone"); err != nil {
		return &report.EnvError{Cause: fmt.Sprintf("falta rclone en el PATH (sudo apt install rclone)")}
	}
	return nil
}
