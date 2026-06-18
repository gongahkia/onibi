package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB with our typed helpers. Constructed via Open.
type DB struct {
	sql  *sql.DB
	path string
}

// Open opens (or creates) the SQLite database at path, applies migrations,
// and enforces 0600 perms on the file. Pure-Go driver (modernc.org/sqlite)
// so no cgo required.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir for db: %w", err)
	}
	// pragmas: WAL for concurrent readers + writers; foreign_keys on for
	// when we add cross-table refs.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	d.SetMaxOpenConns(1) // sqlite single-writer; multiplexing readers handled by busy_timeout
	if err := d.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	db := &DB{sql: d, path: path}
	if err := db.migrate(); err != nil {
		return nil, err
	}
	// enforce file perms after create
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("chmod db: %w", err)
	}
	return db, nil
}

// Close releases the underlying *sql.DB.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the raw handle for advanced callers (audit log, sessions).
// Prefer the typed helpers in this package where possible.
func (d *DB) SQL() *sql.DB { return d.sql }

func (d *DB) Path() string { return d.path }

const schemaV1 = `
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY
);

-- tgterm-derived KV (see docs/tgterm-patterns.md §2)
CREATE TABLE IF NOT EXISTS kv (
  key    TEXT PRIMARY KEY,
  value  BLOB NOT NULL,
  expire INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_kv_expire ON kv(expire);

-- single-use deeplink pairing tokens; replaces tgterm
-- first-message-becomes-owner race
CREATE TABLE IF NOT EXISTS pairing_tokens (
  token      TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  consumed   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_pairing_expires ON pairing_tokens(expires_at);

-- blocking-approval state machine (phase 3)
CREATE TABLE IF NOT EXISTS approvals (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL,
  agent       TEXT NOT NULL,
  tool        TEXT NOT NULL,
  input_json  TEXT NOT NULL,
  state       TEXT NOT NULL DEFAULT 'pending',     -- pending|approved|denied|edited|expired|cancelled
  edited_json TEXT,
  reason      TEXT,
  msg_id      INTEGER,                              -- telegram message id for callback editing
  chat_id     INTEGER,
  created_at  INTEGER NOT NULL,
  decided_at  INTEGER,
  decided_by  INTEGER,
  expires_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_approvals_state ON approvals(state, expires_at);
CREATE INDEX IF NOT EXISTS idx_approvals_msg ON approvals(chat_id, msg_id);

-- audit log of every decision and injection
CREATE TABLE IF NOT EXISTS audit (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  ts              INTEGER NOT NULL,
  action          TEXT NOT NULL,
  session_id      TEXT,
  payload_hash    TEXT,
  decided_by_chat INTEGER,
  detail          TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit(ts);

-- installed hook script registry for tamper detection
CREATE TABLE IF NOT EXISTS hooks (
  agent       TEXT NOT NULL,
  path        TEXT NOT NULL,
  sha256      TEXT NOT NULL,
  version     TEXT,
  installed_at INTEGER NOT NULL,
  PRIMARY KEY (agent, path)
);

CREATE TABLE IF NOT EXISTS hook_backups (
  agent         TEXT NOT NULL,
  path          TEXT NOT NULL,
  source_sha256 TEXT NOT NULL,
  backup_path   TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  PRIMARY KEY (agent, path, source_sha256)
);

-- session registry (phase 6 — multi-session)
CREATE TABLE IF NOT EXISTS sessions (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  agent         TEXT NOT NULL,
  cwd           TEXT,
  cmd           TEXT,
  transport     TEXT NOT NULL DEFAULT 'pty',  -- pty|tmux
  tmux_target   TEXT,
  started_at    INTEGER NOT NULL,
  ended_at      INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sessions_name ON sessions(name);

-- Telegram-originated prompt queue (durable, per-session FIFO)
CREATE TABLE IF NOT EXISTS prompt_queue (
  id          TEXT PRIMARY KEY,
  session_id  TEXT NOT NULL,
  chat_id     INTEGER NOT NULL,
  text        TEXT NOT NULL,
  state       TEXT NOT NULL DEFAULT 'queued', -- queued|sent|cancelled|failed
  position    INTEGER NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  sent_at     INTEGER
);
CREATE INDEX IF NOT EXISTS idx_prompt_queue_session_state_pos ON prompt_queue(session_id, state, position);
CREATE INDEX IF NOT EXISTS idx_prompt_queue_state ON prompt_queue(state, created_at);
`

