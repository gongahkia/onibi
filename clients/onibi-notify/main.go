// Command onibi-notify is the client invoked by agent and shell hooks. Two
// modes:
//
//  1. Fire-and-forget (default). Writes a JSON event to the daemon's local
//     Unix socket. Fails open: if the daemon is down, exit 0 silently.
//
//  2. RPC (--wait, used for approval_request). Reads the agent's tool
//     payload from stdin, sends it, blocks for the daemon's decision,
//     writes the agent-appropriate response JSON to stdout, exits with a
//     code the agent's hook system understands:
//     - approve  → exit 0
//     - edited   → exit 0 with updatedInput in stdout JSON
//     - deny     → exit 2 with reason on stderr
//     - expired  → exit 2 with "Approval expired" on stderr
//     - cancelled→ exit 0 (let the tool proceed normally — daemon
//     unavailable shouldn't block work)
//
// Identity is supplied by env vars set when the daemon spawned the agent:
//
//	ONIBI_SOCK         absolute path to intake socket (required)
//	ONIBI_SESSION_ID   stable id of this session (optional)
//
// Flags:
//
//	--type <name>         required (agent_done, agent_awaiting,
//	                      cmd_done, approval_request, ...)
//	--wait                RPC mode (currently approval_request only)
//	--status <int>        exit code (cmd_done)
//	--cmd <string>        command line (cmd_done)
//	--elapsed-ms <int>    elapsed (cmd_done)
//	--text <string>       human-readable detail
//	--tail-stdin          read up to 64 KiB of tail from stdin
package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
)

const (
	maxTailBytes    = 64 << 10
	approvalTimeout = 6 * time.Minute // approval TTL is 5min + slack
)

func main() {
	typ := flag.String("type", "", "event type")
	agent := flag.String("agent", "", "agent/provider name")
	format := flag.String("format", "", "provider input/output format")
	eventName := flag.String("event", "", "provider event name override")
	session := flag.String("session", "", "session id override")
	response := flag.String("response", "provider", "wait response format: provider or onibi-json")
	wait := flag.Bool("wait", false, "block for response (RPC mode)")
	status := flag.Int("status", 0, "exit status (cmd_done)")
	cmd := flag.String("cmd", "", "command line (cmd_done)")
	elapsedMS := flag.Int64("elapsed-ms", 0, "elapsed milliseconds")
	text := flag.String("text", "", "human-readable detail")
	tailStdin := flag.Bool("tail-stdin", false, "read tail from stdin (fire-and-forget mode)")
	flag.Parse()

	if *typ == "" && !*wait {
		os.Exit(0)
	}
	sock := resolveSocket()
	if sock == "" {
		// daemon not active — silently no-op so we don't block hooks
		os.Exit(0)
	}

	if *wait {
		runWait(sock, *typ, *agent, *format, *response)
		return
	}

	raw := readHookStdin()
	parsed := parseHookPayload(raw)
	evType := *typ
	if evType == "" {
		evType = typeForEvent(firstNonEmpty(*eventName, parsed.EventName))
	}

	// fire-and-forget event
	ev := intake.Event{
		Type:              evType,
		Session:           firstNonEmpty(*session, os.Getenv("ONIBI_SESSION_ID"), parsed.SessionID),
		Agent:             firstNonEmpty(*agent, parsed.Agent),
		PID:               os.Getppid(),
		CWD:               parsed.CWD,
		EventName:         firstNonEmpty(*eventName, parsed.EventName),
		ProviderSessionID: parsed.ProviderSessionID,
		Status:            *status,
		Cmd:               *cmd,
		Elapsed:           *elapsedMS,
		Text:              firstNonEmpty(*text, parsed.Text),
		RawJSON:           string(raw),
	}
	if *tailStdin {
		ev.Tail = string(raw)
	}
	_ = intake.Send(sock, ev)
	os.Exit(0)
}

// runWait handles RPC mode. Provider PreToolUse/BeforeTool hooks supply JSON
// on stdin. We normalize it, ask the daemon, then emit provider output.
func runWait(sock, typ, agent, format, response string) {
	if typ != "approval_request" {
		// not implemented — fail open
		os.Exit(0)
	}

	raw := readHookStdin()
	parsed := parseHookPayload(raw)
	if agent == "" {
		agent = parsed.Agent
	}
	if format == "" {
		format = agent
	}

	ev := intake.Event{
		Type:              intake.TypeApprovalRequest,
		Session:           firstNonEmpty(os.Getenv("ONIBI_SESSION_ID"), parsed.SessionID),
		Agent:             agent,
		PID:               os.Getppid(),
		CWD:               parsed.CWD,
		EventName:         parsed.EventName,
		ProviderSessionID: parsed.ProviderSessionID,
		Tool:              parsed.Tool,
		InputJSON:         parsed.InputJSON,
		RawJSON:           string(raw),
	}
	if ev.InputJSON == "" {
		ev.InputJSON = "{}"
	}

	resp, err := intake.Request(sock, ev, approvalTimeout)
	if err != nil {
		// daemon down or any other error — fail open (let the tool proceed
		// normally; the user can still cancel manually if needed). This
		// matches our fail-open contract from §1 hard rules.
		os.Exit(0)
	}

	if response == "onibi-json" {
		writeJSON(resp)
		os.Exit(0)
	}
	stdout, stderr, code := providerResponse(format, resp)
	if stdout != "" {
		_, _ = os.Stdout.WriteString(stdout)
	}
	if stderr != "" {
		_, _ = os.Stderr.WriteString(stderr)
	}
	os.Exit(code)
}

