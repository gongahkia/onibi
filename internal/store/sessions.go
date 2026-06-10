package store

import (
	"context"
	"database/sql"
	"time"
)

type SessionEntry struct {
	ID         string
	Name       string
	Agent      string
	CWD        string
	Transport  string
	TmuxTarget string
	StartedAt  time.Time
	EndedAt    time.Time
	Ended      bool
}

func (d *DB) SessionUpsertStart(ctx context.Context, id, name, agent, cwd, transport, tmuxTarget string, started time.Time) error {
	if transport == "" {
		transport = "pty"
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO sessions(id, name, agent, cwd, transport, tmux_target, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   agent=excluded.agent,
		   cwd=excluded.cwd,
		   transport=excluded.transport,
		   tmux_target=excluded.tmux_target,
		   started_at=excluded.started_at,
		   ended_at=NULL`,
		id, name, agent, nullIfEmpty(cwd), transport, nullIfEmpty(tmuxTarget), started.Unix())
	return err
}

func (d *DB) SessionMarkEnded(ctx context.Context, id string, ended time.Time) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE sessions SET ended_at = COALESCE(ended_at, ?) WHERE id = ?`,
		ended.Unix(), id)
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
		`SELECT id, name, agent, COALESCE(cwd, ''), transport, COALESCE(tmux_target, ''),
		        started_at, ended_at
		   FROM sessions `+where+`
		  ORDER BY started_at DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionEntry
	for rows.Next() {
		var e SessionEntry
		var started int64
		var ended sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Name, &e.Agent, &e.CWD, &e.Transport, &e.TmuxTarget, &started, &ended); err != nil {
			return nil, err
		}
		e.StartedAt = time.Unix(started, 0)
		if ended.Valid {
			e.Ended = true
			e.EndedAt = time.Unix(ended.Int64, 0)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
