package ci

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"debfactory/internal/ci/citest"
	"debfactory/internal/forge"
	"debfactory/internal/report"
)

func newNotifier(t *testing.T) (*GitHubNotifier, *citest.FakeGitHub, *bytes.Buffer) {
	t.Helper()
	gh := citest.NewFakeGitHub(t)
	c, err := forge.New(gh.URL, citest.Repo, secretToken)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	return &GitHubNotifier{GH: c, Label: "pipeline-failure", RunURL: "https://github.com/owner/debfactory/actions/runs/7",
		Commit: strings.Repeat("a", 40), Out: &report.Printer{Out: &out, Err: &out}, Token: secretToken}, gh, &out
}

func TestNotifyLifecycle(t *testing.T) {
	ctx := context.Background()
	n, gh, _ := newNotifier(t)
	e := &report.Error{Package: "fnm", Version: "1.40.0-0~tepache1", Arch: "arm64", Suite: "noble", Stage: report.Test,
		Cause: "salió con código 127: fnm: not found; token " + secretToken}
	n.Fail(ctx, e)
	is := gh.OpenIssues()
	if len(is) != 1 {
		t.Fatalf("issues = %d", len(is))
	}
	want := "<!-- debfactory:failure package=fnm version=1.40.0-0~tepache1 stage=test -->\n" +
		"**Paquete:** fnm · **Versión:** 1.40.0-0~tepache1 · **Etapa:** test\n\n" +
		"**Motivo en noble/arm64:** salió con código 127: fnm: not found; token ***\n\n" +
		"**Log:** https://github.com/owner/debfactory/actions/runs/7 · **Commit:** aaaaaaa\n"
	if is[0].Title != "fallo: fnm 1.40.0-0~tepache1 en test" || is[0].Body != want || is[0].Labels[0] != "pipeline-failure" {
		t.Errorf("issue =\n%q\n%q", is[0].Title, is[0].Body)
	}
	// Repetido: comentario (FR-021), también desde otro notificador (otra ejecución).
	n2, _, _ := newNotifier(t)
	n2.GH = n.GH
	n2.Fail(ctx, e)
	if is := gh.OpenIssues(); len(is) != 1 || len(is[0].Comments) != 1 {
		t.Fatalf("deduplicación: %+v", is)
	}
	// Otra etapa del mismo paquete: otro issue.
	n.Fail(ctx, &report.Error{Package: "fnm", Version: "1.40.0-0~tepache1", Stage: report.Lintian, Cause: "E: x"})
	// Un fallo global.
	n.Fail(ctx, &report.Error{Stage: report.State, Cause: "PR cerrado"})
	if is := gh.OpenIssues(); len(is) != 3 || is[2].Title != "fallo: pipeline - en state" {
		t.Fatalf("issues = %+v", is)
	}
	// Publicar fnm 1.40.0 cierra sus dos issues; el de state sigue.
	n3, _, _ := newNotifier(t)
	n3.GH = n.GH
	n3.Published(ctx, "fnm", "1.40.0-0~tepache1", 2)
	if is := gh.OpenIssues(); len(is) != 1 || is[0].Title != "fallo: pipeline - en state" {
		t.Fatalf("tras publicar: %+v", is)
	}
	if c := gh.Issues[1].Comments; !strings.Contains(c[len(c)-1], "resuelto por https://github.com/owner/debfactory/actions/runs/7: publicado en serial 2") {
		t.Errorf("comentario de cierre: %q", c)
	}
	n3.StagePassed(ctx, report.State)
	if is := gh.OpenIssues(); len(is) != 0 {
		t.Fatalf("tras pasar state: %+v", is)
	}
	if strings.Contains(gh.AllBodies(), secretToken) {
		t.Error("el token llegó a GitHub")
	}
}

func TestNotifyIgnoresOthers(t *testing.T) {
	ctx := context.Background()
	n, gh, _ := newNotifier(t)
	// Un issue con el marcador pero sin la etiqueta, y uno con la etiqueta sin marcador.
	gh.Issues[50] = &citest.Issue{Number: 50, Title: "x", State: "open",
		Body: "<!-- debfactory:failure package=fnm version=1 stage=test -->"}
	gh.Issues[51] = &citest.Issue{Number: 51, Title: "y", State: "open", Labels: []string{"pipeline-failure"}, Body: "otro"}
	n.Fail(ctx, &report.Error{Package: "fnm", Version: "1", Stage: report.Test, Cause: "c"})
	if len(gh.OpenIssues()) != 3 {
		t.Errorf("issues = %d; el de otra etiqueta no cuenta", len(gh.OpenIssues()))
	}
	n.Published(ctx, "fnm", "1", 1)
	if len(gh.OpenIssues()) != 2 {
		t.Errorf("solo debía cerrarse el propio")
	}
}

func TestNotifyPagination(t *testing.T) {
	ctx := context.Background()
	n, gh, _ := newNotifier(t)
	for i := 1; i <= 150; i++ {
		gh.Issues[i] = &citest.Issue{Number: i, State: "open", Labels: []string{"pipeline-failure"}, Body: "otro"}
	}
	gh.Issues[150].Body = "<!-- debfactory:failure package=bob version=2 stage=lintian -->"
	n.Fail(ctx, &report.Error{Package: "bob", Version: "2", Stage: report.Lintian, Cause: "c"})
	if len(gh.Issues[150].Comments) != 1 || len(gh.Issues) != 150 {
		t.Errorf("no encontró el issue de la página 2")
	}
}

func TestNotifyAPIDown(t *testing.T) {
	n, gh, out := newNotifier(t)
	gh.Fail = true
	n.Fail(context.Background(), &report.Error{Package: "fnm", Version: "1", Stage: report.Test, Cause: "c"})
	if !strings.Contains(out.String(), "notify: no se pudieron leer los issues") {
		t.Errorf("salida: %s", out)
	}
}
