package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	PromptQueued    = "queued"
	PromptSent      = "sent"
	PromptCancelled = "cancelled"
	PromptFailed    = "failed"
)

type PromptEntry struct {
	ID        string
	SessionID string
	ChatID    int64
	Text      string
	State     string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
	SentAt    time.Time
}

var ErrPromptNotQueued = errors.New("prompt is not queued")

func (d *DB) PromptEnqueue(ctx context.Context, sessionID string, chatID int64, text string) (PromptEntry, error) {
	id, err := newPromptID()
	if err != nil {
		return PromptEntry{}, err
	}
	now := time.Now().Unix()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return PromptEntry{}, err
	}
	defer tx.Rollback()
	var pos int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM prompt_queue WHERE session_id = ? AND state = ?`, sessionID, PromptQueued).Scan(&pos); err != nil {
		return PromptEntry{}, err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO prompt_queue(id, session_id, chat_id, text, state, position, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sessionID, chatID, text, PromptQueued, pos, now, now)
	if err != nil {
		return PromptEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return PromptEntry{}, err
	}
	return PromptEntry{ID: id, SessionID: sessionID, ChatID: chatID, Text: text, State: PromptQueued, Position: pos, CreatedAt: time.Unix(now, 0), UpdatedAt: time.Unix(now, 0)}, nil
}

func (d *DB) PromptGet(ctx context.Context, id string) (PromptEntry, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, session_id, chat_id, text, state, position, created_at, updated_at, COALESCE(sent_at, 0)
		   FROM prompt_queue WHERE id = ?`, id)
	return scanPrompt(row)
}

func (d *DB) PromptList(ctx context.Context, sessionID string, includeDone bool, n int) ([]PromptEntry, error) {
	if n <= 0 {
		n = 50
	}
	where := `WHERE state = ?`
	args := []any{PromptQueued}
	if includeDone {
		where = `WHERE 1 = 1`
		args = nil
	}
	if sessionID != "" {
		where += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	args = append(args, n)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, session_id, chat_id, text, state, position, created_at, updated_at, COALESCE(sent_at, 0)
		   FROM prompt_queue `+where+`
		  ORDER BY CASE WHEN state = 'queued' THEN 0 ELSE 1 END, position ASC, created_at DESC
		  LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PromptEntry
	for rows.Next() {
		p, err := scanPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) PromptNext(ctx context.Context, sessionID string) (PromptEntry, bool, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, session_id, chat_id, text, state, position, created_at, updated_at, COALESCE(sent_at, 0)
		   FROM prompt_queue
		  WHERE session_id = ? AND state = ?
		  ORDER BY position ASC, created_at ASC
		  LIMIT 1`, sessionID, PromptQueued)
	p, err := scanPrompt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PromptEntry{}, false, nil
	}
	if err != nil {
		return PromptEntry{}, false, err
	}
	return p, true, nil
}

func (d *DB) PromptUpdateText(ctx context.Context, id, text string) (PromptEntry, error) {
	now := time.Now().Unix()
	res, err := d.sql.ExecContext(ctx,
		`UPDATE prompt_queue SET text = ?, updated_at = ? WHERE id = ? AND state = ?`,
		text, now, id, PromptQueued)
	if err != nil {
		return PromptEntry{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return PromptEntry{}, ErrPromptNotQueued
	}
	return d.PromptGet(ctx, id)
}

func (d *DB) PromptMove(ctx context.Context, id string, position int) (PromptEntry, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return PromptEntry{}, err
	}
	defer tx.Rollback()
	p, err := promptGetTx(ctx, tx, id)
	if err != nil {
		return PromptEntry{}, err
	}
	if p.State != PromptQueued {
		return PromptEntry{}, ErrPromptNotQueued
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM prompt_queue WHERE session_id = ? AND state = ?`, p.SessionID, PromptQueued).Scan(&count); err != nil {
		return PromptEntry{}, err
	}
	if position < 1 {
		position = 1
	}
	if position > count {
		position = count
	}
	if position == p.Position {
		return p, tx.Commit()
	}
	if position < p.Position {
		if _, err := tx.ExecContext(ctx,
			`UPDATE prompt_queue SET position = position + 1, updated_at = ? WHERE session_id = ? AND state = ? AND position >= ? AND position < ?`,
			time.Now().Unix(), p.SessionID, PromptQueued, position, p.Position); err != nil {
			return PromptEntry{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE prompt_queue SET position = position - 1, updated_at = ? WHERE session_id = ? AND state = ? AND position <= ? AND position > ?`,
			time.Now().Unix(), p.SessionID, PromptQueued, position, p.Position); err != nil {
			return PromptEntry{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE prompt_queue SET position = ?, updated_at = ? WHERE id = ?`, position, time.Now().Unix(), id); err != nil {
		return PromptEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return PromptEntry{}, err
	}
	return d.PromptGet(ctx, id)
}

func (d *DB) PromptMoveRelative(ctx context.Context, id string, delta int) (PromptEntry, error) {
	p, err := d.PromptGet(ctx, id)
	if err != nil {
		return PromptEntry{}, err
	}
	return d.PromptMove(ctx, id, p.Position+delta)
}

func (d *DB) PromptSetState(ctx context.Context, id, state string) (PromptEntry, error) {
	now := time.Now().Unix()
	sentAt := any(nil)
	if state == PromptSent {
		sentAt = now
	}
	res, err := d.sql.ExecContext(ctx,
		`UPDATE prompt_queue SET state = ?, updated_at = ?, sent_at = COALESCE(?, sent_at) WHERE id = ? AND state = ?`,
		state, now, sentAt, id, PromptQueued)
	if err != nil {
		return PromptEntry{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return PromptEntry{}, ErrPromptNotQueued
	}
	return d.PromptGet(ctx, id)
}

func (d *DB) PromptCancelQueued(ctx context.Context, sessionID string) (int64, error) {
	now := time.Now().Unix()
	res, err := d.sql.ExecContext(ctx,
		`UPDATE prompt_queue SET state = ?, updated_at = ? WHERE state = ? AND (? = '' OR session_id = ?)`,
		PromptCancelled, now, PromptQueued, sessionID, sessionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) PromptFailQueued(ctx context.Context, sessionID string) (int64, error) {
	now := time.Now().Unix()
	res, err := d.sql.ExecContext(ctx,
		`UPDATE prompt_queue SET state = ?, updated_at = ? WHERE state = ? AND session_id = ?`,
		PromptFailed, now, PromptQueued, sessionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanPrompt(scanner interface {
	Scan(dest ...any) error
}) (PromptEntry, error) {
	var p PromptEntry
	var created, updated, sent int64
	err := scanner.Scan(&p.ID, &p.SessionID, &p.ChatID, &p.Text, &p.State, &p.Position, &created, &updated, &sent)
	if err != nil {
		return PromptEntry{}, err
	}
	p.CreatedAt = time.Unix(created, 0)
	p.UpdatedAt = time.Unix(updated, 0)
	if sent > 0 {
		p.SentAt = time.Unix(sent, 0)
	}
	return p, nil
}

func promptGetTx(ctx context.Context, tx *sql.Tx, id string) (PromptEntry, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, session_id, chat_id, text, state, position, created_at, updated_at, COALESCE(sent_at, 0)
		   FROM prompt_queue WHERE id = ?`, id)
	return scanPrompt(row)
}

func newPromptID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("prompt id: %w", err)
	}
	return "p" + hex.EncodeToString(b[:]), nil
}
