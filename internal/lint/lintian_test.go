package lint

import (
	"strings"
	"testing"
)

// TestDockerfilePatch: la imagen corrige la revisión dep5 y falla al
// construirse si la línea de lintian cambió (specs/009).
func TestDockerfilePatch(t *testing.T) {
	d := dockerfile("debian:trixie")
	for _, want := range []string{"grep -q 'my $parser = XML::LibXML->new;'",
		"XML::LibXML->new(expand_entities => 0, load_ext_dtd => 0);", dep5File} {
		if !strings.Contains(d, want) {
			t.Errorf("falta %q en\n%s", want, d)
		}
	}
}
