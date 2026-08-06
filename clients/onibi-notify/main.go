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
	if *typ != "approval_request" || *agent != "pi" || *format != "pi" || *response != "onibi-json" || !*wait {
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

func piResponse(resp intake.Response) map[string]string {
	if resp.Decision == "approve" {
		return map[string]string{"decision": "approve"}
	}
	if resp.Decision == "deny" || resp.Decision == "expired" {
		return map[string]string{"decision": resp.Decision, "reason": resp.Reason}
	}
	return map[string]string{"decision": "cancelled", "reason": resp.Reason}
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
