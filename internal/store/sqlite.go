package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/envelope"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB with our typed helpers. Constructed via Open.
type DB struct {
	sql      *sql.DB
	path     string
	cryptbox *CryptBox
}

type OpenOption func(*openOptions) error

type openOptions struct {
	cryptbox *CryptBox
}

func WithStoreKey(masterKey []byte) OpenOption {
	return func(opts *openOptions) error {
		box, err := NewCryptBox(masterKey)
		if err != nil {
			return err
		}
		opts.cryptbox = box
		return nil
	}
}

func WithCryptBox(box *CryptBox) OpenOption {
	return func(opts *openOptions) error {
		if box == nil {
			return ErrCryptBoxUnavailable
		}
		opts.cryptbox = box
		return nil
	}
}

func OpenEphemeral(path string) (*DB, error) {
	key, err := envelope.NewKey()
	if err != nil {
		return nil, err
	}
	return Open(path, WithStoreKey(key))
}

// Open opens (or creates) the SQLite database at path, applies migrations,
// and enforces 0600 perms on the file. Pure-Go driver (modernc.org/sqlite)
// so no cgo required.
func Open(path string, opts ...OpenOption) (*DB, error) {
	var cfg openOptions
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
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
	db := &DB{sql: d, path: path, cryptbox: cfg.cryptbox}
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

func (d *DB) CryptBox() *CryptBox { return d.cryptbox }

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
  token_hash TEXT PRIMARY KEY,
  token_enc  BLOB NOT NULL,
  role       TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer')),
  session_id TEXT NOT NULL DEFAULT '',
  max_uses   INTEGER NOT NULL DEFAULT 1 CHECK (max_uses > 0),
  use_count  INTEGER NOT NULL DEFAULT 0 CHECK (use_count >= 0),
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
  msg_id      INTEGER,                              -- legacy rendered-message id
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
  last_activity INTEGER,
  ended_at      INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sessions_name ON sessions(name);

-- paired web cockpit browser sessions
CREATE TABLE IF NOT EXISTS web_sessions (
  cookie_hash    TEXT PRIMARY KEY,
  cookie_enc     BLOB NOT NULL,
  user_agent_enc BLOB NOT NULL,
  key_verifier_enc BLOB,
  role             TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer')),
  share_session_id TEXT NOT NULL DEFAULT '',
  share_expires_at INTEGER NOT NULL DEFAULT 0,
  created_at       INTEGER NOT NULL,
  last_seen_at     INTEGER NOT NULL,
  revoked          INTEGER NOT NULL DEFAULT 0,
  revoked_reason   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_web_sessions_revoked ON web_sessions(revoked, last_seen_at);

-- prompt queue (durable, per-session FIFO)
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

-- encrypted session snapshots and parsed transcript turns
CREATE TABLE IF NOT EXISTS snapshots (
  id                TEXT PRIMARY KEY,
  session_id        TEXT NOT NULL,
  name              TEXT NOT NULL,
  created_at        INTEGER NOT NULL,
  ring_buffer_enc   BLOB NOT NULL,
  cwd_enc           BLOB NOT NULL,
  env_enc           BLOB NOT NULL,
  transcript_offset INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_snapshots_session_created ON snapshots(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_name ON snapshots(name);

CREATE TABLE IF NOT EXISTS transcript_turns (
  id              TEXT PRIMARY KEY,
  session_id      TEXT NOT NULL,
  turn_index      INTEGER NOT NULL,
  role            TEXT NOT NULL,
  content_enc     BLOB NOT NULL,
  tool_calls_enc  BLOB NOT NULL,
  ts              INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_transcript_turns_session_turn ON transcript_turns(session_id, turn_index);
CREATE INDEX IF NOT EXISTS idx_transcript_turns_ts ON transcript_turns(ts);

CREATE TABLE IF NOT EXISTS workspaces (
  name        TEXT PRIMARY KEY,
  path_enc    BLOB NOT NULL,
  ssh_key_ref TEXT,
  last_seen   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_workspaces_last_seen ON workspaces(last_seen);

CREATE TABLE IF NOT EXISTS profiles (
  name         TEXT PRIMARY KEY,
  data_enc     BLOB NOT NULL,
  last_used_at INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_profiles_last_used ON profiles(last_used_at DESC, name ASC);

CREATE TABLE IF NOT EXISTS push_subscriptions (
  endpoint_hash TEXT PRIMARY KEY,
  endpoint_enc  BLOB NOT NULL,
  p256dh_enc    BLOB NOT NULL,
  auth_enc      BLOB NOT NULL,
  created_at    INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_push_subscriptions_last_seen ON push_subscriptions(last_seen_at);
`

func (d *DB) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := d.sql.ExecContext(ctx, schemaV1); err != nil {
		return fmt.Errorf("apply schema v1: %w", err)
	}
	if err := d.migrateEncryptedTables(ctx); err != nil {
		return err
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
	if err := d.ensureColumn(ctx, "sessions", "last_activity", "INTEGER"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "web_sessions", "key_verifier_enc", "BLOB"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "pairing_tokens", "role", "TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer'))"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "web_sessions", "role", "TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer'))"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "web_sessions", "share_session_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "web_sessions", "share_expires_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "web_sessions", "revoked_reason", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "pairing_tokens", "session_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "pairing_tokens", "max_uses", "INTEGER NOT NULL DEFAULT 1 CHECK (max_uses > 0)"); err != nil {
		return err
	}
	if err := d.ensureColumn(ctx, "pairing_tokens", "use_count", "INTEGER NOT NULL DEFAULT 0 CHECK (use_count >= 0)"); err != nil {
		return err
	}
	_, err := d.sql.ExecContext(ctx, "INSERT OR IGNORE INTO schema_version(version) VALUES (1), (7), (8), (9), (10), (11), (12), (13)")
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

func (d *DB) migrateEncryptedTables(ctx context.Context) error {
	hasPlainToken, err := d.hasColumn(ctx, "pairing_tokens", "token")
	if err != nil {
		return err
	}
	if hasPlainToken {
		if err := d.migratePairingTokens(ctx); err != nil {
			return err
		}
	}
	hasPlainSessionID, err := d.hasColumn(ctx, "web_sessions", "session_id")
	if err != nil {
		return err
	}
	if hasPlainSessionID {
		if err := d.migrateWebSessions(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureColumn(ctx context.Context, table, column, decl string) error {
	ok, err := d.hasColumn(ctx, table, column)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	_, err = d.sql.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return nil
	}
	return err
}

func (d *DB) hasColumn(ctx context.Context, table, column string) (bool, error) {
	rows, err := d.sql.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (d *DB) migratePairingTokens(ctx context.Context) error {
	rows, err := d.sql.QueryContext(ctx, `SELECT token, created_at, expires_at, consumed FROM pairing_tokens`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		token            string
		created, expires int64
		consumed         int
	}
	var rowsToCopy []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.token, &r.created, &r.expires, &r.consumed); err != nil {
			return err
		}
		rowsToCopy = append(rowsToCopy, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE pairing_tokens_new (
  token_hash TEXT PRIMARY KEY,
  token_enc  BLOB NOT NULL,
  role       TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer')),
  session_id TEXT NOT NULL DEFAULT '',
  max_uses   INTEGER NOT NULL DEFAULT 1 CHECK (max_uses > 0),
  use_count  INTEGER NOT NULL DEFAULT 0 CHECK (use_count >= 0),
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  consumed   INTEGER NOT NULL DEFAULT 0
)`); err != nil {
		return err
	}
	for _, r := range rowsToCopy {
		hash := lookupHash(r.token)
		sealed, err := d.sealString(ctx, "pairing_tokens", hash, "token_enc", r.token)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO pairing_tokens_new(token_hash, token_enc, role, session_id, max_uses, use_count, created_at, expires_at, consumed)
			 VALUES (?, ?, 'owner', '', 1, ?, ?, ?, ?)`,
			hash, sealed, r.consumed, r.created, r.expires, r.consumed); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_pairing_expires`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE pairing_tokens`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE pairing_tokens_new RENAME TO pairing_tokens`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_pairing_expires ON pairing_tokens(expires_at)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) migrateWebSessions(ctx context.Context) error {
	rows, err := d.sql.QueryContext(ctx, `SELECT session_id, COALESCE(device_label, ''), created_at, last_seen_at, revoked FROM web_sessions`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		sessionID, deviceLabel string
		created, lastSeen      int64
		revoked                int
	}
	var rowsToCopy []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.sessionID, &r.deviceLabel, &r.created, &r.lastSeen, &r.revoked); err != nil {
			return err
		}
		rowsToCopy = append(rowsToCopy, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE web_sessions_new (
  cookie_hash    TEXT PRIMARY KEY,
  cookie_enc     BLOB NOT NULL,
  user_agent_enc BLOB NOT NULL,
  key_verifier_enc BLOB,
  role             TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'viewer')),
  share_session_id TEXT NOT NULL DEFAULT '',
  share_expires_at INTEGER NOT NULL DEFAULT 0,
  created_at       INTEGER NOT NULL,
  last_seen_at     INTEGER NOT NULL,
  revoked          INTEGER NOT NULL DEFAULT 0,
  revoked_reason   TEXT NOT NULL DEFAULT ''
)`); err != nil {
		return err
	}
	for _, r := range rowsToCopy {
		hash := lookupHash(r.sessionID)
		sessionEnc, err := d.sealString(ctx, "web_sessions", hash, "cookie_enc", r.sessionID)
		if err != nil {
			return err
		}
		labelEnc, err := d.sealString(ctx, "web_sessions", hash, "user_agent_enc", r.deviceLabel)
		if err != nil {
			return err
		}
		reason := ""
		if r.revoked != 0 {
			reason = WebSessionReasonRevoked
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO web_sessions_new(cookie_hash, cookie_enc, user_agent_enc, key_verifier_enc, role, share_session_id, share_expires_at, created_at, last_seen_at, revoked, revoked_reason)
			 VALUES (?, ?, ?, NULL, 'owner', '', 0, ?, ?, ?, ?)`,
			hash, sessionEnc, labelEnc, r.created, r.lastSeen, r.revoked, reason); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_web_sessions_revoked`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE web_sessions`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE web_sessions_new RENAME TO web_sessions`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_web_sessions_revoked ON web_sessions(revoked, last_seen_at)`); err != nil {
		return err
	}
	return tx.Commit()
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

// KVSetEncryptedString stores an encrypted value in kv with no expiry.
func (d *DB) KVSetEncryptedString(ctx context.Context, key, value string) error {
	sealed, err := d.sealString(ctx, "kv", key, "value", value)
	if err != nil {
		return err
	}
	return d.KVSet(ctx, key, sealed, 0)
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

// KVGetEncryptedString retrieves an encrypted string from kv.
func (d *DB) KVGetEncryptedString(ctx context.Context, key string) (string, bool, error) {
	sealed, ok, err := d.KVGet(ctx, key)
	if err != nil || !ok {
		return "", ok, err
	}
	opened, err := d.openString(ctx, "kv", key, "value", sealed)
	if err != nil {
		return "", true, err
	}
	return opened, true, nil
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

const (
	PairRoleOwner  = "owner"
	PairRoleViewer = "viewer"
)

type PairingTokenClaim struct {
	Role      string
	SessionID string
	ExpiresAt time.Time
}

// PutPairingToken stores a freshly minted token with TTL ttl.
func (d *DB) PutPairingToken(ctx context.Context, token string, ttl time.Duration) error {
	return d.PutPairingTokenWithRole(ctx, token, ttl, PairRoleOwner, "", 1)
}

func (d *DB) PutViewerPairingToken(ctx context.Context, token, sessionID string, ttl time.Duration, maxUses int) error {
	if sessionID == "" {
		return errors.New("viewer pair token requires session id")
	}
	return d.PutPairingTokenWithRole(ctx, token, ttl, PairRoleViewer, sessionID, maxUses)
}

func (d *DB) PutPairingTokenWithRole(ctx context.Context, token string, ttl time.Duration, role, sessionID string, maxUses int) error {
	if !validPairRole(role) {
		return fmt.Errorf("invalid pair role: %s", role)
	}
	if maxUses <= 0 {
		return errors.New("pair max uses must be positive")
	}
	if role == PairRoleOwner {
		sessionID = ""
		maxUses = 1
	}
	now := time.Now().Unix()
	hash := lookupHash(token)
	sealed, err := d.sealString(ctx, "pairing_tokens", hash, "token_enc", token)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO pairing_tokens(token_hash, token_enc, role, session_id, max_uses, use_count, created_at, expires_at, consumed)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?, 0)`,
		hash, sealed, role, sessionID, maxUses, now, now+int64(ttl.Seconds()))
	return err
}

// ConsumePairingToken atomically claims a token. Owner tokens are single-use;
// viewer tokens are reusable until max_uses.
func (d *DB) ConsumePairingToken(ctx context.Context, token string) (bool, error) {
	_, ok, err := d.ClaimPairingToken(ctx, token)
	return ok, err
}

func (d *DB) ClaimPairingToken(ctx context.Context, token string) (PairingTokenClaim, bool, error) {
	hash := lookupHash(token)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return PairingTokenClaim{}, false, err
	}
	defer tx.Rollback()
	var claim PairingTokenClaim
	var consumed, expiresAt, maxUses, useCount int64
	err = tx.QueryRowContext(ctx,
		`SELECT role, session_id, consumed, expires_at, max_uses, use_count
		   FROM pairing_tokens WHERE token_hash = ?`,
		hash).Scan(&claim.Role, &claim.SessionID, &consumed, &expiresAt, &maxUses, &useCount)
	if errors.Is(err, sql.ErrNoRows) {
		return PairingTokenClaim{}, false, nil
	}
	if err != nil {
		return PairingTokenClaim{}, false, err
	}
	if !validPairRole(claim.Role) {
		return PairingTokenClaim{}, false, fmt.Errorf("invalid pair role: %s", claim.Role)
	}
	now := time.Now().Unix()
	if expiresAt <= now || useCount >= maxUses || (claim.Role == PairRoleOwner && consumed != 0) {
		return PairingTokenClaim{}, false, nil
	}
	claim.ExpiresAt = time.Unix(expiresAt, 0)
	var res sql.Result
	if claim.Role == PairRoleOwner {
		res, err = tx.ExecContext(ctx,
			`UPDATE pairing_tokens SET consumed = 1, use_count = use_count + 1
			 WHERE token_hash = ? AND role = 'owner' AND consumed = 0 AND expires_at > ? AND use_count < max_uses`,
			hash, now)
	} else {
		res, err = tx.ExecContext(ctx,
			`UPDATE pairing_tokens SET use_count = use_count + 1
			 WHERE token_hash = ? AND role = 'viewer' AND expires_at > ? AND use_count < max_uses`,
			hash, now)
	}
	if err != nil {
		return PairingTokenClaim{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return PairingTokenClaim{}, false, err
	}
	if n != 1 {
		return PairingTokenClaim{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return PairingTokenClaim{}, false, err
	}
	return claim, true, nil
}

func validPairRole(role string) bool {
	return role == PairRoleOwner || role == PairRoleViewer
}

// PurgeExpiredPairings deletes expired token rows. Run periodically; mostly
// cosmetic since ConsumePairingToken already gates on expires_at.
func (d *DB) PurgeExpiredPairings(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM pairing_tokens WHERE expires_at <= ?`,
		time.Now().Unix())
	return err
}

// ----------------------------------------------------------------------------
// Web sessions
// ----------------------------------------------------------------------------

// PutWebSession records an owner browser session.
func (d *DB) PutWebSession(ctx context.Context, sessionID, deviceLabel string, now time.Time) error {
	return d.PutWebSessionWithRole(ctx, sessionID, deviceLabel, PairRoleOwner, now)
}

func (d *DB) PutWebSessionWithRole(ctx context.Context, sessionID, deviceLabel, role string, now time.Time) error {
	return d.putWebSession(ctx, sessionID, deviceLabel, role, "", time.Time{}, now)
}

func (d *DB) PutViewerWebSession(ctx context.Context, sessionID, deviceLabel, shareSessionID string, shareExpiresAt, now time.Time) error {
	if strings.TrimSpace(shareSessionID) == "" {
		return errors.New("viewer web session requires share session id")
	}
	if shareExpiresAt.IsZero() {
		return errors.New("viewer web session requires share expiry")
	}
	return d.putWebSession(ctx, sessionID, deviceLabel, PairRoleViewer, shareSessionID, shareExpiresAt, now)
}

func (d *DB) putWebSession(ctx context.Context, sessionID, deviceLabel, role, shareSessionID string, shareExpiresAt, now time.Time) error {
	if !validPairRole(role) {
		return fmt.Errorf("invalid web session role: %s", role)
	}
	if role == PairRoleOwner {
		shareSessionID = ""
		shareExpiresAt = time.Time{}
	}
	ts := now.Unix()
	shareExpires := int64(0)
	if !shareExpiresAt.IsZero() {
		shareExpires = shareExpiresAt.Unix()
	}
	hash := lookupHash(sessionID)
	sessionEnc, err := d.sealString(ctx, "web_sessions", hash, "cookie_enc", sessionID)
	if err != nil {
		return err
	}
	labelEnc, err := d.sealString(ctx, "web_sessions", hash, "user_agent_enc", deviceLabel)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO web_sessions(cookie_hash, cookie_enc, user_agent_enc, role, share_session_id, share_expires_at, created_at, last_seen_at, revoked, revoked_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, '')
		 ON CONFLICT(cookie_hash) DO UPDATE SET
		   cookie_enc=excluded.cookie_enc,
		   user_agent_enc=excluded.user_agent_enc,
		   role=excluded.role,
		   share_session_id=excluded.share_session_id,
		   share_expires_at=excluded.share_expires_at,
		   last_seen_at=excluded.last_seen_at,
		   revoked=0,
		   revoked_reason=''`,
		hash, sessionEnc, labelEnc, role, shareSessionID, shareExpires, ts, ts)
	return err
}

func (d *DB) SetWebSessionKeyVerifier(ctx context.Context, sessionID string, verifier []byte) (bool, error) {
	hash := lookupHash(sessionID)
	sealed, err := d.sealBytes(ctx, "web_sessions", hash, "key_verifier_enc", verifier)
	if err != nil {
		return false, err
	}
	res, err := d.sql.ExecContext(ctx,
		`UPDATE web_sessions SET key_verifier_enc = ? WHERE cookie_hash = ? AND revoked = 0`,
		sealed, hash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (d *DB) WebSessionKeyVerifier(ctx context.Context, sessionID string) ([]byte, bool, error) {
	hash := lookupHash(sessionID)
	row := d.sql.QueryRowContext(ctx,
		`SELECT key_verifier_enc FROM web_sessions WHERE cookie_hash = ? AND revoked = 0`,
		hash)
	var sealed []byte
	err := row.Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(sealed) == 0 {
		return nil, false, nil
	}
	verifier, err := d.openBytes(ctx, "web_sessions", hash, "key_verifier_enc", sealed)
	if err != nil {
		return nil, false, err
	}
	return verifier, true, nil
}

// TouchWebSession updates last_seen_at iff the session is not revoked.
func (d *DB) TouchWebSession(ctx context.Context, sessionID string, now time.Time) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE web_sessions SET last_seen_at = ?
		 WHERE cookie_hash = ? AND revoked = 0 AND (share_expires_at = 0 OR share_expires_at > ?)`,
		now.Unix(), lookupHash(sessionID), now.Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// WebSessionValid reports whether sessionID exists and is not revoked.
func (d *DB) WebSessionValid(ctx context.Context, sessionID string) (bool, error) {
	status, err := d.WebSessionStatus(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return status.Valid, nil
}

func (d *DB) WebSessionStatus(ctx context.Context, sessionID string) (WebSessionStatus, error) {
	var revoked int
	var shareExpiresAt int64
	var reason string
	err := d.sql.QueryRowContext(ctx,
		`SELECT revoked, share_expires_at, revoked_reason FROM web_sessions WHERE cookie_hash = ?`,
		lookupHash(sessionID)).Scan(&revoked, &shareExpiresAt, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return WebSessionStatus{Reason: WebSessionReasonMissing}, nil
	}
	if err != nil {
		return WebSessionStatus{}, err
	}
	if revoked != 0 {
		if reason == "" {
			reason = WebSessionReasonRevoked
		}
		return WebSessionStatus{Reason: reason}, nil
	}
	if shareExpiresAt != 0 && shareExpiresAt <= time.Now().Unix() {
		return WebSessionStatus{Reason: WebSessionReasonExpired}, nil
	}
	return WebSessionStatus{Valid: true}, nil
}

type PushSubscription struct {
	Endpoint   string
	P256dh     string
	Auth       string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

func (d *DB) PutPushSubscription(ctx context.Context, endpoint, p256dh, auth string, now time.Time) error {
	endpoint = strings.TrimSpace(endpoint)
	p256dh = strings.TrimSpace(p256dh)
	auth = strings.TrimSpace(auth)
	if endpoint == "" || p256dh == "" || auth == "" {
		return errors.New("push subscription endpoint and keys required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	hash := lookupHash(endpoint)
	endpointEnc, err := d.sealString(ctx, "push_subscriptions", hash, "endpoint_enc", endpoint)
	if err != nil {
		return err
	}
	p256dhEnc, err := d.sealString(ctx, "push_subscriptions", hash, "p256dh_enc", p256dh)
	if err != nil {
		return err
	}
	authEnc, err := d.sealString(ctx, "push_subscriptions", hash, "auth_enc", auth)
	if err != nil {
		return err
	}
	ts := now.Unix()
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO push_subscriptions(endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, created_at, last_seen_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(endpoint_hash) DO UPDATE SET
		   endpoint_enc=excluded.endpoint_enc,
		   p256dh_enc=excluded.p256dh_enc,
		   auth_enc=excluded.auth_enc,
		   last_seen_at=excluded.last_seen_at`,
		hash, endpointEnc, p256dhEnc, authEnc, ts, ts)
	return err
}

func (d *DB) PushSubscriptions(ctx context.Context) ([]PushSubscription, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, created_at, last_seen_at
		   FROM push_subscriptions
		  ORDER BY last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var hash string
		var endpointEnc, p256dhEnc, authEnc []byte
		var createdAt, lastSeenAt int64
		if err := rows.Scan(&hash, &endpointEnc, &p256dhEnc, &authEnc, &createdAt, &lastSeenAt); err != nil {
			return nil, err
		}
		endpoint, err := d.openString(ctx, "push_subscriptions", hash, "endpoint_enc", endpointEnc)
		if err != nil {
			return nil, err
		}
		p256dh, err := d.openString(ctx, "push_subscriptions", hash, "p256dh_enc", p256dhEnc)
		if err != nil {
			return nil, err
		}
		auth, err := d.openString(ctx, "push_subscriptions", hash, "auth_enc", authEnc)
		if err != nil {
			return nil, err
		}
		out = append(out, PushSubscription{
			Endpoint:   endpoint,
			P256dh:     p256dh,
			Auth:       auth,
			CreatedAt:  time.Unix(createdAt, 0),
			LastSeenAt: time.Unix(lastSeenAt, 0),
		})
	}
	return out, rows.Err()
}

func (d *DB) DeletePushSubscription(ctx context.Context, endpoint string) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint_hash = ?`, lookupHash(strings.TrimSpace(endpoint)))
	return err
}

func (d *DB) sealString(ctx context.Context, table, rowID, column, value string) ([]byte, error) {
	return d.sealBytes(ctx, table, rowID, column, []byte(value))
}

func (d *DB) sealBytes(ctx context.Context, table, rowID, column string, value []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	return d.cryptbox.Seal(ctx, value, RowAAD(table, rowID, column))
}

func (d *DB) openString(ctx context.Context, table, rowID, column string, sealed []byte) (string, error) {
	opened, err := d.openBytes(ctx, table, rowID, column, sealed)
	if err != nil {
		return "", err
	}
	return string(opened), nil
}

func (d *DB) openBytes(ctx context.Context, table, rowID, column string, sealed []byte) ([]byte, error) {
	if d.cryptbox == nil {
		return nil, ErrCryptBoxUnavailable
	}
	opened, err := d.cryptbox.Open(ctx, sealed, RowAAD(table, rowID, column))
	if err != nil {
		return nil, err
	}
	return opened, nil
}

func lookupHash(value string) string {
	sum := sha256.Sum256([]byte("onibi-store-lookup-v1\x00" + value))
	return hex.EncodeToString(sum[:])
}
