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
	if p.Command != "ls" || p.ToolTarget != "ls" || p.Risk != "low" {
		t.Fatalf("missing normalized command fields: %+v", p)
	}

	raw = []byte(`{"sessionId":"s2","toolName":"run","toolArgs":{"x":1}}`)
	p = parseHookPayload(raw)
	if p.SessionID != "s2" || p.Tool != "run" || !strings.Contains(p.InputJSON, `"x":1`) {
		t.Fatalf("bad camel payload: %+v", p)
	}
}

func TestParseHookPayloadProviderTargets(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"claude_file", `{"tool_name":"Edit","tool_input":{"file_path":"internal/app.go","old_string":"a","new_string":"b"}}`, "internal/app.go"},
		{"codex_apply_patch", `{"toolName":"apply_patch","toolArgs":{"cmd":"apply_patch <<'PATCH'"}}`, "apply_patch <<'PATCH'"},
		{"gemini", `{"tool_name":"run_shell_command","tool_input":{"command":"npm publish"}}`, "npm publish"},
		{"copilot_camel", `{"toolName":"writeFile","toolArgs":{"filePath":"src/index.ts","content":"x"}}`, "src/index.ts"},
		{"plugin_mcp", `{"tool":"mcp.call","input":{"server":"fs","tool":"read","arguments":{"path":"README.md"}}}`, "fs read"},
	}
	for _, c := range cases {
		p := parseHookPayload([]byte(c.raw))
		if !strings.Contains(p.ToolTarget, c.want) {
			t.Fatalf("%s target = %q want %q", c.name, p.ToolTarget, c.want)
		}
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

	out, _, code = providerResponse("gemini", intake.Response{Decision: "deny", Reason: "no"})
	if code != 0 || !strings.Contains(out, `"decision":"deny"`) || !strings.Contains(out, `"reason":"no"`) {
		t.Fatalf("bad gemini deny: code=%d out=%s", code, out)
	}

	out, _, code = providerResponse("gemini", intake.Response{Decision: "edited", UpdatedInput: `{"command":"echo ok"}`})
	if code != 0 {
		t.Fatalf("bad gemini edit code=%d out=%s", code, out)
	}
	var gemini map[string]any
	if err := json.Unmarshal([]byte(out), &gemini); err != nil {
		t.Fatal(err)
	}
	geminiHSO := gemini["hookSpecificOutput"].(map[string]any)
	if gemini["decision"] != "allow" || geminiHSO["hookEventName"] != "BeforeTool" {
		t.Fatalf("bad gemini edit response: %s", out)
	}
	toolInput := geminiHSO["tool_input"].(map[string]any)
	if toolInput["command"] != "echo ok" {
		t.Fatalf("bad gemini tool_input: %s", out)
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
