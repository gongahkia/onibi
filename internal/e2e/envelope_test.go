package e2e

import (
	"encoding/hex"
	"testing"
)

func TestDeriveSessionKeyMatchesWebCryptoVector(t *testing.T) {
	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i)
	}
	got := DeriveSessionKey(master, []byte("session-123"))
	want := "ddb4b228646608a53f4bef9fc74be6f6a1b8ee7bdae530b9a436c2fd5c9f76ce"
	if hex.EncodeToString(got) != want {
		t.Fatalf("session key = %x, want %s", got, want)
	}
}
