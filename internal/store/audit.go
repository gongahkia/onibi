package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

// AuditEntry is the typed read shape; writes use AuditAppend.
type AuditEntry struct {
	ID            int64     `json:"id"`
	TS            time.Time `json:"ts"`
	Action        string    `json:"action"`
	SessionID     string    `json:"session_id"`
	PayloadHash   string    `json:"payload_hash"`
	DecidedByChat int64     `json:"decided_by_chat"`
	Detail        string    `json:"detail"`
}

// AuditAppend writes a single audit row. Never blocks user-facing flow;
// failures are returned to the caller which is expected to log+continue.
func (d *DB) AuditAppend(ctx context.Context, action, sessionID, payload string, decidedBy int64, detail string) error {
	hash := ""
	if payload != "" {
		sum := sha256.Sum256([]byte(payload))
		hash = hex.EncodeToString(sum[:])
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO audit(ts, action, session_id, payload_hash, decided_by_chat, detail)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().Unix(), action, sessionID, hash, decidedBy, detail)
	return err
}

// AuditRecent returns the most recent n entries (newest first).
func (d *DB) AuditRecent(ctx context.Context, n int) ([]AuditEntry, error) {
	if n <= 0 {
		n = 50
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, ts, action, COALESCE(session_id, ''), COALESCE(payload_hash, ''),
		        COALESCE(decided_by_chat, 0), COALESCE(detail, '')
		 FROM audit ORDER BY ts DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Action, &e.SessionID, &e.PayloadHash,
			&e.DecidedByChat, &e.Detail); err != nil {
			return nil, err
		}
		e.TS = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) AuditAll(ctx context.Context) ([]AuditEntry, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, ts, action, COALESCE(session_id, ''), COALESCE(payload_hash, ''),
		        COALESCE(decided_by_chat, 0), COALESCE(detail, '')
		 FROM audit ORDER BY ts ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Action, &e.SessionID, &e.PayloadHash,
			&e.DecidedByChat, &e.Detail); err != nil {
			return nil, err
		}
		e.TS = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AuditCount reports total rows; useful for `onibi doctor` summary.
func (d *DB) AuditCount(ctx context.Context) (int64, error) {
	var n int64
	err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit`).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}
