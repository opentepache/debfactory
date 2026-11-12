package fetch

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAndVerifyChecksums(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("B", 64)
	data := "# comentario\n" + a + "  buf-Linux-x86_64.tar.gz\n" + b + " *dir/buf-Linux-aarch64.tar.gz\n\n"
	sums, err := ParseChecksums([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if sums["buf-Linux-x86_64.tar.gz"] != a || sums["buf-Linux-aarch64.tar.gz"] != strings.ToLower(b) {
		t.Fatalf("sums = %v", sums)
	}
	url := "https://github.com/bufbuild/buf/releases/download/v1.75.0/"
	if err := VerifyChecksum(sums, url+"sha256.txt", url+"buf-Linux-x86_64.tar.gz", a); err != nil {
		t.Error(err)
	}
	err = VerifyChecksum(sums, url+"sha256.txt", url+"buf-Linux-x86_64.tar.gz", strings.Repeat("c", 64))
	var cm *ChecksumMismatch
	if !errors.As(err, &cm) || err.Error() != "sha256 no coincide con sha256.txt de upstream para buf-Linux-x86_64.tar.gz" {
		t.Errorf("error = %v", err)
	}
	if err := VerifyChecksum(sums, url+"sha256.txt", url+"buf-Windows.zip", a); err == nil {
		t.Error("un asset ausente debería fallar")
	}
	if _, err := ParseChecksums([]byte("no es un checksum\n")); err == nil {
		t.Error("formato inválido aceptado")
	}
}
