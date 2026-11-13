// Package redact filtra la salida de procesos externos (gpg, aptly, rclone)
// antes de mostrarla: solo deja las líneas de error y oculta cualquier
// secreto (constitución III; FR-020).
package redact

import (
	"regexp"
	"strings"
)

var (
	errorLine = regexp.MustCompile(`(?i)error|fail`)
	// clave seguida de separador y valor: passphrase=..., secret_access_key = ...
	keyValue = regexp.MustCompile(`(?i)(passphrase|secret_access_key|access_key_id|token)(["']?\s*[:=]\s*|\s+)("[^"]*"|'[^']*'|\S+)`)
)

// Lines devuelve las líneas de error de b, sin secretos. Cada literal de
// secrets se sustituye por ***; los valores tras passphrase,
// secret_access_key, access_key_id y token también. Una línea que contenga
// "PRIVATE KEY" nunca se devuelve.
func Lines(b []byte, secrets ...string) []string {
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" || !errorLine.MatchString(l) {
			continue
		}
		if s, ok := clean(l, secrets); ok {
			out = append(out, s)
		}
	}
	return out
}

// String limpia un texto completo (todas sus líneas, no solo las de error).
func String(s string, secrets ...string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if c, ok := clean(l, secrets); ok {
			out = append(out, c)
		}
	}
	return strings.Join(out, "\n")
}

func clean(l string, secrets []string) (string, bool) {
	if strings.Contains(strings.ToUpper(l), "PRIVATE KEY") {
		return "", false
	}
	for _, s := range secrets {
		if s = strings.TrimSpace(s); s != "" {
			l = strings.ReplaceAll(l, s, "***")
		}
	}
	return keyValue.ReplaceAllString(l, "${1}${2}***"), true
}
