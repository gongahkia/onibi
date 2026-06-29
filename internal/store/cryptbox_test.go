package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/envelope"
)

func TestCryptBoxRoundTripWithRandomAAD(t *testing.T) {
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := NewCryptBox(key)
	if err != nil {
		t.Fatal(err)
	}
	aad := make([]byte, 64)
	if _, err := rand.Read(aad); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("secret row value")
	sealed, err := box.Seal(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Fatalf("sealed payload contains plaintext")
	}
	opened, err := box.Open(context.Background(), sealed, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("opened = %q want %q", opened, plaintext)
	}
}

func TestOpenWithStoreKeyInjectsCryptBox(t *testing.T) {
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(t.TempDir(), "crypt.sqlite"), WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if db.CryptBox() == nil {
		t.Fatal("CryptBox nil")
	}
	aad := RowAAD("web_sessions", "session-1", "device_label")
	sealed, err := db.CryptBox().Seal(context.Background(), []byte("iPhone"), aad)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := db.CryptBox().Open(context.Background(), sealed, aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != "iPhone" {
		t.Fatalf("opened = %q", opened)
	}
}
