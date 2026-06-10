package approval

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

// ErrAlreadyDecided is returned by Decide when the approval is already in a
// terminal state. Idempotent callers should treat this as success.
var ErrAlreadyDecided = errors.New("approval already decided")

// ErrUnknownApproval is returned when no row matches the supplied id.
var ErrUnknownApproval = errors.New("unknown approval id")

// Queue owns the in-memory waiters map plus the SQLite-backed state machine.
// Safe for concurrent use.
type Queue struct {
	db  *store.DB
	ttl time.Duration

	mu      sync.Mutex
	waiters map[string]chan Decision // approval id → single-shot delivery channel
}

// New returns a Queue using the given TTL for fresh approvals.
func New(db *store.DB, ttl time.Duration) *Queue {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Queue{db: db, ttl: ttl, waiters: map[string]chan Decision{}}
}

// Request creates a pending approval row, registers an in-memory waiter,
// and returns the new id plus a channel the caller blocks on. The channel
// receives exactly one Decision (from Decide, expiry sweeper, or Cancel)
// then is closed.
//
// The caller is responsible for:
//   - sending a Telegram message that surfaces the approval (the daemon
//     does this via SetMessage after the bot send returns msg/chat ids)
//   - reading the Decision from the returned channel
//   - context-cancelling if it gives up (no need to call Cancel — the
//     waiter map is GC'd when the approval is decided OR purged)
func (q *Queue) Request(ctx context.Context, sessionID, agent, tool, inputJSON string) (string, <-chan Decision, error) {
	id, err := newID()
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	exp := now.Add(q.ttl)

	_, err = q.db.SQL().ExecContext(ctx,
		`INSERT INTO approvals(id, session_id, agent, tool, input_json, state, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sessionID, agent, tool, inputJSON, StatePending, now.Unix(), exp.Unix())
	if err != nil {
		return "", nil, fmt.Errorf("insert approval: %w", err)
	}

	ch := make(chan Decision, 1)
	q.mu.Lock()
	q.waiters[id] = ch
	q.mu.Unlock()
	return id, ch, nil
}

// SetMessage records the Telegram (chat, message) the approval was rendered
// to, so a follow-up editMessageReplyMarkup can edit it in place after the
// decision lands.
func (q *Queue) SetMessage(ctx context.Context, id string, chatID, msgID int64) error {
	_, err := q.db.SQL().ExecContext(ctx,
		`UPDATE approvals SET chat_id = ?, msg_id = ? WHERE id = ?`,
		chatID, msgID, id)
	return err
}

// Get returns the approval row.
func (q *Queue) Get(ctx context.Context, id string) (*Approval, error) {
	row := q.db.SQL().QueryRowContext(ctx,
		`SELECT id, session_id, agent, tool, input_json, state,
		        COALESCE(edited_json, ''), COALESCE(msg_id, 0), COALESCE(chat_id, 0),
		        created_at, COALESCE(decided_at, 0), expires_at
		 FROM approvals WHERE id = ?`, id)
	a := &Approval{}
	var createdAt, decidedAt, expiresAt int64
	err := row.Scan(&a.ID, &a.SessionID, &a.Agent, &a.Tool, &a.InputJSON, &a.State,
		&a.EditedJSON, &a.MsgID, &a.ChatID, &createdAt, &decidedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnknownApproval
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt = time.Unix(createdAt, 0)
	if decidedAt > 0 {
		a.DecidedAt = time.Unix(decidedAt, 0)
	}
	a.ExpiresAt = time.Unix(expiresAt, 0)
	return a, nil
}

// Decide is the only path that transitions a pending approval to a terminal
// state. Atomic — uses WHERE state='pending' guard so concurrent callers
// (Telegram callback + expiry sweeper + cancel) can't double-decide.
//
// On success, delivers the Decision to the registered waiter (if any) and
// removes it from the waiters map. Returns ErrAlreadyDecided if another
// caller won.
func (q *Queue) Decide(ctx context.Context, id string, verdict Verdict, editedJSON, reason string, decidedBy int64) error {
	st := StateForVerdict(verdict)
	if st == "" {
		return fmt.Errorf("invalid verdict %q", verdict)
	}
	now := time.Now()
	res, err := q.db.SQL().ExecContext(ctx,
		`UPDATE approvals
		   SET state = ?,
		       edited_json = ?,
		       decided_at = ?
		 WHERE id = ? AND state = ?`,
		st, nullIfEmpty(editedJSON), now.Unix(), id, StatePending)
	if err != nil {
		return fmt.Errorf("update approval: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// either unknown id or already decided — distinguish for caller
		if _, gerr := q.Get(ctx, id); errors.Is(gerr, ErrUnknownApproval) {
			return ErrUnknownApproval
		}
		return ErrAlreadyDecided
	}

	d := Decision{
		Verdict:   verdict,
		Reason:    reason,
		DecidedBy: decidedBy,
		DecidedAt: now.Unix(),
	}
	if verdict == VerdictEdit && editedJSON != "" {
		d.UpdatedInput = json.RawMessage(editedJSON)
	}
	q.deliver(id, d)
	return nil
}

// Cancel is a convenience that decides with VerdictCancel. Use this when
// the daemon is shutting down with approvals still pending.
func (q *Queue) Cancel(ctx context.Context, id, reason string) error {
	return q.Decide(ctx, id, VerdictCancel, "", reason, 0)
}

// PendingIDs returns the ids of all currently-pending approvals (used by
// the expiry sweeper). Excludes already-expired-but-not-swept rows by
// checking expires_at > now-grace.
func (q *Queue) PendingIDs(ctx context.Context) ([]string, error) {
	rows, err := q.db.SQL().QueryContext(ctx,
		`SELECT id FROM approvals WHERE state = ?`, StatePending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ExpireOverdue sweeps all pending approvals whose expires_at <= now,
// transitioning them to expired and notifying waiters. Returns the count.
func (q *Queue) ExpireOverdue(ctx context.Context) (int, error) {
	now := time.Now().Unix()
	rows, err := q.db.SQL().QueryContext(ctx,
		`SELECT id FROM approvals WHERE state = ? AND expires_at <= ?`,
		StatePending, now)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	n := 0
	for _, id := range ids {
		if err := q.Decide(ctx, id, VerdictExpire, "", "approval expired (5 min TTL)", 0); err == nil {
			n++
		} else if errors.Is(err, ErrAlreadyDecided) {
			// raced with a real decision — fine
			continue
		} else {
			return n, err
		}
	}
	return n, nil
}

// deliver sends the decision to the registered waiter and removes it.
// Idempotent — if no waiter is registered (e.g. daemon restart, the hook's
// socket connection already EOF'd), this is a no-op. The DB row still
// reflects the decision for audit and `onibi log`.
func (q *Queue) deliver(id string, d Decision) {
	q.mu.Lock()
	ch, ok := q.waiters[id]
	if ok {
		delete(q.waiters, id)
	}
	q.mu.Unlock()
	if !ok {
		return
	}
	// non-blocking send into 1-buffered channel — guaranteed to succeed
	// since each waiter is created fresh per Request and only delivered to
	// once. Close after send so a reader using select-with-default can also
	// observe channel close.
	ch <- d
	close(ch)
}

// DropWaiter removes the in-memory waiter for id without changing the DB
// state. Used by the intake handler when its socket connection drops
// (client gave up); the approval row remains pending and may still be
// decided by Telegram callback — that decision just won't go anywhere
// (no waiter) which is fine.
func (q *Queue) DropWaiter(id string) {
	q.mu.Lock()
	delete(q.waiters, id)
	q.mu.Unlock()
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
