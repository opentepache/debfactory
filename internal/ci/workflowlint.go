package ci

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Reglas de los workflows de `packages` (specs/009, contracts/pipeline.md,
// constitución IX): los jobs en runners hospedados no reciben secretos ni
// escritura del repositorio; solo un job (publish, en la PC) usa runner propio
// y nunca en el workflow de PR; todas las acciones van fijadas por sha.

// hostedRunners son las etiquetas permitidas en runners hospedados: los
// estándar gratuitos, o la que elige `ci matrix` (que solo emite esas).
var hostedRunners = []string{"ubuntu-24.04", "ubuntu-24.04-arm", "${{ matrix.runner }}"}

// hostedPermissions son los permisos que un job hospedado puede declarar.
var hostedPermissions = map[string]string{
	"contents":      "read",
	"id-token":      "write", // atestación
	"attestations":  "write", // atestación
	"models":        "read",  // resumen con IA (jobs de PR sin secretos)
	"pull-requests": "write", // comentario del PR (job sin IA)
}

var pinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)

type wfJob struct {
	RunsOn      any              `yaml:"runs-on"`
	Permissions any              `yaml:"permissions"`
	Steps       []map[string]any `yaml:"steps"`
	Uses        string           `yaml:"uses"`
}

type workflow struct {
	Name string           `yaml:"name"`
	Jobs map[string]wfJob `yaml:"jobs"`
	raw  map[string]any
}

// LintWorkflow revisa un workflow y devuelve los problemas encontrados.
func LintWorkflow(name string, data []byte) []string {
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return []string{fmt.Sprintf("%s: YAML inválido: %v", name, err)}
	}
	var rawJobs struct {
		Jobs map[string]any `yaml:"jobs"`
	}
	yaml.Unmarshal(data, &rawJobs)
	var out []string
	add := func(job, format string, args ...any) {
		out = append(out, fmt.Sprintf("%s: job %s: %s", name, job, fmt.Sprintf(format, args...)))
	}
	jobs := make([]string, 0, len(wf.Jobs))
	for j := range wf.Jobs {
		jobs = append(jobs, j)
	}
	sort.Strings(jobs)
	var selfHosted []string
	for _, j := range jobs {
		job := wf.Jobs[j]
		labels := runsOn(job.RunsOn)
		if job.Uses != "" && !pinnedAction.MatchString(job.Uses) && !strings.HasPrefix(job.Uses, "./") {
			add(j, "workflow reutilizable sin fijar por sha: %s", job.Uses)
		}
		for _, st := range job.Steps {
			if u, _ := st["uses"].(string); u != "" && !pinnedAction.MatchString(u) && !strings.HasPrefix(u, "./") {
				add(j, "acción sin fijar por sha de 40 caracteres: %s", u)
			}
		}
		if slices.Contains(labels, "self-hosted") {
			selfHosted = append(selfHosted, j)
			if j != "publish" || !slices.Equal(labels, []string{"self-hosted", "debfactory-main"}) {
				add(j, "runner propio %v: solo el job publish en [self-hosted, debfactory-main]", labels)
			}
			continue
		}
		if len(labels) != 1 || !slices.Contains(hostedRunners, labels[0]) {
			add(j, "runner %v: solo runners estándar (%s)", labels, strings.Join(hostedRunners, ", "))
		}
		perms, ok := job.Permissions.(map[string]any)
		if !ok {
			add(j, "un job hospedado MUST declarar sus permisos (mapa, aunque sea {})")
		}
		for k, v := range perms {
			if want, ok := hostedPermissions[k]; !ok || fmt.Sprint(v) != want {
				add(j, "permiso %s: %v no está permitido en un runner hospedado", k, v)
			}
		}
		if b, err := yaml.Marshal(rawJobs.Jobs[j]); err == nil && strings.Contains(string(b), "secrets.") {
			add(j, "un job hospedado no puede usar secrets.*")
		}
	}
	if len(selfHosted) > 1 {
		out = append(out, fmt.Sprintf("%s: más de un job en runner propio: %v", name, selfHosted))
	}
	if wf.Name == "pr" && len(selfHosted) > 0 {
		out = append(out, fmt.Sprintf("%s: el workflow de PR no puede usar runners propios (%v)", name, selfHosted))
	}
	return out
}

// runsOn normaliza runs-on (cadena o lista) a una lista de etiquetas.
func runsOn(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	}
	return nil
}
