package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

type SessionEntry struct {
	ID                string
	Name              string
	Agent             string
	CWD               string
	Command           string
	Transport         string
	TmuxTarget        string
	StartedAt         time.Time
	LastActivity      time.Time
	RecoveryState     fleet.SessionRecoveryState
	RecoveryReason    string
	RecoveryUpdatedAt time.Time
	EndedAt           time.Time
	Ended             bool
}

func (d *DB) SessionUpsertStart(ctx context.Context, id, name, agent, cwd, command, transport, tmuxTarget string, started time.Time) error {
	if transport == "" {
		transport = "pty"
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO sessions(id, name, agent, cwd, cmd, transport, tmux_target, started_at, last_activity, recovery_state, recovery_reason, recovery_updated_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   agent=excluded.agent,
		   cwd=excluded.cwd,
		   cmd=excluded.cmd,
		   transport=excluded.transport,
		   tmux_target=excluded.tmux_target,
		   started_at=excluded.started_at,
		   last_activity=excluded.last_activity
		 WHERE sessions.ended_at IS NULL`,
		id, name, agent, nullIfEmpty(cwd), nullIfEmpty(command), transport, nullIfEmpty(tmuxTarget), started.Unix(), started.Unix(), string(fleet.SessionRecoveryHealthy), "", started.Unix())
	return err
}

func (d *DB) SessionTouch(ctx context.Context, id string, at time.Time) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE sessions SET last_activity = ? WHERE id = ?`,
		at.Unix(), id)
	return err
}

func (d *DB) SessionMarkEnded(ctx context.Context, id string, ended time.Time) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE sessions
		    SET ended_at = COALESCE(ended_at, ?),
		        recovery_state = CASE WHEN ended_at IS NULL THEN ? ELSE recovery_state END,
		        recovery_reason = CASE WHEN ended_at IS NULL THEN ? ELSE recovery_reason END,
		        recovery_updated_at = CASE WHEN ended_at IS NULL THEN ? ELSE recovery_updated_at END
		  WHERE id = ?`,
		ended.Unix(), string(fleet.SessionRecoveryTerminated), "session terminated", ended.Unix(), id)
	return err
}

func (d *DB) SessionTransitionRecovery(ctx context.Context, id string, next fleet.SessionRecoveryState, reason string, at time.Time) (bool, error) {
	if strings.TrimSpace(id) == "" || !next.Valid() || at.IsZero() {
		return false, errors.New("invalid session recovery transition")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 512 || (next != fleet.SessionRecoveryHealthy && reason == "") || (next == fleet.SessionRecoveryHealthy && reason != "") {
		return false, errors.New("invalid session recovery reason")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	var ended sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT recovery_state, ended_at FROM sessions WHERE id = ?`, id).Scan(&current, &ended); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	state := fleet.SessionRecoveryState(current)
	if !state.Valid() {
		return false, errors.New("invalid persisted session recovery state")
	}
	if ended.Valid {
		return false, nil
	}
	if state == next {
		return false, nil
	}
	if !validSessionRecoveryTransition(state, next) {
		return false, errors.New("invalid session recovery transition")
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE sessions SET recovery_state = ?, recovery_reason = ?, recovery_updated_at = ? WHERE id = ? AND recovery_state = ? AND ended_at IS NULL`,
		string(next), reason, at.UTC().Unix(), id, string(state))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *DB) SessionsActive(ctx context.Context) ([]SessionEntry, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, agent, COALESCE(cwd, ''), COALESCE(cmd, ''), transport, COALESCE(tmux_target, ''),
		        started_at, COALESCE(last_activity, started_at), COALESCE(recovery_state, 'healthy'), COALESCE(recovery_reason, ''), COALESCE(recovery_updated_at, 0), ended_at
		   FROM sessions
		  WHERE ended_at IS NULL
		  ORDER BY started_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

func (d *DB) Session(ctx context.Context, id string) (SessionEntry, bool, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, agent, COALESCE(cwd, ''), COALESCE(cmd, ''), transport, COALESCE(tmux_target, ''),
		        started_at, COALESCE(last_activity, started_at), COALESCE(recovery_state, 'healthy'), COALESCE(recovery_reason, ''), COALESCE(recovery_updated_at, 0), ended_at
		   FROM sessions
		  WHERE id = ?`, id)
	if err != nil {
		return SessionEntry{}, false, err
	}
	defer rows.Close()
	entries, err := scanSessions(rows)
	if err != nil {
		return SessionEntry{}, false, err
	}
	if len(entries) == 0 {
		return SessionEntry{}, false, nil
	}
	return entries[0], true, nil
}

