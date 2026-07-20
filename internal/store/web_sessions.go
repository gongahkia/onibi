package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type WebSession struct {
	SessionID     string
	DeviceLabel   string
	CreatedAt     time.Time
	LastSeenAt    time.Time
	Revoked       bool
	RevokedReason string
}

const (
	WebSessionReasonMissing    = "session-missing"
	WebSessionReasonRevoked    = "session-revoked"
	WebSessionReasonStoreRekey = "store-rekey"
)

type WebSessionStatus struct {
	Valid  bool
	Reason string
}

func (d *DB) legacyOwnerClause() string {
	if d.webSessionsHaveRole {
		return " AND role = 'owner'"
	}
	return ""
}

func (d *DB) ListWebSessions(ctx context.Context, includeRevoked bool) ([]WebSession, error) {
	where := "WHERE 1 = 1"
	if !includeRevoked {
		where += " AND revoked = 0"
	}
	where += d.legacyOwnerClause()
	rows, err := d.sql.QueryContext(ctx,
		`SELECT cookie_hash, cookie_enc, user_agent_enc, created_at, last_seen_at, revoked, revoked_reason
		   FROM web_sessions `+where+`
		  ORDER BY revoked ASC, last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WebSession{}
	for rows.Next() {
		var s WebSession
		var hash string
		var sessionEnc, labelEnc []byte
		var created, last int64
		var revoked int
		if err := rows.Scan(&hash, &sessionEnc, &labelEnc, &created, &last, &revoked, &s.RevokedReason); err != nil {
			return nil, err
		}
		sessionID, err := d.openString(ctx, "web_sessions", hash, "cookie_enc", sessionEnc)
		if err != nil {
			return nil, err
		}
		label, err := d.openString(ctx, "web_sessions", hash, "user_agent_enc", labelEnc)
		if err != nil {
			return nil, err
		}
		s.SessionID = sessionID
		s.DeviceLabel = label
		s.CreatedAt = time.Unix(created, 0)
		s.LastSeenAt = time.Unix(last, 0)
		s.Revoked = revoked != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) RevokeWebSession(ctx context.Context, sessionID string) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE web_sessions SET revoked = 1, revoked_reason = ? WHERE cookie_hash = ? AND revoked = 0`+d.legacyOwnerClause(),
		WebSessionReasonRevoked, lookupHash(sessionID))
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
	hash := lookupHash(sessionID)
	row := d.sql.QueryRowContext(ctx,
		`SELECT cookie_enc, user_agent_enc, created_at, last_seen_at, revoked, revoked_reason
		   FROM web_sessions WHERE cookie_hash = ?`+d.legacyOwnerClause(), hash)
	var s WebSession
	var sessionEnc, labelEnc []byte
	var created, last int64
	var revoked int
	err := row.Scan(&sessionEnc, &labelEnc, &created, &last, &revoked, &s.RevokedReason)
	if errors.Is(err, sql.ErrNoRows) {
		return WebSession{}, false, nil
	}
	if err != nil {
		return WebSession{}, false, err
	}
	openedSessionID, err := d.openString(ctx, "web_sessions", hash, "cookie_enc", sessionEnc)
	if err != nil {
		return WebSession{}, false, err
	}
	label, err := d.openString(ctx, "web_sessions", hash, "user_agent_enc", labelEnc)
	if err != nil {
		return WebSession{}, false, err
	}
	s.SessionID = openedSessionID
	s.DeviceLabel = label
	s.CreatedAt = time.Unix(created, 0)
	s.LastSeenAt = time.Unix(last, 0)
	s.Revoked = revoked != 0
	return s, true, nil
}
