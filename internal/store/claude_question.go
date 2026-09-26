package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	ClaudeQuestionPending   = "pending"
	ClaudeQuestionAnswered  = "answered"
	ClaudeQuestionCancelled = "cancelled"
	ClaudeQuestionExpired   = "expired"
)

type ClaudeQuestion struct {
	ID        string
	SessionID string
	InputJSON json.RawMessage
	State     string
	Answers   map[string]string
	Reason    string
	CreatedAt time.Time
	DecidedAt time.Time
	DecidedBy int64
	ExpiresAt time.Time
}

func (d *DB) ClaudeQuestionCreate(ctx context.Context, item ClaudeQuestion) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.SessionID) == "" || len(item.InputJSON) == 0 || item.ExpiresAt.IsZero() {
		return errors.New("Claude question id, session, input, and expiry required")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	if item.State == "" {
		item.State = ClaudeQuestionPending
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO claude_questions(id,session_id,input_json,state,created_at,expires_at) VALUES(?,?,?,?,?,?)`, item.ID, item.SessionID, item.InputJSON, item.State, item.CreatedAt.Unix(), item.ExpiresAt.Unix())
	return err
}

func (d *DB) ClaudeQuestion(ctx context.Context, id string) (*ClaudeQuestion, bool, error) {
	var item ClaudeQuestion
	var input, answers []byte
	var created, decided sql.NullInt64
	var expires int64
	var by sql.NullInt64
	err := d.sql.QueryRowContext(ctx, `SELECT id,session_id,input_json,state,COALESCE(answers_json,''),COALESCE(reason,''),created_at,decided_at,decided_by,expires_at FROM claude_questions WHERE id=?`, id).Scan(&item.ID, &item.SessionID, &input, &item.State, &answers, &item.Reason, &created, &decided, &by, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	item.InputJSON = append(item.InputJSON, input...)
	if len(answers) > 0 && json.Unmarshal(answers, &item.Answers) != nil {
		return nil, false, errors.New("invalid stored Claude answers")
	}
	item.CreatedAt, item.ExpiresAt = time.Unix(created.Int64, 0), time.Unix(expires, 0)
	if decided.Valid {
		item.DecidedAt = time.Unix(decided.Int64, 0)
	}
	if by.Valid {
		item.DecidedBy = by.Int64
	}
	return &item, true, nil
}

func (d *DB) ClaudeQuestionResolve(ctx context.Context, id, state string, answers map[string]string, reason string, by int64) (*ClaudeQuestion, bool, error) {
	if state != ClaudeQuestionAnswered && state != ClaudeQuestionCancelled && state != ClaudeQuestionExpired {
		return nil, false, errors.New("invalid Claude question state")
	}
	var encoded []byte
	var err error
	if answers != nil {
		encoded, err = json.Marshal(answers)
		if err != nil {
			return nil, false, err
		}
	}
	now := time.Now().Unix()
	var decidedBy any
	if by != 0 {
		decidedBy = by
	}
	result, err := d.sql.ExecContext(ctx, `UPDATE claude_questions SET state=?,answers_json=?,reason=?,decided_at=?,decided_by=? WHERE id=? AND state=?`, state, nullIfEmpty(string(encoded)), nullIfEmpty(strings.TrimSpace(reason)), now, decidedBy, id, ClaudeQuestionPending)
	if err != nil {
		return nil, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	item, found, err := d.ClaudeQuestion(ctx, id)
	return item, found && changed == 1, err
}

func (d *DB) ClaudeQuestionsCancelPending(ctx context.Context, reason string) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `UPDATE claude_questions SET state=?,reason=?,decided_at=? WHERE state=?`, ClaudeQuestionCancelled, nullIfEmpty(strings.TrimSpace(reason)), time.Now().Unix(), ClaudeQuestionPending)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *DB) ClaudeQuestionsCancelSession(ctx context.Context, sessionID, reason string) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `UPDATE claude_questions SET state=?,reason=?,decided_at=? WHERE session_id=? AND state=?`, ClaudeQuestionCancelled, nullIfEmpty(strings.TrimSpace(reason)), time.Now().Unix(), sessionID, ClaudeQuestionPending)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
