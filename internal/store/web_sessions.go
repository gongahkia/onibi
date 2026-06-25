package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type WebSession struct {
	SessionID   string
	DeviceLabel string
	CreatedAt   time.Time
	LastSeenAt  time.Time
	Revoked     bool
}

func (d *DB) ListWebSessions(ctx context.Context, includeRevoked bool) ([]WebSession, error) {
	where := "WHERE revoked = 0"
	if includeRevoked {
		where = ""
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT session_id, COALESCE(device_label, ''), created_at, last_seen_at, revoked
		   FROM web_sessions `+where+`
		  ORDER BY revoked ASC, last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebSession
	for rows.Next() {
		var s WebSession
		var created, last int64
		var revoked int
		if err := rows.Scan(&s.SessionID, &s.DeviceLabel, &created, &last, &revoked); err != nil {
			return nil, err
		}
		s.CreatedAt = time.Unix(created, 0)
		s.LastSeenAt = time.Unix(last, 0)
		s.Revoked = revoked != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) RevokeWebSession(ctx context.Context, sessionID string) (bool, error) {
	res, err := d.sql.ExecContext(ctx, `UPDATE web_sessions SET revoked = 1 WHERE session_id = ? AND revoked = 0`, sessionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (d *DB) WebSession(ctx context.Context, sessionID string) (WebSession, bool, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT session_id, COALESCE(device_label, ''), created_at, last_seen_at, revoked
		   FROM web_sessions WHERE session_id = ?`, sessionID)
	var s WebSession
	var created, last int64
	var revoked int
	err := row.Scan(&s.SessionID, &s.DeviceLabel, &created, &last, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return WebSession{}, false, nil
	}
	if err != nil {
		return WebSession{}, false, err
	}
	s.CreatedAt = time.Unix(created, 0)
	s.LastSeenAt = time.Unix(last, 0)
	s.Revoked = revoked != 0
	return s, true, nil
}
