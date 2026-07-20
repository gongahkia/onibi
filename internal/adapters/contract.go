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
	Version                string               `json:"version"`
	Agent                  string               `json:"agent"`
	Certified              bool                 `json:"certified"`
	MinimumProviderVersion string               `json:"minimum_provider_version,omitempty"`
	Installation           InstallationContract `json:"installation"`
	Approval               ApprovalContract     `json:"approval"`
	Lifecycle              LifecycleContract    `json:"lifecycle"`
	Recovery               RecoveryContract     `json:"recovery"`
	Audit                  AuditContract        `json:"audit"`
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

type AuditContract struct {
	DecisionRecorded   bool `json:"decision_recorded"`
	PayloadHashOnly    bool `json:"payload_hash_only"`
	RawPayloadRecorded bool `json:"raw_payload_recorded"`
}

var adapterContracts = map[string]AdapterContract{
	capability.AgentClaude: certifiedContract(capability.AgentClaude, true, false, "run claude and inspect /hooks"),
	capability.AgentCodex:  certifiedContract(capability.AgentCodex, false, true, "run codex, review hooks, and trust matching commands"),
	capability.AgentPi:     certifiedContract(capability.AgentPi, true, false, "run /reload"),
	"copilot":              copilotContract(),
	"gemini":               geminiContract(),
	"opencode":             openCodeContract(),
	"amp":                  ampContract(),
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
		Audit: AuditContract{
			DecisionRecorded:   true,
			PayloadHashOnly:    true,
			RawPayloadRecorded: false,
		},
	}
}

func ampContract() AdapterContract {
	return AdapterContract{
		Version:   CertifiedContractVersion,
		Agent:     "amp",
		Certified: false,
		Installation: InstallationContract{
			Managed:              true,
			Idempotent:           true,
			IntegrityVerified:    true,
			OriginalConfigBacked: true,
		},
		Approval: ApprovalContract{
			Delivery:          "same_uid_unix_socket",
			BlocksTool:        true,
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
		},
		Recovery: RecoveryContract{
			HookReloadInstruction: "run plugins: reload",
			PendingApproval:       "no plugin-level waiter recovery claim; use the persisted Onibi approval state",
		},
		Audit: AuditContract{},
	}
}

func openCodeContract() AdapterContract {
	return AdapterContract{
		Version:                CertifiedContractVersion,
		Agent:                  "opencode",
		Certified:              false,
		MinimumProviderVersion: "1.18.3",
		Installation: InstallationContract{
			Managed:              true,
			Idempotent:           true,
			IntegrityVerified:    true,
			OriginalConfigBacked: true,
		},
		Approval: ApprovalContract{
			Delivery:          "same_uid_unix_socket",
			BlocksTool:        true,
			Approve:           DecisionAllow,
			Deny:              DecisionDeny,
			Edit:              DecisionAllowWithUpdated,
			Expire:            DecisionDeny,
			DaemonUnavailable: DecisionAllow,
			RequestTimeout:    DecisionAllow,
		},
		Lifecycle: LifecycleContract{
			Activity:        true,
			ApprovalRequest: true,
			TurnComplete:    true,
			SessionExit:     true,
		},
		Recovery: RecoveryContract{
			HookReloadInstruction: "restart OpenCode or start a new session",
			PendingApproval:       "no plugin-level waiter recovery claim; use the persisted Onibi approval state",
		},
		Audit: AuditContract{},
	}
}

func geminiContract() AdapterContract {
	return AdapterContract{
		Version:                CertifiedContractVersion,
		Agent:                  "gemini",
		Certified:              false,
		MinimumProviderVersion: "0.43.0",
		Installation: InstallationContract{
			Managed:              true,
			Idempotent:           true,
			IntegrityVerified:    true,
			OriginalConfigBacked: true,
		},
		Approval: ApprovalContract{
			Delivery:          "same_uid_unix_socket",
			BlocksTool:        true,
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
			SessionExit:     true,
		},
		Recovery: RecoveryContract{
			HookReloadInstruction: "restart Gemini CLI and inspect the configured hooks",
			PendingApproval:       "no hook-level waiter recovery claim; use the persisted Onibi approval state",
		},
		Audit: AuditContract{},
	}
}

func copilotContract() AdapterContract {
	return AdapterContract{
		Version:                CertifiedContractVersion,
		Agent:                  "copilot",
		Certified:              false,
		MinimumProviderVersion: "1.0.54",
		Installation: InstallationContract{
			Managed:              true,
			Idempotent:           true,
			IntegrityVerified:    true,
			OriginalConfigBacked: true,
		},
		Approval: ApprovalContract{
			Delivery:          "same_uid_unix_socket",
			BlocksTool:        true,
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
			SessionExit:     true,
		},
		Recovery: RecoveryContract{
			HookReloadInstruction: "restart Copilot CLI so hook configurations are loaded",
			PendingApproval:       "no hook-level waiter recovery claim; use the persisted Onibi approval state",
		},
		Audit: AuditContract{},
	}
}

func ContractFor(name string) (AdapterContract, bool) {
	c, ok := adapterContracts[strings.ToLower(strings.TrimSpace(name))]
	return c, ok
}

func CertifiedContracts() []AdapterContract {
	out := make([]AdapterContract, 0, len(capability.V1Agents()))
	for _, name := range capability.V1Agents() {
		if c, ok := ContractFor(name); ok && c.Certified {
			out = append(out, c)
		}
	}
	return out
}
