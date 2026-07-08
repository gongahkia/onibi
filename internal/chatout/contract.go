package chatout

import (
	"context"
	"time"
)

type Capability string

const (
	CapabilityApprovalSend     Capability = "approval.send"
	CapabilityApprovalDecision Capability = "approval.decision"
	CapabilityTextOut          Capability = "text.out"
	CapabilityTextIn           Capability = "text.in"
	CapabilityTailStream       Capability = "tail.stream"
	CapabilityReconnect        Capability = "reconnect"
	CapabilityNotifyReceipt    Capability = "notify.receipt"
)

type Sender struct {
	ID          string
	DisplayName string
	ChannelID   string
}

type ApprovalRequest struct {
	ID        string
	SessionID string
	Agent     string
	Tool      string
	InputJSON string
	Diff      string
	RiskLevel string
}

type Decision struct {
	ApprovalID   string
	Verdict      string
	UpdatedInput string
	Sender       Sender
	MessageID    string
}

type RateLimitBucket struct {
	Limit  int
	Burst  int
	Window time.Duration
}

type RateLimitPolicy struct {
	PerSecond RateLimitBucket
	PerMinute RateLimitBucket
}

type AuditInteraction struct {
	Kind      string
	MessageID string
	SessionID string
	Payload   string
	Sender    Sender
	Meta      map[string]any
}

type Provider interface {
	Name() string
	Capabilities() []Capability
	SendApproval(context.Context, ApprovalRequest) (string, error)
	OnDecision(string, func(Decision)) error
	SendText(context.Context, string) error
	OnInboundText(func(string, Sender)) error
	TailStream(context.Context, string, <-chan []byte) error
	Connect(context.Context) error
	Reconnect(context.Context) error
	Close() error
	RecordInteraction(context.Context, AuditInteraction) error
	RateLimit() RateLimitPolicy
}
