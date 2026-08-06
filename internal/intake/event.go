package intake

import "github.com/gongahkia/onibi/internal/approval"

const (
	TypeApprovalRequest = "approval_request"
	TypeSessionInput    = "session_input"
	TypeSessionPeek     = "session_peek"
	TypeSessionNew      = "session_new"
	TypeSessionControl  = "session_control"
	TypePing            = "ping"
)

// Event is the same-user Unix-socket protocol for local CLI control and Pi approvals.
type Event struct {
	Type    string   `json:"type"`
	Session string   `json:"session,omitempty"`
	Agent   string   `json:"agent,omitempty"`
	CWD     string   `json:"cwd,omitempty"`
	Text    string   `json:"text,omitempty"`
	Enter   bool     `json:"enter,omitempty"`
	Action  string   `json:"action,omitempty"`
	Name    string   `json:"name,omitempty"`
	Args    []string `json:"args,omitempty"`

	Tool      string            `json:"tool,omitempty"`
	InputJSON string            `json:"input_json,omitempty"`
	Approval  *approval.Request `json:"approval,omitempty"`
	TS        int64             `json:"ts,omitempty"`
}
