package intake

import (
	"errors"
	"time"
)

// RejectedError marks an RPC failure that happened before any input, key,
// or lifecycle change reached a session.
type RejectedError struct{ Err error }

func (e *RejectedError) Error() string { return e.Err.Error() }
func (e *RejectedError) Unwrap() error { return e.Err }

func Reject(err error) error {
	if err == nil {
		return nil
	}
	return &RejectedError{Err: err}
}

func IsRejected(err error) bool {
	var target *RejectedError
	return errors.As(err, &target)
}

// Attention is a program-agnostic summary of whether a session may need a
// person: recent output, quiet output, a terminal bell, or an input prompt.
type Attention struct {
	State        string    `json:"state"`
	Reason       string    `json:"reason,omitempty"`
	Since        time.Time `json:"since,omitzero"`
	LastOutputAt time.Time `json:"lastOutputAt,omitzero"`
}

type SessionInfo struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Agent          string    `json:"agent"`
	Transport      string    `json:"transport"`
	CWD            string    `json:"cwd,omitempty"`
	Command        string    `json:"command,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
	Attention      Attention `json:"attention"`
}

// Response is the JSON the server writes back to a local request-mode client.
type Response struct {
	// Decision is the Pi approval verdict: approve, deny, expired, or cancelled.
	Decision string `json:"decision"`
	// Reason is a short human-readable string describing the outcome
	// (populated for deny/expired/cancelled).
	Reason string `json:"reason,omitempty"`
	// DecidedBy is the deciding actor id for audit/error messages.
	DecidedBy int64 `json:"decided_by,omitempty"`
	// Text is used by non-approval request/response calls.
	Text string `json:"text,omitempty"`
	// SessionID is used by session lifecycle RPC calls.
	SessionID string `json:"session_id,omitempty"`
	// Rejected reports that the failure described by Reason preceded any
	// session side effect, so the caller may treat the action as not sent.
	Rejected bool `json:"rejected,omitempty"`
	// ANSI carries captured output with terminal escapes when the backend
	// preserves them; Text carries the same capture as plain text.
	ANSI     string        `json:"ansi,omitempty"`
	Sessions []SessionInfo `json:"sessions,omitempty"`
}
