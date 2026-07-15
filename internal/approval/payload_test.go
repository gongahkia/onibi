package approval

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func TestNormalizeRequestCertifiedAdapters(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "pi"} {
		t.Run(agent, func(t *testing.T) {
			req, err := NormalizeRequest(Request{
				SessionID: "s1",
				Agent:     agent,
				Tool:      "Bash",
				Input:     []byte(`{"z":1,"command":"rm -rf /tmp/x"}`),
				Details:   Details{Command: "forged"},
				Risk:      Risk{Level: RiskLow},
			})
			if err != nil {
				t.Fatal(err)
			}
			if req.Version != ApprovalSchemaV1 || string(req.Input) != `{"command":"rm -rf /tmp/x","z":1}` {
				t.Fatalf("request = %#v", req)
			}
			if req.Details.Command != "rm -rf /tmp/x" || req.Risk.Level != RiskHigh {
				t.Fatalf("derived request = %#v", req)
			}
		})
	}
}

func TestNormalizeRequestRejectsInvalidBoundary(t *testing.T) {
	for _, input := range []string{"[]", `"command"`, "null", `{"x":`, ""} {
		_, err := NormalizeRequest(Request{SessionID: "s1", Agent: "claude", Tool: "Bash", Input: []byte(input)})
		if input == "" {
			if err != nil {
				t.Fatalf("empty input = %v", err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("input %q accepted", input)
		}
	}
	if _, err := NormalizeRequest(Request{Agent: "claude", Tool: "Bash", Input: []byte(`{}`)}); err == nil {
		t.Fatal("missing session accepted")
	}
}

func TestPayloadForApprovalScrubsSensitiveDetails(t *testing.T) {
	payload, err := PayloadForApproval(Approval{
		ID:        "a1",
		SessionID: "s1",
		Agent:     "claude",
		Tool:      "Bash",
		InputJSON: `{"command":"deploy --token raw-sensitive-value"}`,
		State:     StatePending,
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Version != ApprovalSchemaV1 || strings.Contains(payload.Details.Command, "raw-sensitive-value") || strings.Contains(payload.ScrubbedInput, "raw-sensitive-value") {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestDecisionAuditHashesCanonicalInputOnly(t *testing.T) {
	db, err := store.OpenEphemeral(t.TempDir() + "/onibi.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db, time.Minute)
	id, _, err := q.RequestModel(context.Background(), Request{
		SessionID: "s1",
		Agent:     "pi",
		Tool:      "Bash",
		Input:     []byte(`{"command":"deploy --token raw-sensitive-value"}`),
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Decide(context.Background(), id, VerdictDeny, "", "no", 7); err != nil {
		t.Fatal(err)
	}
	entries, err := db.AuditRecent(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].PayloadHash == "" || strings.Contains(entries[0].Detail, "raw-sensitive-value") {
		t.Fatalf("audit = %#v", entries)
	}
}
