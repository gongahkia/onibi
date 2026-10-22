package store

import (
	"path/filepath"
	"testing"
)

func TestTelegramPairOwnerCommitsBindingAndConsumesCodeAtomically(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	if err := db.KVSetString(ctx, "telegram.pair_code", "123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `CREATE TRIGGER fail_owner BEFORE INSERT ON kv WHEN NEW.key='telegram.owner_user_id' BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.TelegramPairOwner(ctx, 42, 7, "123456"); err == nil {
		t.Fatal("pairing acknowledged a failed owner write")
	}
	if code, ok, err := db.KVGetString(ctx, "telegram.pair_code"); err != nil || !ok || code != "123456" {
		t.Fatalf("failed pairing consumed code: %q %t %v", code, ok, err)
	}
	if _, found, err := db.KVGetString(ctx, "telegram.owner_chat_id"); err != nil || found {
		t.Fatalf("failed pairing partially stored owner: found=%t err=%v", found, err)
	}
	if _, err := db.SQL().ExecContext(ctx, `DROP TRIGGER fail_owner`); err != nil {
		t.Fatal(err)
	}
	if err := db.TelegramPairOwner(ctx, 42, 7, "123456"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"telegram.owner_chat_id": "42", "telegram.owner_user_id": "7"} {
		if got, ok, err := db.KVGetString(ctx, key); err != nil || !ok || got != want {
			t.Fatalf("binding %s=%q, found=%t err=%v", key, got, ok, err)
		}
	}
	if _, found, err := db.KVGetString(ctx, "telegram.pair_code"); err != nil || found {
		t.Fatalf("successful pairing retained code: found=%t err=%v", found, err)
	}
	if err := db.TelegramPairOwner(ctx, 99, 99, "123456"); err == nil {
		t.Fatal("used pairing code could replace owner")
	}
}
