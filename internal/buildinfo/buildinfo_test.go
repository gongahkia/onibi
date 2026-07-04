package buildinfo

import (
	"encoding/base64"
	"testing"
)

func TestReleasePublicKeyDecodesBase64(t *testing.T) {
	old := ReleasePublicKeyB64
	t.Cleanup(func() { ReleasePublicKeyB64 = old })
	ReleasePublicKeyB64 = base64.StdEncoding.EncodeToString([]byte("  key\n"))
	if got := ReleasePublicKey(); got != "key" {
		t.Fatalf("ReleasePublicKey() = %q", got)
	}
}

func TestReleasePublicKeyIgnoresInvalidBase64(t *testing.T) {
	old := ReleasePublicKeyB64
	t.Cleanup(func() { ReleasePublicKeyB64 = old })
	ReleasePublicKeyB64 = "%"
	if got := ReleasePublicKey(); got != "" {
		t.Fatalf("ReleasePublicKey() = %q", got)
	}
}
