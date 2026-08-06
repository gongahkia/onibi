package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/store"
)

type ClaudeQuestionEvent struct{ Question *store.ClaudeQuestion }

type claudeQuestionInput struct {
	Questions []claudeQuestionSpec `json:"questions"`
}
type claudeQuestionSpec struct {
	Header      string                 `json:"header"`
	Question    string                 `json:"question"`
	Options     []claudeQuestionOption `json:"options"`
	MultiSelect bool                   `json:"multiSelect"`
}
type claudeQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

func (d *Daemon) ClaudeQuestionEvents() <-chan ClaudeQuestionEvent { return d.claudeQuestionEvents }

func parseClaudeQuestionInput(raw []byte) (claudeQuestionInput, []byte, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return claudeQuestionInput{}, nil, errors.New("invalid Claude question payload")
	}
	var input claudeQuestionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return claudeQuestionInput{}, nil, errors.New("invalid Claude question payload")
	}
	if len(input.Questions) < 1 || len(input.Questions) > 4 {
		return claudeQuestionInput{}, nil, errors.New("Claude question count must be 1..4")
	}
	seen := map[string]bool{}
	for i := range input.Questions {
		q := &input.Questions[i]
		q.Header, q.Question = strings.TrimSpace(q.Header), strings.TrimSpace(q.Question)
		if q.Header == "" || q.Question == "" || utf8.RuneCountInString(q.Header) > 64 || utf8.RuneCountInString(q.Question) > 1000 {
			return claudeQuestionInput{}, nil, errors.New("invalid Claude question text")
		}
		if seen[q.Question] {
			return claudeQuestionInput{}, nil, errors.New("duplicate Claude question")
		}
		seen[q.Question] = true
		if len(q.Options) < 1 || len(q.Options) > 12 {
			return claudeQuestionInput{}, nil, errors.New("Claude options must be 1..12")
		}
		options := map[string]bool{}
		for j := range q.Options {
			o := &q.Options[j]
			o.Label, o.Description = strings.TrimSpace(o.Label), strings.TrimSpace(o.Description)
			if o.Label == "" || utf8.RuneCountInString(o.Label) > 128 || utf8.RuneCountInString(o.Description) > 512 || options[o.Label] {
				return claudeQuestionInput{}, nil, errors.New("invalid Claude option")
			}
			options[o.Label] = true
		}
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return claudeQuestionInput{}, nil, err
	}
	return input, canonical, nil
}

func (d *Daemon) handleClaudeQuestion(ctx context.Context, ev intake.Event) (intake.Response, error) {
	if d.DB == nil {
		return intake.Response{Decision: "cancelled", Reason: "state unavailable"}, nil
	}
	s, err := d.sessionByID(ev.Session)
	if err != nil || s.Agent != "claude" || s.Transport != "tmux" || strings.ToLower(strings.TrimSpace(ev.Agent)) != "claude" {
		return intake.Response{Decision: "cancelled", Reason: "unknown Claude session"}, nil
	}
	input, canonical, err := parseClaudeQuestionInput([]byte(ev.InputJSON))
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
	}
	_ = input
	item := store.ClaudeQuestion{ID: NewID(), SessionID: s.ID, InputJSON: canonical, ExpiresAt: time.Now().Add(d.ClaudeQuestionTimeout)}
	if err := d.DB.ClaudeQuestionCreate(ctx, item); err != nil {
		return intake.Response{Decision: "cancelled", Reason: "store Claude question"}, nil
	}
	item.State = store.ClaudeQuestionPending
	item.CreatedAt = time.Now()
	d.claudeQuestionMu.Lock()
	d.claudeQuestions[item.ID] = make(chan struct{})
	d.claudeQuestionMu.Unlock()
	d.audit(ctx, "claude.question.request", s.ID, string(canonical), 0, "id="+item.ID)
	select {
	case d.claudeQuestionEvents <- ClaudeQuestionEvent{Question: &item}:
	default:
		_, _, _ = d.resolveClaudeQuestion(ctx, item.ID, store.ClaudeQuestionCancelled, nil, "question queue busy", 0)
	}
	return d.waitClaudeQuestion(ctx, item.ID, item.ExpiresAt)
}

