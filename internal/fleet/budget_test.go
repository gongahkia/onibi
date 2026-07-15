package fleet

import (
	"strings"
	"testing"
	"time"
)

func TestBudgetReportNormalizesAndBindsHeartbeatSignature(t *testing.T) {
	report := BudgetReport{Date: "2026-07-15", DailyTokens: 12, GlobalLimit: 10, OnOverrun: "KILL", Sessions: []BudgetSession{{SessionID: "session-b", Agent: "CODEX", Tokens: 4, Limit: 3, OnOverrun: "interrupt"}, {SessionID: "session-a", Agent: "Claude", Tokens: 8, Limit: 5, OnOverrun: "kill", Measured: true}}}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	normalized := report.Normalized()
	if normalized.OnOverrun != "kill" || normalized.Sessions[0].SessionID != "session-a" || normalized.Sessions[0].Agent != "claude" {
		t.Fatalf("normalized=%#v", normalized)
	}
	heartbeat := Heartbeat{Version: ProtocolVersion, OwnerID: "owner-local", HostID: "host-local", SentAt: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC), BinaryVersion: "v1.0.0", Budget: normalized}
	baseline := string(HeartbeatSigningPayload(heartbeat))
	heartbeat.Budget.Sessions[0].Tokens++
	if baseline == string(HeartbeatSigningPayload(heartbeat)) {
		t.Fatal("budget tokens did not bind heartbeat signature")
	}
	heartbeat.Budget.Date = "2026-07-14"
	if err := heartbeat.Validate(); err == nil {
		t.Fatal("expected UTC-day mismatch rejection")
	}
}

func TestBudgetReportRejectsUnmeasuredAndOversizedInputs(t *testing.T) {
	badAgent := BudgetReport{Date: "2026-07-15", OnOverrun: "interrupt", Sessions: []BudgetSession{{SessionID: "session-a", Agent: "shell", OnOverrun: "interrupt"}}}
	if err := badAgent.Validate(); err == nil {
		t.Fatal("expected unsupported agent rejection")
	}
	report := BudgetReport{Date: "2026-07-15", OnOverrun: "interrupt", Sessions: make([]BudgetSession, maxBudgetSessions+1)}
	for i := range report.Sessions {
		report.Sessions[i] = BudgetSession{SessionID: "session-" + strings.Repeat("a", 55) + string(rune('a'+i%26)), Agent: "claude", OnOverrun: "interrupt"}
	}
	if err := report.Validate(); err == nil {
		t.Fatal("expected oversized report rejection")
	}
}
