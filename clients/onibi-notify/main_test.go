package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
)

func TestParsePiPayload(t *testing.T) {
	payload, err := parsePiPayload(strings.NewReader(`{"version":"onibi.pi.v1","tool_name":"bash","tool_input":{"command":"pwd"},"cwd":"/tmp","pi_session_id":"pi-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if payload.ToolName != "bash" || string(payload.ToolInput) != `{"command":"pwd"}` || payload.PiSession != "pi-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestParsePiPayloadRejectsUnknownShape(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version":"old","tool_name":"bash","tool_input":{}}`, `{"version":"onibi.pi.v1","tool_name":"bash","tool_input":[]}`} {
		if _, err := parsePiPayload(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestPiResponse(t *testing.T) {
	if got := piResponse(intake.Response{Decision: "approve"}); got["decision"] != "approve" {
		t.Fatalf("approve = %#v", got)
	}
	if got := piResponse(intake.Response{Decision: "deny", Reason: "no"}); got["decision"] != "deny" || got["reason"] != "no" {
		t.Fatalf("deny = %#v", got)
	}
}

func TestParsePiLifecyclePayload(t *testing.T) {
	payload, err := parsePiLifecyclePayload(strings.NewReader(`{"version":"onibi.pi.v1","lifecycle":"agent_end","run_id":"run-1","cwd":"/tmp","pi_session_id":"pi-1"}`))
	if err != nil || payload.Lifecycle != "agent_end" || payload.PiSession != "pi-1" || payload.RunID != "run-1" {
		t.Fatalf("payload=%#v err=%v", payload, err)
	}
	if _, err := parsePiLifecyclePayload(strings.NewReader(`{"version":"onibi.pi.v1","lifecycle":"unknown","run_id":"run-1"}`)); err == nil {
		t.Fatal("accepted unknown lifecycle")
	}
}

func TestRunFailsClosedWhenDaemonIsUnavailable(t *testing.T) {
	var output bytes.Buffer
	err := run(
		[]string{"--agent", "pi", "--format", "pi", "--type", "approval_request", "--wait", "--response", "onibi-json"},
		strings.NewReader(`{"version":"onibi.pi.v1","tool_name":"bash","tool_input":{"command":"pwd"}}`),
		&output,
		func(key string) string {
			if key == "ONIBI_SESSION_ID" {
				return "session-1"
			}
			if key == "ONIBI_SOCK" {
				return "/tmp/onibi-missing.sock"
			}
			return ""
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]string
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["decision"] != "cancelled" {
		t.Fatalf("response=%#v", response)
	}
}