func (d *Daemon) waitClaudeQuestion(ctx context.Context, id string, expiry time.Time) (intake.Response, error) {
	d.claudeQuestionMu.Lock()
	waiter := d.claudeQuestions[id]
	d.claudeQuestionMu.Unlock()
	if waiter == nil {
		return intake.Response{Decision: "cancelled", Reason: "question unavailable"}, nil
	}
	timer := time.NewTimer(time.Until(expiry))
	defer timer.Stop()
	select {
	case <-waiter:
	case <-timer.C:
		_, _, _ = d.resolveClaudeQuestion(context.Background(), id, store.ClaudeQuestionExpired, nil, "timed out", 0)
	case <-ctx.Done():
		_, _, _ = d.resolveClaudeQuestion(context.Background(), id, store.ClaudeQuestionCancelled, nil, "daemon stopped", 0)
	}
	item, found, err := d.DB.ClaudeQuestion(context.Background(), id)
	d.unregisterClaudeQuestion(id)
	if err != nil || !found {
		return intake.Response{Decision: "cancelled", Reason: "question unavailable"}, nil
	}
	if item.State == store.ClaudeQuestionAnswered {
		return intake.Response{Decision: "approve", Answers: item.Answers}, nil
	}
	if item.State == store.ClaudeQuestionExpired {
		return intake.Response{Decision: "expired", Reason: item.Reason}, nil
	}
	return intake.Response{Decision: "cancelled", Reason: item.Reason}, nil
}

func (d *Daemon) ClaudeQuestion(ctx context.Context, id string) (*store.ClaudeQuestion, bool, error) {
	if d.DB == nil {
		return nil, false, errors.New("state unavailable")
	}
	return d.DB.ClaudeQuestion(ctx, id)
}

func (d *Daemon) AnswerClaudeQuestion(ctx context.Context, id string, answers map[string]string, by int64) (*store.ClaudeQuestion, error) {
	item, found, err := d.ClaudeQuestion(ctx, id)
	if err != nil || !found {
		return nil, errors.New("Claude question expired")
	}
	if item.State != store.ClaudeQuestionPending || !time.Now().Before(item.ExpiresAt) {
		return nil, errors.New("Claude question expired")
	}
	if err := validateClaudeAnswers(item.InputJSON, answers); err != nil {
		return nil, err
	}
	updated, changed, err := d.resolveClaudeQuestion(ctx, id, store.ClaudeQuestionAnswered, answers, "answered from Telegram", by)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, errors.New("Claude question expired")
	}
	return updated, nil
}

func (d *Daemon) CancelClaudeQuestion(ctx context.Context, id, reason string, by int64) error {
	_, changed, err := d.resolveClaudeQuestion(ctx, id, store.ClaudeQuestionCancelled, nil, reason, by)
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("Claude question expired")
	}
	return nil
}

func validateClaudeAnswers(raw []byte, answers map[string]string) error {
	input, _, err := parseClaudeQuestionInput(raw)
	if err != nil {
		return err
	}
	if len(answers) != len(input.Questions) {
		return errors.New("all Claude questions require an answer")
	}
	for _, q := range input.Questions {
		answer := strings.TrimSpace(answers[q.Question])
		if answer == "" || utf8.RuneCountInString(answer) > 1000 {
			return fmt.Errorf("answer required for %s", q.Header)
		}
		labels := map[string]bool{}
		for _, option := range q.Options {
			labels[option.Label] = true
		}
		values := strings.Split(answer, ",")
		if !q.MultiSelect && len(values) != 1 {
			return fmt.Errorf("%s accepts one answer", q.Header)
		}
		seen := map[string]bool{}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if !labels[value] {
				continue
			}
			if seen[value] {
				return fmt.Errorf("duplicate answer for %s", q.Header)
			}
			seen[value] = true
		}
	}
	return nil
}

func (d *Daemon) resolveClaudeQuestion(ctx context.Context, id, state string, answers map[string]string, reason string, by int64) (*store.ClaudeQuestion, bool, error) {
	item, changed, err := d.DB.ClaudeQuestionResolve(ctx, id, state, answers, reason, by)
	if changed {
		d.audit(ctx, "claude.question."+state, item.SessionID, "", by, "id="+id)
		d.closeClaudeQuestionWaiter(id)
	}
	return item, changed, err
}

func (d *Daemon) unregisterClaudeQuestion(id string) {
	d.claudeQuestionMu.Lock()
	delete(d.claudeQuestions, id)
	d.claudeQuestionMu.Unlock()
}
func (d *Daemon) closeClaudeQuestionWaiter(id string) {
	d.claudeQuestionMu.Lock()
	waiter := d.claudeQuestions[id]
	d.claudeQuestionMu.Unlock()
	if waiter != nil {
		select {
		case <-waiter:
		default:
			close(waiter)
		}
	}
}
func (d *Daemon) cancelClaudeQuestionsForSession(ctx context.Context, sessionID, reason string) {
	if d.DB == nil {
		return
	}
	rows, err := d.DB.SQL().QueryContext(ctx, `SELECT id FROM claude_questions WHERE session_id=? AND state=?`, sessionID, store.ClaudeQuestionPending)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			_, _, _ = d.resolveClaudeQuestion(ctx, id, store.ClaudeQuestionCancelled, nil, reason, 0)
		}
	}
}
