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

func TestParseClaudePayload(t *testing.T) {
	payload, err := parseClaudePayload(strings.NewReader(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"pwd"},"cwd":"/tmp"}`))
	if err != nil || payload.ToolName != "Bash" || string(payload.ToolInput) != `{"command":"pwd"}` {
		t.Fatalf("payload=%#v err=%v", payload, err)
	}
	if _, err := parseClaudePayload(strings.NewReader(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":[]}`)); err == nil {
		t.Fatal("accepted invalid Claude payload")
	}
}

func TestClaudeResponse(t *testing.T) {
	approved := claudeResponse("approve", "")
	hook := approved["hookSpecificOutput"].(map[string]any)
	decision := hook["decision"].(map[string]any)
	if decision["behavior"] != "allow" {
		t.Fatalf("approval=%#v", approved)
	}
	denied := claudeResponse("deny", "no")
	hook = denied["hookSpecificOutput"].(map[string]any)
	decision = hook["decision"].(map[string]any)
	if decision["behavior"] != "deny" || decision["message"] != "no" {
		t.Fatalf("denial=%#v", denied)
	}
}

func TestClaudeQuestionResponse(t *testing.T) {
	raw := json.RawMessage(`{"questions":[{"header":"Mode","question":"Choose mode","options":[{"label":"Safe"}]}]}`)
	response := claudeQuestionResponse(raw, intake.Response{Decision: "approve", Answers: map[string]string{"Choose mode": "Safe"}})
	hook := response["hookSpecificOutput"].(map[string]any)
	if hook["permissionDecision"] != "allow" {
		t.Fatalf("response=%#v", response)
	}
	updated := hook["updatedInput"].(map[string]any)
	if updated["answers"].(map[string]string)["Choose mode"] != "Safe" {
		t.Fatalf("response=%#v", response)
	}
}

func TestRunClaudeQuestionFailsClosed(t *testing.T) {
	var output bytes.Buffer
	err := run([]string{"--agent", "claude", "--format", "claude", "--type", "question_request", "--wait", "--response", "claude-question-json"}, strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"header":"Mode","question":"Choose mode","options":[{"label":"Safe"}]}]}}`), &output, func(string) string { return "" })
	if err != nil || !strings.Contains(output.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("err=%v output=%s", err, output.String())
	}
}

func TestRunClaudeFailsClosedWhenDaemonIsUnavailable(t *testing.T) {
	var output bytes.Buffer
	err := run(
		[]string{"--agent", "claude", "--format", "claude", "--type", "approval_request", "--wait", "--response", "claude-json"},
		strings.NewReader(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"pwd"}}`),
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
	if !strings.Contains(output.String(), `"behavior":"deny"`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestClaudeLifecycle(t *testing.T) {
	for hook, want := range map[string]string{"Stop": "agent_end", "StopFailure": "agent_failed"} {
		got, err := claudeLifecycle(hook)
		if err != nil || got != want {
			t.Fatalf("hook=%q lifecycle=%q err=%v", hook, got, err)
		}
	}
	if _, err := claudeLifecycle("PermissionRequest"); err == nil {
		t.Fatal("accepted PermissionRequest as lifecycle")
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
