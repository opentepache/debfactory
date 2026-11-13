package restore

import "testing"

func TestManifestVersion(t *testing.T) {
	m := []byte("name: fnm\nversion: \"1.39.0\"\npav: 2\n")
	if v := manifestVersion(m); v != "1.39.0-0~pav2" {
		t.Errorf("v = %q", v)
	}
	if v := manifestVersion([]byte("name: x\n")); v != "" {
		t.Errorf("sin versión = %q", v)
	}
}
