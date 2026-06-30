package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type SnapshotEntry struct {
	ID               string
	SessionID        string
	Name             string
	CreatedAt        time.Time
	RingBuffer       []byte
	CWD              string
	Env              []string
	TranscriptOffset int64
}

func (d *DB) SnapshotSave(ctx context.Context, s SnapshotEntry) error {
	if s.ID == "" {
		return errors.New("snapshot id required")
	}
	if s.SessionID == "" {
		return errors.New("snapshot session id required")
	}
	if s.Name == "" {
		return errors.New("snapshot name required")
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	ring, err := d.sealBytes(ctx, "snapshots", s.ID, "ring_buffer_enc", s.RingBuffer)
	if err != nil {
		return err
	}
	cwd, err := d.sealString(ctx, "snapshots", s.ID, "cwd_enc", s.CWD)
	if err != nil {
		return err
	}
	envPlain, err := json.Marshal(s.Env)
	if err != nil {
		return err
	}
	env, err := d.sealBytes(ctx, "snapshots", s.ID, "env_enc", envPlain)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO snapshots(id, session_id, name, created_at, ring_buffer_enc, cwd_enc, env_enc, transcript_offset)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.SessionID, s.Name, s.CreatedAt.Unix(), ring, cwd, env, s.TranscriptOffset)
	return err
}

func (d *DB) SnapshotByName(ctx context.Context, name string) (SnapshotEntry, bool, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, session_id, name, created_at, ring_buffer_enc, cwd_enc, env_enc, transcript_offset
		   FROM snapshots WHERE name = ? ORDER BY created_at DESC LIMIT 1`, name)
	return d.scanSnapshot(ctx, row)
}

func (d *DB) SnapshotDeleteByName(ctx context.Context, name string) (bool, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM snapshots WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (d *DB) SnapshotsList(ctx context.Context) ([]SnapshotEntry, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, session_id, name, created_at, ring_buffer_enc, cwd_enc, env_enc, transcript_offset
		   FROM snapshots ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SnapshotEntry
	for rows.Next() {
		s, err := d.scanSnapshotRows(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) SessionByID(ctx context.Context, id string) (SessionEntry, bool, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, agent, COALESCE(cwd, ''), COALESCE(cmd, ''), transport, COALESCE(tmux_target, ''),
		        started_at, COALESCE(last_activity, started_at), ended_at
		   FROM sessions WHERE id = ? LIMIT 1`, id)
	if err != nil {
		return SessionEntry{}, false, err
	}
	defer rows.Close()
	sessions, err := scanSessions(rows)
	if err != nil {
		return SessionEntry{}, false, err
	}
	if len(sessions) == 0 {
		return SessionEntry{}, false, nil
	}
	return sessions[0], true, nil
}

type snapshotScanner interface {
	Scan(dest ...any) error
}

func (d *DB) scanSnapshot(ctx context.Context, row snapshotScanner) (SnapshotEntry, bool, error) {
	s, err := d.scanSnapshotRows(ctx, row)
	if errors.Is(err, sql.ErrNoRows) {
		return SnapshotEntry{}, false, nil
	}
	return s, err == nil, err
}

func (d *DB) scanSnapshotRows(ctx context.Context, row snapshotScanner) (SnapshotEntry, error) {
	var s SnapshotEntry
	var created int64
	var ringEnc, cwdEnc, envEnc []byte
	if err := row.Scan(&s.ID, &s.SessionID, &s.Name, &created, &ringEnc, &cwdEnc, &envEnc, &s.TranscriptOffset); err != nil {
		return SnapshotEntry{}, err
	}
	var err error
	s.RingBuffer, err = d.openBytes(ctx, "snapshots", s.ID, "ring_buffer_enc", ringEnc)
	if err != nil {
		return SnapshotEntry{}, err
	}
	s.CWD, err = d.openString(ctx, "snapshots", s.ID, "cwd_enc", cwdEnc)
	if err != nil {
		return SnapshotEntry{}, err
	}
	envPlain, err := d.openBytes(ctx, "snapshots", s.ID, "env_enc", envEnc)
	if err != nil {
		return SnapshotEntry{}, err
	}
	if err := json.Unmarshal(envPlain, &s.Env); err != nil {
		return SnapshotEntry{}, err
	}
	s.CreatedAt = time.Unix(created, 0)
	return s, nil
}
