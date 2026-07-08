package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/denytest"
	"github.com/gongahkia/onibi/internal/intake"
)

func TestParsePreToolUse(t *testing.T) {
	tool, input := ParsePreToolUse(strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
	if tool != "Bash" {
		t.Fatalf("tool = %q", tool)
	}
	if input != `{"command":"ls"}` {
		t.Fatalf("input = %q", input)
	}
}

func TestParsePreToolUseInvalidFallsBack(t *testing.T) {
	tool, input := ParsePreToolUse(strings.NewReader(`{`))
	if tool != "" || input != "{}" {
		t.Fatalf("got (%q, %q)", tool, input)
	}
}

func TestPreToolUseEditedOutput(t *testing.T) {
	res := PreToolUseResponse(intake.Response{
		Decision:     "edited",
		UpdatedInput: `{"command":"echo ok"}`,
	})
	if res.ExitCode != 0 || res.Stderr != "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		t.Fatal(err)
	}
	spec := out["hookSpecificOutput"].(map[string]any)
	if spec["permissionDecision"] != "allow" {
		t.Fatalf("decision = %v", spec["permissionDecision"])
	}
	updated := spec["updatedInput"].(map[string]any)
	if updated["command"] != "echo ok" {
		t.Fatalf("updated command = %v", updated["command"])
	}
}

func TestPreToolUseDeniedOutput(t *testing.T) {
	res := PreToolUseResponse(intake.Response{Decision: "deny", Reason: "no"})
	if res.ExitCode != 2 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "no") {
		t.Fatalf("stderr = %q", res.Stderr)
	}
	if !strings.Contains(res.Stdout, `"permissionDecision":"deny"`) {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestAdapterClaudeDenyBlocksTool(t *testing.T) {
	notify := denytest.DenyNotify(t)
	target := denytest.Target(t, Agent)
	h := buildEventHook(notify, eventSpec{event: "PreToolUse", typ: "approval_request", wait: true, response: "provider"})
	cmd := h["hooks"].([]any)[0].(map[string]any)["command"].(string)
	res := denytest.RunHook(t, cmd, `{"tool_name":"Bash","tool_input":{"command":"touch `+target+`"}}`)
	if res.Code == 0 || !strings.Contains(res.Stdout, `"permissionDecision":"deny"`) {
		t.Fatalf("deny hook did not block: code=%d stdout=%q stderr=%q", res.Code, res.Stdout, res.Stderr)
	}
	denytest.CreateIfAllowed(t, target, res.Code == 0)
}
