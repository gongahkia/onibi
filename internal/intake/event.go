package intake

const (
	TypeSessionInput   = "session_input"
	TypeSessionPeek    = "session_peek"
	TypeSessionNew     = "session_new"
	TypeSessionAdopt   = "session_adopt"
	TypeSessionControl = "session_control"
	TypeSessionList    = "session_list"
	TypeSessionCapture = "session_capture"
	TypeSessionKey     = "session_key"
	TypePing           = "ping"
)

// Event is the same-user Unix-socket protocol for local CLI and web control.
type Event struct {
	Type    string   `json:"type"`
	Session string   `json:"session,omitempty"`
	Agent   string   `json:"agent,omitempty"`
	CWD     string   `json:"cwd,omitempty"`
	Text    string   `json:"text,omitempty"`
	Enter   bool     `json:"enter,omitempty"`
	Action  string   `json:"action,omitempty"`
	Name    string   `json:"name,omitempty"`
	Mux     string   `json:"mux,omitempty"`
	Args    []string `json:"args,omitempty"`
	Key     string   `json:"key,omitempty"`
	Lines   int      `json:"lines,omitempty"`
	// Exact requires Session to be a full live session ID. Name and prefix
	// matching remain available to the interactive CLI only.
	Exact bool `json:"exact,omitempty"`

	TS int64 `json:"ts,omitempty"`
}
