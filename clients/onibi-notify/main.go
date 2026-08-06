// Command onibi-notify is the narrow local approval client used by Onibi's Pi extension.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
)

const maxPayloadBytes = 64 << 10

type piPayload struct {
	Version   string          `json:"version"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	CWD       string          `json:"cwd"`
	PiSession string          `json:"pi_session_id"`
}

type piLifecyclePayload struct {
	Version   string `json:"version"`
	Lifecycle string `json:"lifecycle"`
	RunID     string `json:"run_id"`
	CWD       string `json:"cwd"`
	PiSession string `json:"pi_session_id"`
}
type claudePayload struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	CWD           string          `json:"cwd"`
}

func main() {
	_ = run(os.Args[1:], os.Stdin, os.Stdout, os.Getenv)
}

func run(args []string, input io.Reader, output io.Writer, getenv func(string) string) error {
	fs := flag.NewFlagSet("onibi-notify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	typ := fs.String("type", "", "event type")
	agent := fs.String("agent", "", "agent name")
	format := fs.String("format", "", "payload format")
	response := fs.String("response", "", "response format")
	wait := fs.Bool("wait", false, "wait for an approval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *agent == "claude" && *format == "claude" {
		return runClaude(*typ, *response, *wait, input, output, getenv)
	}
	if *agent != "pi" || *format != "pi" {
		return nil
	}
	if *typ == "agent_lifecycle" {
		return sendPiLifecycle(input, getenv)
	}
	if *typ != "approval_request" || *response != "onibi-json" || !*wait {
		return nil
	}
	write := func(decision, reason string) error {
		_, err := output.Write(append(mustJSON(map[string]string{"decision": decision, "reason": reason}), '\n'))
		return err
	}
	sessionID := strings.TrimSpace(getenv("ONIBI_SESSION_ID"))
	socket := resolveSocket(getenv)
	if sessionID == "" || socket == "" {
		return write("cancelled", "Onibi session unavailable")
	}
	payload, err := parsePiPayload(io.LimitReader(input, maxPayloadBytes+1))
	if err != nil {
		return write("cancelled", "Invalid Onibi approval request")
	}
	req, err := approval.NormalizeRequest(approval.Request{SessionID: sessionID, Agent: "pi", Tool: payload.ToolName, Input: payload.ToolInput})
	if err != nil {
		return write("cancelled", "Invalid Onibi approval request")
	}
	resp, err := intake.Request(socket, intake.Event{Type: intake.TypeApprovalRequest, Session: sessionID, Agent: "pi", CWD: payload.CWD, Tool: req.Tool, InputJSON: string(req.Input), Approval: &req}, 5*time.Minute)
	if err != nil {
		return write("cancelled", "Onibi unavailable")
	}
	_, err = output.Write(append(mustJSON(piResponse(resp)), '\n'))
	return err
}

func runClaude(typ, response string, wait bool, input io.Reader, output io.Writer, getenv func(string) string) error {
	if typ == "agent_lifecycle" {
		return sendClaudeLifecycle(input, getenv)
	}
	if typ == "question_request" && response == "claude-question-json" && wait {
		return runClaudeQuestion(input, output, getenv)
	}
	if typ != "approval_request" || response != "claude-json" || !wait {
		return nil
	}
	write := func(decision, reason string) error {
		_, err := output.Write(append(mustJSON(claudeResponse(decision, reason)), '\n'))
		return err
	}
	sessionID := strings.TrimSpace(getenv("ONIBI_SESSION_ID"))
	socket := resolveSocket(getenv)
	if sessionID == "" || socket == "" {
		return write("deny", "Onibi session unavailable")
	}
	payload, err := parseClaudePayload(io.LimitReader(input, maxPayloadBytes+1))
	if err != nil {
		return write("deny", "Invalid Onibi approval request")
	}
	req, err := approval.NormalizeRequest(approval.Request{SessionID: sessionID, Agent: "claude", Tool: payload.ToolName, Input: payload.ToolInput})
	if err != nil {
		return write("deny", "Invalid Onibi approval request")
	}
	resp, err := intake.Request(socket, intake.Event{Type: intake.TypeApprovalRequest, Session: sessionID, Agent: "claude", CWD: payload.CWD, Tool: req.Tool, InputJSON: string(req.Input), Approval: &req}, 5*time.Minute)
	if err != nil {
		return write("deny", "Onibi unavailable")
	}
	return write(resp.Decision, resp.Reason)
}

func runClaudeQuestion(input io.Reader, output io.Writer, getenv func(string) string) error {
	write := func(payload claudePayload, resp intake.Response) error {
		_, err := output.Write(append(mustJSON(claudeQuestionResponse(payload.ToolInput, resp)), '\n'))
		return err
	}
	sessionID := strings.TrimSpace(getenv("ONIBI_SESSION_ID"))
	socket := resolveSocket(getenv)
	payload, err := parseClaudePayload(io.LimitReader(input, maxPayloadBytes+1))
	if err != nil || payload.HookEventName != "PreToolUse" || payload.ToolName != "AskUserQuestion" || !validClaudeQuestionInput(payload.ToolInput) {
		return write(payload, intake.Response{Decision: "cancelled", Reason: "Invalid Onibi question request"})
	}
	if sessionID == "" || socket == "" {
		return write(payload, intake.Response{Decision: "cancelled", Reason: "Onibi session unavailable"})
	}
	resp, err := intake.Request(socket, intake.Event{Type: intake.TypeClaudeQuestion, Session: sessionID, Agent: "claude", CWD: payload.CWD, Tool: payload.ToolName, InputJSON: string(payload.ToolInput)}, 11*time.Minute)
	if err != nil {
		return write(payload, intake.Response{Decision: "cancelled", Reason: "Onibi unavailable"})
	}
	return write(payload, resp)
}

func sendPiLifecycle(input io.Reader, getenv func(string) string) error {
	sessionID := strings.TrimSpace(getenv("ONIBI_SESSION_ID"))
	socket := resolveSocket(getenv)
	if sessionID == "" || socket == "" {
		return errors.New("Onibi session unavailable")
	}
	payload, err := parsePiLifecyclePayload(io.LimitReader(input, maxPayloadBytes+1))
	if err != nil {
		return err
	}
	_, err = intake.Request(socket, intake.Event{Type: intake.TypeAgentLifecycle, Session: sessionID, Agent: "pi", CWD: payload.CWD, Lifecycle: payload.Lifecycle, RunID: payload.RunID}, 2*time.Second)
	return err
}

func sendClaudeLifecycle(input io.Reader, getenv func(string) string) error {
	sessionID := strings.TrimSpace(getenv("ONIBI_SESSION_ID"))
	socket := resolveSocket(getenv)
	if sessionID == "" || socket == "" {
		return errors.New("Onibi session unavailable")
	}
	payload, err := parseClaudePayload(io.LimitReader(input, maxPayloadBytes+1))
	if err != nil {
		return err
	}
	lifecycle, err := claudeLifecycle(payload.HookEventName)
	if err != nil {
		return err
	}
	_, err = intake.Request(socket, intake.Event{Type: intake.TypeAgentLifecycle, Session: sessionID, Agent: "claude", CWD: payload.CWD, Lifecycle: lifecycle}, 2*time.Second)
	return err
}

func claudeLifecycle(hook string) (string, error) {
	switch hook {
	case "Stop":
		return "agent_end", nil
	case "StopFailure":
		return "agent_failed", nil
	default:
		return "", os.ErrInvalid
	}
}

func parsePiPayload(r io.Reader) (piPayload, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return piPayload{}, err
	}
	if len(raw) > maxPayloadBytes {
		return piPayload{}, errors.New("Pi payload exceeds 64 KiB")
	}
	var payload piPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return piPayload{}, err
	}
	payload.ToolName = strings.TrimSpace(payload.ToolName)
	if payload.Version != "onibi.pi.v1" || payload.ToolName == "" || len(payload.ToolInput) == 0 {
		return piPayload{}, os.ErrInvalid
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload.ToolInput, &object); err != nil || object == nil {
		return piPayload{}, os.ErrInvalid
	}
	return payload, nil
}

func parsePiLifecyclePayload(r io.Reader) (piLifecyclePayload, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return piLifecyclePayload{}, err
	}
	if len(raw) > maxPayloadBytes {
		return piLifecyclePayload{}, errors.New("Pi lifecycle exceeds 64 KiB")
	}
	var payload piLifecyclePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return piLifecyclePayload{}, err
	}
	payload.Lifecycle = strings.TrimSpace(payload.Lifecycle)
	payload.RunID = strings.TrimSpace(payload.RunID)
	if payload.Version != "onibi.pi.v1" || payload.RunID == "" || (payload.Lifecycle != "agent_start" && payload.Lifecycle != "agent_end") {
		return piLifecyclePayload{}, os.ErrInvalid
	}
	return payload, nil
}

func parseClaudePayload(r io.Reader) (claudePayload, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return claudePayload{}, err
	}
	if len(raw) > maxPayloadBytes {
		return claudePayload{}, errors.New("Claude payload exceeds 64 KiB")
	}
	var payload claudePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return claudePayload{}, err
	}
	payload.HookEventName = strings.TrimSpace(payload.HookEventName)
	payload.ToolName = strings.TrimSpace(payload.ToolName)
	if payload.HookEventName == "" {
		return claudePayload{}, os.ErrInvalid
	}
	if payload.HookEventName == "PermissionRequest" || payload.HookEventName == "PreToolUse" {
		if payload.ToolName == "" || len(payload.ToolInput) == 0 {
			return claudePayload{}, os.ErrInvalid
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(payload.ToolInput, &object); err != nil || object == nil {
			return claudePayload{}, os.ErrInvalid
		}
	}
	return payload, nil
}

func validClaudeQuestionInput(raw json.RawMessage) bool {
	var input struct {
		Questions []json.RawMessage `json:"questions"`
	}
	return json.Unmarshal(raw, &input) == nil && len(input.Questions) > 0
}

func piResponse(resp intake.Response) map[string]string {
	if resp.Decision == "approve" {
		return map[string]string{"decision": "approve"}
	}
	if resp.Decision == "deny" || resp.Decision == "expired" {
		return map[string]string{"decision": resp.Decision, "reason": resp.Reason}
	}
	return map[string]string{"decision": "cancelled", "reason": resp.Reason}
}

func claudeResponse(decision, reason string) map[string]any {
	output := map[string]any{"behavior": "deny", "message": firstNonEmpty(strings.TrimSpace(reason), "Denied by Onibi")}
	if decision == "approve" {
		output = map[string]any{"behavior": "allow"}
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": output}}
}

func claudeQuestionResponse(raw json.RawMessage, resp intake.Response) map[string]any {
	output := map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": firstNonEmpty(strings.TrimSpace(resp.Reason), "Onibi question unavailable")}
	if resp.Decision != "approve" || len(resp.Answers) == 0 {
		return map[string]any{"hookSpecificOutput": output}
	}
	var input struct {
		Questions json.RawMessage `json:"questions"`
	}
	if json.Unmarshal(raw, &input) != nil || len(input.Questions) == 0 {
		return map[string]any{"hookSpecificOutput": output}
	}
	output["permissionDecision"] = "allow"
	delete(output, "permissionDecisionReason")
	output["updatedInput"] = map[string]any{"questions": json.RawMessage(input.Questions), "answers": resp.Answers}
	return map[string]any{"hookSpecificOutput": output}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resolveSocket(getenv func(string) string) string {
	if socket := strings.TrimSpace(getenv("ONIBI_SOCK")); socket != "" {
		return socket
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return ""
	}
	return paths.Socket
}

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
