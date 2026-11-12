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
		pav      int
		err      string
	}{
		{"1.39.0", 1, ""},
		{"1.39.0", 2, ""},
		{"2.66.1", 12, ""},
		{"1.0.0", 10, ""}, // pav10 > pav9: dpkg compara los dígitos como número
	} {
		err := CheckVersions(c.upstream, c.pav)
		if (err == nil) != (c.err == "") || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s pav%d: %v", c.upstream, c.pav, err)
		}
	}
	if ok, _ := compare("1.0.0-0~pav1", "lt", "1.0.0-0"); !ok {
		t.Error("0~pav1 debe ser menor que la revisión 0 de Debian")
	}
}
