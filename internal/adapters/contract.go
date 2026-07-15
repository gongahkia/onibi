package adapters

import (
	"strings"

	"github.com/gongahkia/onibi/internal/capability"
)

const CertifiedContractVersion = "1"

type DecisionEffect string

const (
	DecisionAllow            DecisionEffect = "allow"
	DecisionDeny             DecisionEffect = "deny"
	DecisionAllowWithUpdated DecisionEffect = "allow_with_updated_input"
)

type AdapterContract struct {
	Version      string               `json:"version"`
	Agent        string               `json:"agent"`
	Certified    bool                 `json:"certified"`
	Installation InstallationContract `json:"installation"`
	Approval     ApprovalContract     `json:"approval"`
	Lifecycle    LifecycleContract    `json:"lifecycle"`
	Recovery     RecoveryContract     `json:"recovery"`
	Budget       BudgetContract       `json:"budget"`
	Audit        AuditContract        `json:"audit"`
}

type InstallationContract struct {
	Managed              bool `json:"managed"`
	Idempotent           bool `json:"idempotent"`
	IntegrityVerified    bool `json:"integrity_verified"`
	OriginalConfigBacked bool `json:"original_config_backed_up"`
}

type ApprovalContract struct {
	Delivery          string         `json:"delivery"`
	BlocksTool        bool           `json:"blocks_tool"`
	ReviewRequired    bool           `json:"review_required"`
	Approve           DecisionEffect `json:"approve"`
	Deny              DecisionEffect `json:"deny"`
	Edit              DecisionEffect `json:"edit"`
	Expire            DecisionEffect `json:"expire"`
	DaemonUnavailable DecisionEffect `json:"daemon_unavailable"`
	RequestTimeout    DecisionEffect `json:"request_timeout"`
}

type LifecycleContract struct {
	SessionStart    bool `json:"session_start"`
	Activity        bool `json:"activity"`
	ApprovalRequest bool `json:"approval_request"`
	TurnComplete    bool `json:"turn_complete"`
	SessionExit     bool `json:"session_exit"`
}

type RecoveryContract struct {
	HookReloadInstruction string `json:"hook_reload_instruction"`
	PendingApproval       string `json:"pending_approval"`
}

type BudgetContract struct {
	TokenTelemetry     bool   `json:"token_telemetry"`
	SessionEnforcement bool   `json:"session_enforcement"`
	GlobalEnforcement  bool   `json:"global_enforcement"`
	NonEnforcingReason string `json:"non_enforcing_reason,omitempty"`
}

type AuditContract struct {
	DecisionRecorded   bool `json:"decision_recorded"`
	PayloadHashOnly    bool `json:"payload_hash_only"`
	RawPayloadRecorded bool `json:"raw_payload_recorded"`
}

var certifiedContracts = map[string]AdapterContract{
	capability.AgentClaude: certifiedContract(capability.AgentClaude, true, false, "run claude and inspect /hooks"),
	capability.AgentCodex:  certifiedContract(capability.AgentCodex, false, true, "run codex, review hooks, and trust matching commands"),
	capability.AgentPi:     certifiedContract(capability.AgentPi, true, false, "run /reload"),
}

func certifiedContract(agent string, sessionExit, reviewRequired bool, reload string) AdapterContract {
	return AdapterContract{
		Version:   CertifiedContractVersion,
		Agent:     agent,
		Certified: true,
		Installation: InstallationContract{
			Managed:              true,
			Idempotent:           true,
			IntegrityVerified:    true,
			OriginalConfigBacked: true,
		},
		Approval: ApprovalContract{
			Delivery:          "same_uid_unix_socket",
			BlocksTool:        true,
			ReviewRequired:    reviewRequired,
			Approve:           DecisionAllow,
			Deny:              DecisionDeny,
			Edit:              DecisionAllowWithUpdated,
			Expire:            DecisionDeny,
			DaemonUnavailable: DecisionAllow,
			RequestTimeout:    DecisionAllow,
		},
		Lifecycle: LifecycleContract{
			SessionStart:    true,
			Activity:        true,
			ApprovalRequest: true,
			TurnComplete:    true,
			SessionExit:     sessionExit,
		},
		Recovery: RecoveryContract{
			HookReloadInstruction: reload,
			PendingApproval:       "persisted; parked hook waiters are not reattached after daemon restart",
		},
		Budget: budgetContract(agent),
		Audit: AuditContract{
			DecisionRecorded:   true,
			PayloadHashOnly:    true,
			RawPayloadRecorded: false,
		},
	}
}

func budgetContract(agent string) BudgetContract {
	if agent == capability.AgentClaude {
		return BudgetContract{TokenTelemetry: true, SessionEnforcement: true, GlobalEnforcement: true}
	}
	return BudgetContract{GlobalEnforcement: true, NonEnforcingReason: "interactive token telemetry unavailable"}
}

func ContractFor(name string) (AdapterContract, bool) {
	c, ok := certifiedContracts[strings.ToLower(strings.TrimSpace(name))]
	return c, ok
}

func CertifiedContracts() []AdapterContract {
	out := make([]AdapterContract, 0, len(capability.V1Agents()))
	for _, name := range capability.V1Agents() {
		if c, ok := ContractFor(name); ok {
			out = append(out, c)
		}
	}
	return out
}
