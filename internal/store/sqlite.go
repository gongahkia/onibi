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

type DB struct {
	sql  *sql.DB
	path string
}

func Open(path string) (*DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	raw, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(1)
	if err := raw.Ping(); err != nil {
		_ = raw.Close()
		return nil, err
	}
	db := &DB{sql: raw, path: path}
	if err := db.migrate(); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	return db, nil
}
func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) SQL() *sql.DB { return d.sql }
func (d *DB) Path() string { return d.path }

const schema = `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY);
CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY,value BLOB NOT NULL,expire INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS idx_kv_expire ON kv(expire);
CREATE TABLE IF NOT EXISTS approvals (
 id TEXT PRIMARY KEY,session_id TEXT NOT NULL,agent TEXT NOT NULL,tool TEXT NOT NULL,input_json TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',edited_json TEXT,reason TEXT,msg_id INTEGER,chat_id INTEGER,created_at INTEGER NOT NULL,decided_at INTEGER,decided_by INTEGER,expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_approvals_state ON approvals(state,expires_at);
CREATE INDEX IF NOT EXISTS idx_approvals_msg ON approvals(chat_id,msg_id);
CREATE TABLE IF NOT EXISTS audit (id INTEGER PRIMARY KEY AUTOINCREMENT,ts INTEGER NOT NULL,action TEXT NOT NULL,session_id TEXT,payload_hash TEXT,decided_by_chat INTEGER,detail TEXT);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit(ts);
CREATE TABLE IF NOT EXISTS sessions (
 id TEXT PRIMARY KEY,name TEXT NOT NULL,agent TEXT NOT NULL,cwd TEXT,cmd TEXT,transport TEXT NOT NULL DEFAULT 'tmux',tmux_target TEXT,started_at INTEGER NOT NULL,last_activity INTEGER,recovery_state TEXT NOT NULL DEFAULT 'healthy',recovery_reason TEXT NOT NULL DEFAULT '',recovery_updated_at INTEGER NOT NULL DEFAULT 0,ended_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sessions_name ON sessions(name);
`

func (d *DB) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := d.sql.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	for _, migration := range []string{"ALTER TABLE approvals ADD COLUMN reason TEXT", "ALTER TABLE approvals ADD COLUMN decided_by INTEGER", "ALTER TABLE sessions ADD COLUMN cmd TEXT", "ALTER TABLE sessions ADD COLUMN last_activity INTEGER", "ALTER TABLE sessions ADD COLUMN recovery_state TEXT NOT NULL DEFAULT 'healthy'", "ALTER TABLE sessions ADD COLUMN recovery_reason TEXT NOT NULL DEFAULT ''", "ALTER TABLE sessions ADD COLUMN recovery_updated_at INTEGER NOT NULL DEFAULT 0"} {
		_, _ = d.sql.ExecContext(ctx, migration)
	}
	_, err := d.sql.ExecContext(ctx, "INSERT OR IGNORE INTO schema_version(version) VALUES (1)")
	return err
}
func (d *DB) KVSet(ctx context.Context, key string, value []byte, expire int64) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("kv key required")
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO kv(key,value,expire) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,expire=excluded.expire`, key, value, expire)
	return err
}
func (d *DB) KVSetString(ctx context.Context, key, value string) error {
	return d.KVSet(ctx, key, []byte(value), 0)
}
func (d *DB) KVGet(ctx context.Context, key string) ([]byte, bool, error) {
	var raw []byte
	var expire int64
	err := d.sql.QueryRowContext(ctx, `SELECT value,expire FROM kv WHERE key=?`, key).Scan(&raw, &expire)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if expire > 0 && expire <= time.Now().Unix() {
		_ = d.KVDel(ctx, key)
		return nil, false, nil
	}
	return append([]byte(nil), raw...), true, nil
}
func (d *DB) KVGetString(ctx context.Context, key string) (string, bool, error) {
	raw, ok, err := d.KVGet(ctx, key)
	return string(raw), ok, err
}
func (d *DB) KVDel(ctx context.Context, key string) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM kv WHERE key=?`, key)
	return err
}
func (d *DB) KVPurgeExpired(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM kv WHERE expire>0 AND expire<=?`, time.Now().Unix())
	return err
}
func (d *DB) KVKeysWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT key FROM kv WHERE key LIKE ? ORDER BY key`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}
