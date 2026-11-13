package installtest

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCheckVersions(t *testing.T) {
	if _, err := exec.LookPath("dpkg"); err != nil {
		t.Skip("sin dpkg")
	}
	for _, c := range []struct {
		upstream string
		revision int
		err      string
	}{
		{"1.39.0", 1, ""},
		{"1.39.0", 2, ""},
		{"2.66.1", 12, ""},
		{"1.0.0", 10, ""}, // ~tepache10 > ~tepache9: dpkg compara los dígitos como número
	} {
		err := CheckVersions(c.upstream, c.revision)
		if (err == nil) != (c.err == "") || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s revision %d: %v", c.upstream, c.revision, err)
		}
	}
	if ok, _ := CompareVersions("1.0.0-0~tepache1", "lt", "1.0.0-0"); !ok {
		t.Error("0~tepache1 debe ser menor que la revisión 0 de Debian")
	}
}

// TestSuffixRename comprueba el orden tras la enmienda 1.6.0 de la
// constitución (~pav → ~tepache): las versiones de prueba con el sufijo
// viejo quedan por debajo de las nuevas, y la oficial de Debian gana a ambas.
// Es la única aparición intencionada de "~pav" en el repositorio.
func TestSuffixRename(t *testing.T) {
	if _, err := exec.LookPath("dpkg"); err != nil {
		t.Skip("sin dpkg")
	}
	chain := []string{"1.40.0-0~pav1", "1.40.0-0~tepache1", "1.40.0-1"}
	for i := 0; i+1 < len(chain); i++ {
		ok, err := CompareVersions(chain[i], "lt", chain[i+1])
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("se esperaba %s < %s", chain[i], chain[i+1])
		}
	}
}
