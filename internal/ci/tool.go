package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"debfactory/internal/version"
)

// ToolFile es el archivo del repositorio de paquetes que fija la versión de
// la herramienta (specs/004-split-repos, contracts/herramienta.md).
const ToolFile = "debfactory.version"

var commitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ReadTool lee la versión fijada en root/debfactory.version.
func ReadTool(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ToolFile))
	if err != nil {
		return "", &InputError{ToolFile + " falta o no es un commit (40 hex)"}
	}
	pin := strings.TrimSuffix(string(data), "\n")
	if !commitRe.MatchString(pin) {
		return "", &InputError{ToolFile + " falta o no es un commit (40 hex)"}
	}
	return pin, nil
}

// CheckTool es la autocomprobación de la herramienta (research D4): dentro
// de un job de GitHub Actions, el binario MUST ser exactamente la versión que
// fija la raíz del checkout. Lee siempre root, nunca un worktree de otro
// commit (rebuild usa la herramienta que fija hoy main). Fuera de CI no
// comprueba nada: el mantenedor ejecuta versiones "dev".
func CheckTool(root string, env *Env) error {
	if env == nil || !env.Actions {
		return nil
	}
	pin, err := ReadTool(root)
	if err != nil {
		return err
	}
	if version.Commit != pin {
		return &InputError{fmt.Sprintf("tool: soy %s y packages fija %s", version.Commit, pin)}
	}
	return nil
}
