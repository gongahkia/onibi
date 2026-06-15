package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
)

func TestParseHookPayloadSnakeAndCamel(t *testing.T) {
	raw := []byte(`{"hook_event_name":"PreToolUse","session_id":"s1","cwd":"/tmp/repo","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	p := parseHookPayload(raw)
	if p.EventName != "PreToolUse" || p.SessionID != "s1" || p.Tool != "Bash" {
		t.Fatalf("bad snake payload: %+v", p)
	}
	if !strings.Contains(p.InputJSON, "ls") {
		t.Fatalf("missing input JSON: %s", p.InputJSON)
	}

	raw = []byte(`{"sessionId":"s2","toolName":"run","toolArgs":{"x":1}}`)
	p = parseHookPayload(raw)
	if p.SessionID != "s2" || p.Tool != "run" || !strings.Contains(p.InputJSON, `"x":1`) {
		t.Fatalf("bad camel payload: %+v", p)
	}
}

func TestProviderResponses(t *testing.T) {
	out, errOut, code := providerResponse("codex", intake.Response{Decision: "edited", UpdatedInput: `{"command":"echo ok"}`})
	if code != 0 || errOut != "" {
		t.Fatalf("bad codex edit code=%d stderr=%q", code, errOut)
	}
	var codex map[string]any
	if err := json.Unmarshal([]byte(out), &codex); err != nil {
		t.Fatal(err)
	}
	hso := codex["hookSpecificOutput"].(map[string]any)
	if hso["permissionDecision"] != "allow" {
		t.Fatalf("bad codex response: %s", out)
	}

	out, _, code = providerResponse("copilot", intake.Response{Decision: "deny", Reason: "no"})
	if code != 0 || !strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("bad copilot deny: code=%d out=%s", code, out)
	}

	out, _, code = providerResponse("gemini", intake.Response{Decision: "expired"})
	if code != 0 || !strings.Contains(out, `"decision":"deny"`) {
		t.Fatalf("bad gemini expired: code=%d out=%s", code, out)
	}

	out, errOut, code = providerResponse("codex", intake.Response{Decision: "cancelled", Reason: "unmanaged external hook"})
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("cancelled should fail open: code=%d out=%q stderr=%q", code, out, errOut)
	}
}

func TestManagedSession(t *testing.T) {
	if id, ok := managedSession(""); ok || id != "" {
		t.Fatalf("empty managed session = %q %v", id, ok)
	}
	t.Setenv("ONIBI_SESSION_ID", "env-session")
	if id, ok := managedSession(""); !ok || id != "env-session" {
		t.Fatalf("env managed session = %q %v", id, ok)
	}
	if id, ok := managedSession("flag-session"); !ok || id != "flag-session" {
		t.Fatalf("override managed session = %q %v", id, ok)
	}
}
