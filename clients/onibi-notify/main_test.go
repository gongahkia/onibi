package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
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

	p = parseHookPayload([]byte(`{"sessionId":"event-session","provider_session_id":"pi-session","toolName":"run"}`))
	if p.ProviderSessionID != "pi-session" {
		t.Fatalf("provider session = %q", p.ProviderSessionID)
	}
	p = parseHookPayload([]byte(`{"event":"PreToolUse","session_id":"goose-session","working_dir":"/tmp/goose","tool_name":"developer__shell","tool_input":{"command":"pwd"}}`))
	if p.CWD != "/tmp/goose" || p.Tool != "developer__shell" || !strings.Contains(p.InputJSON, "pwd") {
		t.Fatalf("bad goose payload: %+v", p)
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

func TestNormalizedApprovalRequestUsesV1Model(t *testing.T) {
	p := parseHookPayload([]byte(`{"toolName":"Bash","toolArgs":{"z":1,"command":"rm -rf /tmp/x"}}`))
	req, err := normalizedApprovalRequest("s1", "codex", p)
	if err != nil {
		t.Fatal(err)
	}
	if req.Version != approval.ApprovalSchemaV1 || string(req.Input) != `{"command":"rm -rf /tmp/x","z":1}` || req.Risk.Level != approval.RiskHigh {
		t.Fatalf("request = %#v", req)
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
	out, _, code = providerResponse("copilot", intake.Response{Decision: "edited", UpdatedInput: `{"command":"echo ok"}`})
	if code != 0 {
		t.Fatalf("bad copilot edit code=%d out=%s", code, out)
	}
	var copilot map[string]any
	if err := json.Unmarshal([]byte(out), &copilot); err != nil {
		t.Fatal(err)
	}
	modifiedArgs := copilot["modifiedArgs"].(map[string]any)
	if modifiedArgs["command"] != "echo ok" {
		t.Fatalf("bad copilot modifiedArgs: %s", out)
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

	out, errOut, code = providerResponse("goose", intake.Response{Decision: "deny", Reason: "no"})
	if out != "" || code != 2 || errOut != "no\n" {
		t.Fatalf("bad goose deny: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	out, errOut, code = providerResponse("goose", intake.Response{Decision: "edited", UpdatedInput: `{"command":"echo ok"}`})
	if out != "" || code != 2 || !strings.Contains(errOut, "cannot apply edited tool input") {
		t.Fatalf("bad goose edit: code=%d stdout=%q stderr=%q", code, out, errOut)
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

func TestWaitForApprovalTimeoutReturnsCancelledAndAudits(t *testing.T) {
	old := approvalRequestTimeout
	approvalRequestTimeout = 20 * time.Millisecond
	t.Cleanup(func() { approvalRequestTimeout = old })
	sock := filepath.Join(os.TempDir(), "onibi-notify-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	t.Cleanup(func() { _ = os.Remove(sock) })
	seen := make(chan intake.Event, 1)
	srv := intake.New(sock, func(_ context.Context, ev intake.Event) error {
		seen <- ev
		return nil
	}, nil)
	srv.SetApprovalHandler(func(context.Context, intake.Event) (intake.Response, error) {
		time.Sleep(200 * time.Millisecond)
		return intake.Response{Decision: "approve"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()
	waitNotifySocket(t, sock)
	resp, err := waitForApproval(sock, intake.Event{
		Type:       intake.TypeApprovalRequest,
		Session:    "s1",
		Managed:    true,
		Tool:       "Bash",
		ToolTarget: "sleep 400",
		InputJSON:  `{"command":"sleep 400"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "cancelled" || !strings.Contains(resp.Reason, "timed out") {
		t.Fatalf("response = %#v", resp)
	}
	select {
	case ev := <-seen:
		if ev.Type != intake.TypeApprovalTimeout || ev.Session != "s1" || ev.Tool != "Bash" {
			t.Fatalf("timeout event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout audit event not sent")
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

func waitNotifySocket(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if intake.SocketActive(sock, 20*time.Millisecond) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("socket did not become active")
}
