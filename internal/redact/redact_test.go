package redact

import (
	"reflect"
	"testing"
)

func TestLines(t *testing.T) {
	in := []byte("Loading packages...\n" +
		"ERROR: unable to publish: gpg: signing failed with passphrase hunter2\n" +
		"rclone: Failed to copy: secret_access_key = abc123 rejected\n" +
		"error: -----BEGIN PGP PRIVATE " + "KEY BLOCK-----\n" +
		"Error: token=xyz, access_key_id: AKIA0000\n" +
		"done\n")
	got := Lines(in, "hunter2")
	want := []string{
		"ERROR: unable to publish: gpg: signing failed with passphrase ***",
		"rclone: Failed to copy: secret_access_key = *** rejected",
		"Error: token=*** access_key_id: ***",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lines =\n%q\nwant\n%q", got, want)
	}
}

func TestString(t *testing.T) {
	if got := String("ok\npassphrase: s3cr3t\nfin", "nada"); got != "ok\npassphrase: ***\nfin" {
		t.Errorf("String = %q", got)
	}
}