func (d *DB) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := d.sql.ExecContext(ctx, schemaV1); err != nil {
		return fmt.Errorf("apply schema v1: %w", err)
	}
	if err := d.ensureColumn(ctx, "sessions", "cmd", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "hooks", "version", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "approvals", "reason", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "approvals", "decided_by", "INTEGER"); err != nil {
		return err
	}
	_, err := d.sql.ExecContext(ctx, "INSERT OR IGNORE INTO schema_version(version) VALUES (1)")
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

func (d *DB) ensureColumn(ctx context.Context, table, column, decl string) error {
	rows, err := d.sql.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return nil
	}
	return err
}

// ----------------------------------------------------------------------------
// KV
// ----------------------------------------------------------------------------

// KVSet upserts a key. expire is unix-seconds (0 = never).
func (d *DB) KVSet(ctx context.Context, key string, value []byte, expire int64) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO kv(key, value, expire) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, expire=excluded.expire`,
		key, value, expire)
	return err
}

// KVSetString is a convenience wrapper.
func (d *DB) KVSetString(ctx context.Context, key, value string) error {
	return d.KVSet(ctx, key, []byte(value), 0)
}

// KVGet returns the value and a found bool. Honors expire (returns
// found=false if expired and best-effort deletes the row).
func (d *DB) KVGet(ctx context.Context, key string) ([]byte, bool, error) {
	var v []byte
	var expire int64
	err := d.sql.QueryRowContext(ctx, `SELECT value, expire FROM kv WHERE key = ?`, key).Scan(&v, &expire)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if expire > 0 && expire < time.Now().Unix() {
		_, _ = d.sql.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, key)
		return nil, false, nil
	}
	return v, true, nil
}

// KVGetString is a convenience wrapper.
func (d *DB) KVGetString(ctx context.Context, key string) (string, bool, error) {
	v, ok, err := d.KVGet(ctx, key)
	if err != nil || !ok {
		return "", ok, err
	}
	return string(v), true, nil
}

// KVDel deletes a key (no-op if missing).
func (d *DB) KVDel(ctx context.Context, key string) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM kv WHERE key = ?`, key)
	return err
}

// KVPurgeExpired deletes rows whose expire is set and elapsed.
func (d *DB) KVPurgeExpired(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM kv WHERE expire > 0 AND expire < ?`, time.Now().Unix())
	return err
}

// KVKeysWithPrefix returns keys that start with prefix.
func (d *DB) KVKeysWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT key FROM kv WHERE key LIKE ?`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// ----------------------------------------------------------------------------
// Pairing tokens
// ----------------------------------------------------------------------------

// PutPairingToken stores a freshly minted token with TTL ttl.
func (d *DB) PutPairingToken(ctx context.Context, token string, ttl time.Duration) error {
	now := time.Now().Unix()
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO pairing_tokens(token, created_at, expires_at, consumed) VALUES (?, ?, ?, 0)`,
		token, now, now+int64(ttl.Seconds()))
	return err
}

// ConsumePairingToken atomically marks the token consumed iff it is
// unexpired and not previously consumed. Returns true only on the single
// winning consume call.
func (d *DB) ConsumePairingToken(ctx context.Context, token string) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE pairing_tokens SET consumed = 1
		 WHERE token = ? AND consumed = 0 AND expires_at > ?`,
		token, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// PurgeExpiredPairings deletes expired-and-unconsumed token rows. Run
// periodically; mostly cosmetic since ConsumePairingToken already gates on
// expires_at.
func (d *DB) PurgeExpiredPairings(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM pairing_tokens WHERE expires_at <= ?`,
		time.Now().Unix())
	return err
}
