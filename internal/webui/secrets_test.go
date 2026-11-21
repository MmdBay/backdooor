package webui

import (
	"strings"
	"testing"
)

func TestForkSecretsAreLongAndUnguessable(t *testing.T) {
	pw := randomStrongSecret(passwordSecretLen)
	if len(pw) != passwordSecretLen {
		t.Fatalf("password length %d, want %d", len(pw), passwordSecretLen)
	}
	if strings.Trim(pw, "0123456789") == "" {
		t.Fatal("password is digits-only; fork default must be high-entropy")
	}
	path := randomPathSegment()
	if len(path) != pathSecretLen {
		t.Fatalf("base path length %d, want %d", len(path), pathSecretLen)
	}
	if !validBasePath(path) {
		t.Fatalf("generated path %q is not valid", path)
	}
}
