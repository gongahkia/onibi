package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenRemovesRetiredTelegramState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onibi.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, stmt := range []string{
		`CREATE TABLE telegram_outbox (id TEXT PRIMARY KEY, fallback_text TEXT)`,
		`INSERT INTO telegram_outbox(id,fallback_text) VALUES('o1','terminal output')`,
		`CREATE TABLE telegram_updates (update_id INTEGER PRIMARY KEY)`,
		`CREATE TABLE telegram_cursor (id INTEGER PRIMARY KEY)`,
	} {
		if _, err := db.SQL().ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"telegram.owner_chat_id", "telegram.target.42", "daemon.health.v1"} {
		if err := db.KVSetString(ctx, key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE 'telegram_%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("retired Telegram tables remain: %d", tables)
	}
	keys, err := db.KVKeysWithPrefix(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "daemon.health.v1" {
		t.Fatalf("keys=%v", keys)
	}
}
