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

func TestOpenRemovesRetiredAgentState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onibi.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, stmt := range []string{
		`CREATE TABLE approvals (id TEXT PRIMARY KEY,session_id TEXT NOT NULL,agent TEXT NOT NULL,tool TEXT NOT NULL,input_json TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',reason TEXT,created_at INTEGER NOT NULL,decided_at INTEGER,decided_by INTEGER,expires_at INTEGER NOT NULL)`,
		`CREATE INDEX idx_approvals_state ON approvals(state,expires_at)`,
		`INSERT INTO approvals(id,session_id,agent,tool,input_json,created_at,expires_at) VALUES('a1','s1','pi','bash','{"command":"secret"}',1,2)`,
		`CREATE TABLE claude_questions (id TEXT PRIMARY KEY,session_id TEXT NOT NULL,input_json TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',answers_json TEXT,reason TEXT,created_at INTEGER NOT NULL,decided_at INTEGER,decided_by INTEGER,expires_at INTEGER NOT NULL)`,
		`CREATE INDEX idx_claude_questions_state ON claude_questions(state,expires_at)`,
		`INSERT INTO claude_questions(id,session_id,input_json,answers_json,created_at,expires_at) VALUES('q1','s1','{}','{"answer":"secret"}',1,2)`,
	} {
		if _, err := db.SQL().ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"codex.thread.aaaa1111", "daemon.health.v1"} {
		if err := db.KVSetString(ctx, key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		db, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var leftover int
		if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name IN ('approvals','claude_questions','idx_approvals_state','idx_claude_questions_state')`).Scan(&leftover); err != nil {
			t.Fatal(err)
		}
		if leftover != 0 {
			t.Fatalf("retired agent tables or indexes remain: %d", leftover)
		}
		keys, err := db.KVKeysWithPrefix(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 1 || keys[0] != "daemon.health.v1" {
			t.Fatalf("keys=%v", keys)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
