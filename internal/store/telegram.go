package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	TelegramUpdateStarted   = "started"
	TelegramUpdateCompleted = "completed"
	TelegramUpdateUncertain = "uncertain"
	OutboxPending           = "pending"
	OutboxRunning           = "running"
	OutboxDelivered         = "delivered"
	OutboxExpired           = "expired"
)

type TelegramOutboxIntent struct {
	ID          string
	DedupeKey   string
	Kind        string
	ChatID      int64
	SessionID   string
	Title       string
	Lines       int
	ForceScreen bool
	State       string
	Attempts    int
	NextAttempt time.Time
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastError   string
}

type TelegramOutboxStats struct {
	Pending   int
	Running   int
	Delivered int
	Expired   int
}

func (d *DB) TelegramOutboxStats(ctx context.Context) (TelegramOutboxStats, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT state,COUNT(*) FROM telegram_outbox GROUP BY state`)
	if err != nil {
		return TelegramOutboxStats{}, err
	}
	defer rows.Close()
	var stats TelegramOutboxStats
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return TelegramOutboxStats{}, err
		}
		switch state {
		case OutboxPending:
			stats.Pending = count
		case OutboxRunning:
			stats.Running = count
		case OutboxDelivered:
			stats.Delivered = count
		case OutboxExpired:
			stats.Expired = count
		}
	}
	return stats, rows.Err()
}

func (d *DB) TelegramNextOffset(ctx context.Context) (int64, error) {
	var offset int64
	err := d.sql.QueryRowContext(ctx, `SELECT next_offset FROM telegram_cursor WHERE id=1`).Scan(&offset)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return offset, err
}

// TelegramClaimUpdate commits the next poll offset before side effects.
func (d *DB) TelegramClaimUpdate(ctx context.Context, updateID int64) (bool, error) {
	if updateID < 1 {
		return false, errors.New("telegram update id required")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO telegram_updates(update_id,state,received_at) VALUES(?,?,?)`, updateID, TelegramUpdateStarted, time.Now().Unix())
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO telegram_cursor(id,next_offset) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET next_offset=MAX(next_offset,excluded.next_offset)`, updateID+1); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (d *DB) TelegramCompleteUpdate(ctx context.Context, updateID int64) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE telegram_updates SET state=?,completed_at=? WHERE update_id=? AND state=?`, TelegramUpdateCompleted, time.Now().Unix(), updateID, TelegramUpdateStarted)
	return err
}

// TelegramMarkUncertainUpdates prevents restart replay and returns the count.
func (d *DB) TelegramMarkUncertainUpdates(ctx context.Context) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `UPDATE telegram_updates SET state=?,completed_at=? WHERE state=?`, TelegramUpdateUncertain, time.Now().Unix(), TelegramUpdateStarted)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *DB) TelegramPurge(ctx context.Context, before time.Time) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM telegram_updates WHERE state IN (?,?) AND received_at<?`, TelegramUpdateCompleted, TelegramUpdateUncertain, before.Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM telegram_outbox WHERE state IN (?,?) AND created_at<?`, OutboxDelivered, OutboxExpired, before.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) TelegramOutboxUpsert(ctx context.Context, item TelegramOutboxIntent) error {
	if item.ID == "" || item.DedupeKey == "" || item.Kind == "" || item.ChatID == 0 || (item.Kind != "ended" && item.Title == "") {
		return errors.New("telegram outbox id, dedupe key, kind, chat id, and title required")
	}
	if item.Lines < 1 {
		item.Lines = 80
	}
	now := time.Now()
	if item.NextAttempt.IsZero() {
		item.NextAttempt = now
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.ExpiresAt.IsZero() {
		item.ExpiresAt = now.Add(24 * time.Hour)
	}
	if item.Kind == "ended" {
		var state string
		err := d.sql.QueryRowContext(ctx, `SELECT state FROM telegram_outbox WHERE dedupe_key=?`, item.DedupeKey).Scan(&state)
		if err == nil && state == OutboxDelivered {
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO telegram_outbox(id,dedupe_key,kind,chat_id,session_id,title,lines,force_screen,state,attempts,next_attempt,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(dedupe_key) DO UPDATE SET kind=excluded.kind,chat_id=excluded.chat_id,session_id=excluded.session_id,title=excluded.title,lines=excluded.lines,force_screen=excluded.force_screen,state=?,attempts=0,next_attempt=excluded.next_attempt,created_at=excluded.created_at,expires_at=excluded.expires_at,last_error=NULL`,
		item.ID, item.DedupeKey, item.Kind, item.ChatID, nullIfEmpty(item.SessionID), item.Title, item.Lines, boolInt(item.ForceScreen), OutboxPending, 0, item.NextAttempt.Unix(), item.CreatedAt.Unix(), item.ExpiresAt.Unix(), OutboxPending)
	return err
}

func (d *DB) TelegramOutboxRecover(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE telegram_outbox SET state=?,next_attempt=? WHERE state=?`, OutboxPending, time.Now().Unix(), OutboxRunning)
	return err
}

func (d *DB) TelegramOutboxClaim(ctx context.Context) (*TelegramOutboxIntent, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `UPDATE telegram_outbox SET state=? WHERE state IN (?,?) AND expires_at<=?`, OutboxExpired, OutboxPending, OutboxRunning, now); err != nil {
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `SELECT id,dedupe_key,kind,chat_id,COALESCE(session_id,''),title,lines,force_screen,state,attempts,next_attempt,created_at,expires_at,COALESCE(last_error,'') FROM telegram_outbox WHERE state=? AND next_attempt<=? ORDER BY created_at LIMIT 1`, OutboxPending, now)
	item, err := scanTelegramOutbox(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE telegram_outbox SET state=? WHERE id=? AND state=?`, OutboxRunning, item.ID, OutboxPending); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	item.State = OutboxRunning
	return &item, nil
}

func (d *DB) TelegramOutboxDelivered(ctx context.Context, id string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE telegram_outbox SET state=?,last_error=NULL WHERE id=?`, OutboxDelivered, id)
	return err
}

func (d *DB) TelegramOutboxRetry(ctx context.Context, id, reason string, next time.Time) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE telegram_outbox SET state=?,attempts=attempts+1,next_attempt=?,last_error=? WHERE id=?`, OutboxPending, next.Unix(), reason, id)
	return err
}

type telegramOutboxRow interface{ Scan(...any) error }

func scanTelegramOutbox(row telegramOutboxRow) (TelegramOutboxIntent, error) {
	var item TelegramOutboxIntent
	var next, created, expires int64
	var force int
	err := row.Scan(&item.ID, &item.DedupeKey, &item.Kind, &item.ChatID, &item.SessionID, &item.Title, &item.Lines, &force, &item.State, &item.Attempts, &next, &created, &expires, &item.LastError)
	if err != nil {
		return TelegramOutboxIntent{}, err
	}
	item.ForceScreen = force != 0
	item.NextAttempt, item.CreatedAt, item.ExpiresAt = time.Unix(next, 0), time.Unix(created, 0), time.Unix(expires, 0)
	return item, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