func (d *DB) SessionRename(ctx context.Context, id, name string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE sessions SET name = ? WHERE id = ?`, name, id)
	return err
}

func (d *DB) SessionsRecent(ctx context.Context, n int, includeEnded bool) ([]SessionEntry, error) {
	if n <= 0 {
		n = 50
	}
	where := "WHERE ended_at IS NULL"
	if includeEnded {
		where = ""
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, agent, COALESCE(cwd, ''), COALESCE(cmd, ''), transport, COALESCE(tmux_target, ''),
		        started_at, COALESCE(last_activity, started_at), COALESCE(recovery_state, 'healthy'), COALESCE(recovery_reason, ''), COALESCE(recovery_updated_at, 0), ended_at
		   FROM sessions `+where+`
		  ORDER BY started_at DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanSessions(rows *sql.Rows) ([]SessionEntry, error) {
	var out []SessionEntry
	for rows.Next() {
		var e SessionEntry
		var started, lastActivity, recoveryUpdated int64
		var ended sql.NullInt64
		var recoveryState string
		if err := rows.Scan(&e.ID, &e.Name, &e.Agent, &e.CWD, &e.Command, &e.Transport, &e.TmuxTarget, &started, &lastActivity, &recoveryState, &e.RecoveryReason, &recoveryUpdated, &ended); err != nil {
			return nil, err
		}
		e.StartedAt = time.Unix(started, 0)
		e.LastActivity = time.Unix(lastActivity, 0)
		e.RecoveryState = fleet.SessionRecoveryState(recoveryState)
		if recoveryUpdated > 0 {
			e.RecoveryUpdatedAt = time.Unix(recoveryUpdated, 0)
		}
		if !validSessionRecoveryRecord(e.RecoveryState, e.RecoveryReason, e.RecoveryUpdatedAt) {
			return nil, errors.New("invalid persisted session recovery state")
		}
		if ended.Valid {
			e.Ended = true
			e.EndedAt = time.Unix(ended.Int64, 0)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func validSessionRecoveryTransition(current, next fleet.SessionRecoveryState) bool {
	switch current {
	case fleet.SessionRecoveryHealthy:
		return next == fleet.SessionRecoveryReconnecting || next == fleet.SessionRecoveryRecovering || next == fleet.SessionRecoveryOrphaned || next == fleet.SessionRecoveryFailed || next == fleet.SessionRecoveryTerminated
	case fleet.SessionRecoveryReconnecting:
		return next == fleet.SessionRecoveryHealthy || next == fleet.SessionRecoveryRecovering || next == fleet.SessionRecoveryOrphaned || next == fleet.SessionRecoveryFailed || next == fleet.SessionRecoveryTerminated
	case fleet.SessionRecoveryRecovering:
		return next == fleet.SessionRecoveryHealthy || next == fleet.SessionRecoveryReconnecting || next == fleet.SessionRecoveryOrphaned || next == fleet.SessionRecoveryFailed || next == fleet.SessionRecoveryTerminated
	case fleet.SessionRecoveryOrphaned, fleet.SessionRecoveryFailed:
		return next == fleet.SessionRecoveryRecovering || next == fleet.SessionRecoveryTerminated
	default:
		return false
	}
}

func validSessionRecoveryRecord(state fleet.SessionRecoveryState, reason string, updatedAt time.Time) bool {
	if !state.Valid() || len(reason) > 512 {
		return false
	}
	if state == fleet.SessionRecoveryHealthy {
		return strings.TrimSpace(reason) == ""
	}
	return strings.TrimSpace(reason) != "" && !updatedAt.IsZero()
}
