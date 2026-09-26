package intake

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
	SessionID string            `json:"session_id,omitempty"`
	Answers   map[string]string `json:"answers,omitempty"`
}
