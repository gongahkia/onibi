package intake

// Event types emitted by hooks and consumed by the daemon.
const (
	TypeAgentDone       = "agent_done"       // turn complete, no input awaited
	TypeAgentAwaiting   = "agent_awaiting"   // agent is blocked on user input
	TypeAgentMessage    = "agent_message"    // arbitrary status message
	TypeCmdDone         = "cmd_done"         // shell command finished (zsh precmd hook)
	TypeSessionExited   = "session_exited"   // host process exited
	TypeApprovalRequest = "approval_request" // Phase 3: tool-call blocked
	TypeSessionInput    = "session_input"    // RPC: write text into a live session
	TypeSessionPeek     = "session_peek"     // RPC: return recent session output
	TypeSessionNew      = "session_new"      // RPC: create a tmux-backed session
	TypeSessionShow     = "session_show"     // RPC: open a visible terminal for a session
	TypeSessionHide     = "session_hide"     // RPC: detach or end visible clients
	TypeDemoApproval    = "demo_approval"    // RPC: create a local fake approval
	TypeTrust           = "trust"            // RPC: list and mutate trust rules
	TypeBudget          = "budget"           // RPC: budget usage summary
	TypePing            = "ping"             // RPC: daemon health probe
)

// Event is the wire-level JSON schema written by hooks and onibi-notify.
// Field set is intentionally a union — hooks fill what they have, daemon
// reads what it needs. Schema is forward-compatible: unknown fields ignored.
type Event struct {
	Type    string `json:"type"`              // one of the Type* constants
	Session string `json:"session,omitempty"` // session id (from ONIBI_SESSION_ID env)
	Managed bool   `json:"managed,omitempty"` // true when session id came from Onibi env/explicit override
	Agent   string `json:"agent,omitempty"`   // emitting adapter/provider
	PID     int    `json:"pid,omitempty"`     // emitting process id (fallback identity)
	CWD     string `json:"cwd,omitempty"`     // provider working directory

	EventName         string `json:"event_name,omitempty"`          // provider lifecycle event name
	ProviderSessionID string `json:"provider_session_id,omitempty"` // provider-native session id

	// cmd_done / session_exited
	Status  int    `json:"status,omitempty"` // exit code
	Cmd     string `json:"cmd,omitempty"`    // command line
	Elapsed int64  `json:"elapsed_ms,omitempty"`

	// agent_*
	Text  string   `json:"text,omitempty"`  // optional human-readable detail
	Tail  string   `json:"tail,omitempty"`  // optional output tail provided by hook
	Enter bool     `json:"enter,omitempty"` // session_input: append newline
	Limit int      `json:"limit,omitempty"` // session_peek: tail bytes
	Mode  string   `json:"mode,omitempty"`  // session_new: headless|visible; session_hide: headless|end
	Name  string   `json:"name,omitempty"`  // session_new: optional label
	Args  []string `json:"args,omitempty"`  // session_new: command args

	// approval_request (Phase 3 — schema reserved now)
	ApprovalID string `json:"approval_id,omitempty"`
	Tool       string `json:"tool,omitempty"`
	ToolTarget string `json:"tool_target,omitempty"`
	Command    string `json:"command,omitempty"`
	FilePath   string `json:"file_path,omitempty"`
	Risk       string `json:"risk,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	InputJSON  string `json:"input_json,omitempty"`
	RawJSON    string `json:"raw_json,omitempty"` // raw provider hook payload

	// trust RPC
	TrustAction string `json:"trust_action,omitempty"`
	TrustRoot   string `json:"trust_root,omitempty"`
	TrustRuleID string `json:"trust_rule_id,omitempty"`
	Effect      string `json:"effect,omitempty"`
	Expires     string `json:"expires,omitempty"`

	TS int64 `json:"ts,omitempty"` // unix epoch seconds; if 0, server fills
}
