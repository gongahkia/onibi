package envelope

import (
	"bytes"
	"testing"
)

func TestCodecRoundTripAndCommitment(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	encoded := EncodeKey(key)
	decoded, err := DecodeKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, key) {
		t.Fatal("decoded key mismatch")
	}
	if Commitment(key) == "" || Commitment(key) != Commitment(decoded) {
		t.Fatal("commitment unstable")
	}
	codec, err := NewCodec(key, "ws:pty")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := codec.Seal("binary", []byte("terminal bytes"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("terminal bytes")) {
		t.Fatalf("ciphertext leaked plaintext: %s", sealed)
	}
	typ, opened, err := codec.Open(sealed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if typ != "binary" || string(opened) != "terminal bytes" {
		t.Fatalf("opened %q %q", typ, opened)
	}
	other, _ := NewCodec(key, "ws:events")
	if _, _, err := other.Open(sealed, nil); err == nil {
		t.Fatal("opened with wrong HKDF info")
	}
}
