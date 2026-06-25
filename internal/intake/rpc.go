package intake

// Response is the JSON the server writes back to a request-mode client
// (currently approval_request). Unsuitable for fire-and-forget events.
type Response struct {
	// Decision is the verdict reached. One of: approve, deny, edited,
	// expired, cancelled.
	Decision string `json:"decision"`
	// UpdatedInput is populated when Decision == "edited"; raw JSON of the
	// new tool input that should be passed to the agent.
	UpdatedInput string `json:"updated_input,omitempty"`
	// Reason is a short human-readable string describing the outcome
	// (populated for deny/expired/cancelled).
	Reason string `json:"reason,omitempty"`
	// DecidedBy is the deciding actor id for audit/error messages.
	DecidedBy int64 `json:"decided_by,omitempty"`
	// Text is used by non-approval request/response calls.
	Text string `json:"text,omitempty"`
	// SessionID is used by session lifecycle RPC calls.
	SessionID string `json:"session_id,omitempty"`
	// Mode is used by session lifecycle RPC calls.
	Mode string `json:"mode,omitempty"`
}
