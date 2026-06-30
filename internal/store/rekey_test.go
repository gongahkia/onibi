package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRekeyKeepsEncryptedRowsReadableWithNewKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rekey.sqlite")
	oldKey := bytesAscending(1)
	newKey := bytesAscending(65)
	db, err := Open(path, WithStoreKey(oldKey))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.PutPairingToken(ctx, "pair-token", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(ctx, "cookie-value", "iPhone", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := db.Rekey(ctx, newKey); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	oldDB, err := Open(path, WithStoreKey(oldKey))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := oldDB.WebSession(ctx, "cookie-value"); err == nil {
		t.Fatal("old key still decrypts web session")
	}
	_ = oldDB.Close()

	newDB, err := Open(path, WithStoreKey(newKey))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newDB.Close() })
	if ok, err := newDB.ConsumePairingToken(ctx, "pair-token"); err != nil || !ok {
		t.Fatalf("consume after rekey ok=%v err=%v", ok, err)
	}
	session, ok, err := newDB.WebSession(ctx, "cookie-value")
	if err != nil || !ok {
		t.Fatalf("web session after rekey ok=%v err=%v", ok, err)
	}
	if session.DeviceLabel != "iPhone" {
		t.Fatalf("session = %#v", session)
	}
}

func bytesAscending(start byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}
