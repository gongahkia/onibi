package intake

import "github.com/gongahkia/onibi/internal/approval"

const (
	TypeApprovalRequest = "approval_request"
	TypeClaudeQuestion  = "claude_question"
	TypeSessionInput    = "session_input"
	TypeSessionPeek     = "session_peek"
	TypeSessionNew      = "session_new"
	TypeSessionControl  = "session_control"
	TypeAgentLifecycle  = "agent_lifecycle"
	TypePing            = "ping"
)

// Event is the same-user Unix-socket protocol for local CLI control and Pi approvals.
type Event struct {
	Type      string   `json:"type"`
	Session   string   `json:"session,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	CWD       string   `json:"cwd,omitempty"`
	Text      string   `json:"text,omitempty"`
	Enter     bool     `json:"enter,omitempty"`
	Action    string   `json:"action,omitempty"`
	Lifecycle string   `json:"lifecycle,omitempty"`
	RunID     string   `json:"run_id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Args      []string `json:"args,omitempty"`

	Tool      string            `json:"tool,omitempty"`
	InputJSON string            `json:"input_json,omitempty"`
	Approval  *approval.Request `json:"approval,omitempty"`
	TS        int64             `json:"ts,omitempty"`
}
