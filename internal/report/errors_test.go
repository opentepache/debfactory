package report

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestErrorFormat(t *testing.T) {
	e := &Error{Package: "fnm", Version: "1.39.0", Stage: Inputs, Cause: "falta algo"}
	if got := e.Error(); got != "debfactory: fnm 1.39.0 -: inputs: falta algo" {
		t.Errorf("sin suite: %q", got)
	}
	e.Arch, e.Suite = "amd64", "trixie"
	if got := e.Error(); got != "debfactory: fnm 1.39.0 amd64 trixie: inputs: falta algo" {
		t.Errorf("con suite: %q", got)
	}
	b, err := json.Marshal(e)
	if err != nil || !strings.Contains(string(b), `"suite":"trixie"`) {
		t.Errorf("json = %s, %v", b, err)
	}
}