func resolveSocket() string {
	if sock := strings.TrimSpace(os.Getenv("ONIBI_SOCK")); sock != "" {
		return sock
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return ""
	}
	return paths.Socket
}

func readHookStdin() []byte {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(os.Stdin, maxTailBytes))
	return b
}

type hookPayload struct {
	Agent             string
	EventName         string
	SessionID         string
	ProviderSessionID string
	CWD               string
	Tool              string
	InputJSON         string
	Text              string
}

func parseHookPayload(raw []byte) hookPayload {
	var p hookPayload
	if len(strings.TrimSpace(string(raw))) == 0 {
		p.InputJSON = "{}"
		return p
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		p.Text = string(raw)
		p.InputJSON = "{}"
		return p
	}
	p.EventName = getString(m, "hook_event_name", "hookEventName", "eventName", "event")
	p.SessionID = getString(m, "session_id", "sessionId", "sessionID")
	p.ProviderSessionID = firstNonEmpty(p.SessionID, nestedString(m, "session", "id"))
	p.CWD = firstNonEmpty(getString(m, "cwd", "directory"), nestedString(m, "project", "root"))
	p.Tool = getString(m, "tool_name", "toolName", "tool", "name")
	input := firstValue(m, "tool_input", "toolInput", "toolArgs", "input", "args")
	if input == nil {
		input = map[string]any{}
	}
	if b, err := json.Marshal(input); err == nil {
		p.InputJSON = string(b)
	} else {
		p.InputJSON = "{}"
	}
	p.Text = summarize(m, p)
	return p
}

func providerResponse(format string, resp intake.Response) (string, string, int) {
	switch strings.ToLower(format) {
	case "claude", "codex":
		return hookSpecificResponse("PreToolUse", resp)
	case "gemini":
		return geminiResponse(resp)
	case "copilot":
		return copilotResponse(resp)
	default:
		return hookSpecificResponse("PreToolUse", resp)
	}
}

func hookSpecificResponse(event string, resp intake.Response) (string, string, int) {
	switch resp.Decision {
	case "approve":
		return marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":      event,
			"permissionDecision": "allow",
		}}), "", 0
	case "edited":
		return marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":      event,
			"permissionDecision": "allow",
			"updatedInput":       jsonObject(resp.UpdatedInput),
		}}), "", 0
	case "deny":
		reason := firstNonEmpty(resp.Reason, "denied by owner via Telegram")
		return marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            event,
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		}}), reason + "\n", 2
	case "expired":
		return marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            event,
			"permissionDecision":       "deny",
			"permissionDecisionReason": "approval expired",
		}}), "approval expired (no decision within 5 min)\n", 2
	default:
		return "", "", 0
	}
}

func geminiResponse(resp intake.Response) (string, string, int) {
	switch resp.Decision {
	case "approve":
		return "{}\n", "", 0
	case "edited":
		return marshal(map[string]any{"decision": "allow", "updatedInput": jsonObject(resp.UpdatedInput)}), "", 0
	case "deny":
		return marshal(map[string]any{"decision": "deny", "reason": firstNonEmpty(resp.Reason, "Denied by Onibi")}), "", 0
	case "expired":
		return marshal(map[string]any{"decision": "deny", "reason": "approval expired"}), "", 0
	default:
		return "", "", 0
	}
}

func copilotResponse(resp intake.Response) (string, string, int) {
	switch resp.Decision {
	case "approve":
		return marshal(map[string]any{"permissionDecision": "allow"}), "", 0
	case "edited":
		return marshal(map[string]any{"permissionDecision": "allow", "modifiedArgs": jsonObject(resp.UpdatedInput)}), "", 0
	case "deny":
		return marshal(map[string]any{"permissionDecision": "deny", "permissionDecisionReason": firstNonEmpty(resp.Reason, "Denied by Onibi")}), "", 0
	case "expired":
		return marshal(map[string]any{"permissionDecision": "deny", "permissionDecisionReason": "approval expired"}), "", 0
	default:
		return "", "", 0
	}
}

func writeJSON(v any) {
	b, _ := json.Marshal(v)
	_, _ = os.Stdout.Write(append(b, '\n'))
}

func marshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b) + "\n"
}

func jsonObject(raw string) any {
	if raw == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return map[string]any{}
	}
	return v
}

func typeForEvent(event string) string {
	e := strings.ToLower(event)
	switch e {
	case "stop", "afteragent", "agent_end", "agent.end", "session.idle", "agentstop":
		return intake.TypeAgentDone
	case "sessionend", "session_end", "session_shutdown", "session.end":
		return intake.TypeSessionExited
	default:
		return intake.TypeAgentMessage
	}
}

func summarize(m map[string]any, p hookPayload) string {
	for _, k := range []string{"message", "title", "prompt", "reason"} {
		if s := getString(m, k); s != "" {
			return s
		}
	}
	if p.Tool != "" {
		return p.EventName + " " + p.Tool
	}
	return p.EventName
}

func getString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func nestedString(m map[string]any, k1, k2 string) string {
	if inner, ok := m[k1].(map[string]any); ok {
		if s, ok := inner[k2].(string); ok {
			return s
		}
	}
	return ""
}

func firstValue(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
