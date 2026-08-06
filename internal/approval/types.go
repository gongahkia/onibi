package approval

import "time"

// State values for the approval row's lifecycle. Terminal states are
// final — the Decide path enforces this via WHERE state='pending' guards.
const (
	StatePending   = "pending"
	StateApproved  = "approved"
	StateDenied    = "denied"
	StateExpired   = "expired"
	StateCancelled = "cancelled"
)

// Verdict mirrors State but as the verb the user (or system) chose.
type Verdict string

const (
	VerdictApprove Verdict = "approve"
	VerdictDeny    Verdict = "deny"
	VerdictExpire  Verdict = "expired"
	VerdictCancel  Verdict = "cancelled"
)

// Terminal reports whether s is a final state.
func Terminal(s string) bool {
	switch s {
	case StateApproved, StateDenied, StateExpired, StateCancelled:
		return true
	}
	return false
}

// StateForVerdict maps a Verdict to its persisted State.
func StateForVerdict(v Verdict) string {
	switch v {
	case VerdictApprove:
		return StateApproved
	case VerdictDeny:
		return StateDenied
	case VerdictExpire:
		return StateExpired
	case VerdictCancel:
		return StateCancelled
	}
	return ""
}

// Approval is the in-memory record of a pending or decided approval.
// Persistence schema lives in internal/store (table: approvals).
type Approval struct {
	ID          string
	SessionID   string
	Agent       string
	Tool        string
	InputJSON string // raw Pi tool input
	State     string
	Reason    string // populated when State == StateDenied/Expired/Cancelled
	CreatedAt time.Time
	DecidedAt time.Time
	ExpiresAt time.Time
	DecidedBy int64 // deciding actor id (audit)
}

// Decision is what the queue returns to the parked Pi extension.
// Exactly one of these is produced per approval.
type Decision struct {
	Verdict   Verdict `json:"verdict"`
	Reason    string  `json:"reason,omitempty"`
	DecidedBy int64   `json:"decided_by,omitempty"`
	DecidedAt int64   `json:"decided_at,omitempty"` // unix sec
}

const (
	EventRequested = "approval.requested"
	EventDecided   = "approval.decided"
	EventExpired   = "approval.expired"
)

// Event is emitted after every approval queue state transition.
type Event struct {
	Type     string
	Approval Approval
	Decision Decision
	At       time.Time
}

// DefaultTTL is the hard upper bound on approval lifetime; stale approvals
// are expired to prevent late-decision hijack.
const DefaultTTL = 5 * time.Minute

// ParanoidTTL is the shorter TTL applied when paranoid-mode is set.
const ParanoidTTL = 60 * time.Second
