package adapters

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/capability"
	"github.com/gongahkia/onibi/internal/store"
)

func TestCertifiedContractsMatchV1AgentAllowlist(t *testing.T) {
	contracts := CertifiedContracts()
	if len(contracts) != len(capability.V1Agents()) {
		t.Fatalf("contracts=%d v1 agents=%d", len(contracts), len(capability.V1Agents()))
	}
	for i, agent := range capability.V1Agents() {
		c := contracts[i]
		if c.Agent != agent || c.Version != CertifiedContractVersion || !c.Certified {
			t.Fatalf("contract[%d]=%+v", i, c)
		}
		if !c.Installation.Managed || !c.Installation.Idempotent || !c.Installation.IntegrityVerified || !c.Installation.OriginalConfigBacked {
			t.Fatalf("installation contract=%+v", c.Installation)
		}
		if c.Approval.Delivery != "same_uid_unix_socket" || !c.Approval.BlocksTool ||
			c.Approval.Deny != DecisionDeny || c.Approval.Expire != DecisionDeny ||
			c.Approval.DaemonUnavailable != DecisionAllow || c.Approval.RequestTimeout != DecisionAllow {
			t.Fatalf("approval contract=%+v", c.Approval)
		}
		if c.Approval.ReviewRequired != (agent == capability.AgentCodex) {
			t.Fatalf("review requirement contract=%+v", c.Approval)
		}
		if !c.Lifecycle.SessionStart || !c.Lifecycle.Activity || !c.Lifecycle.ApprovalRequest || !c.Lifecycle.TurnComplete || c.Recovery.PendingApproval == "" {
			t.Fatalf("lifecycle/recovery contract=%+v %+v", c.Lifecycle, c.Recovery)
		}
		if !c.Budget.GlobalEnforcement || ((agent == capability.AgentClaude || agent == capability.AgentPi) && (!c.Budget.TokenTelemetry || !c.Budget.SessionEnforcement || c.Budget.NonEnforcingReason != "")) || (agent == capability.AgentCodex && (c.Budget.TokenTelemetry || c.Budget.SessionEnforcement || c.Budget.NonEnforcingReason == "")) {
			t.Fatalf("budget contract=%+v", c.Budget)
		}
		if !c.Audit.DecisionRecorded || !c.Audit.PayloadHashOnly || c.Audit.RawPayloadRecorded {
			t.Fatalf("audit contract=%+v", c.Audit)
		}
	}
	if _, ok := ContractFor("opencode"); ok {
		t.Fatal("deferred adapter has a certified contract")
	}
	if _, ok := ContractFor(" CODEX "); !ok {
		t.Fatal("contract lookup must normalize names")
	}
}

func TestCertifiedAdapterInstallAndVerifySmoke(t *testing.T) {
	db, notify := adapterRegistryFixture(t)
	for _, contract := range CertifiedContracts() {
		t.Run(contract.Agent, func(t *testing.T) {
			a, ok := Get(contract.Agent)
			if !ok {
				t.Fatalf("missing adapter %q", contract.Agent)
			}
			if err := a.Install(context.Background(), db, notify); err != nil {
				t.Fatalf("install: %v", err)
			}
			if err := a.Verify(context.Background(), db); err != nil {
				t.Fatalf("verify: %v", err)
			}
			if info := a.Status(context.Background(), db); !info.Installed || !info.Managed || info.Tampered {
				t.Fatalf("status=%+v", info)
			}
		})
	}
}

func TestCertifiedDecisionAuditHashesPayload(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := approval.New(db, approval.DefaultTTL)
	payload := `{"command":"deploy --token raw-sensitive-value"}`
	id, _, err := q.Request(t.Context(), "s1", capability.AgentClaude, "Bash", payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Decide(t.Context(), id, approval.VerdictDeny, "", "owner denied", 7); err != nil {
		t.Fatal(err)
	}
	rows, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Action != "approval.decided" || rows[0].PayloadHash == "" || strings.Contains(rows[0].Detail, "raw-sensitive-value") {
		t.Fatalf("audit=%+v", rows)
	}
}
