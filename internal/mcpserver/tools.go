package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
)

type listSessionsInput struct {
	IncludeRemote bool `json:"include_remote,omitempty"`
}

type listSessionsRow struct {
	ID                    string  `json:"id"`
	Agent                 string  `json:"agent"`
	CWD                   string  `json:"cwd"`
	StartedAt             string  `json:"started_at"`
	LastActivity          string  `json:"last_activity"`
	PendingApprovalsCount int     `json:"pending_approvals_count"`
	TokensUsed            int64   `json:"tokens_used"`
	CostUSD               float64 `json:"cost_usd"`
	RoleRequired          string  `json:"role_required"`
	Workspace             string  `json:"workspace"`
}

type killSessionInput struct {
	SessionID string `json:"session_id"`
	Force     bool   `json:"force,omitempty"`
}

type killSessionOutput struct {
	Killed bool   `json:"killed"`
	Signal string `json:"signal"`
}

type fetchTranscriptInput struct {
	SessionID string `json:"session_id"`
	SinceTurn int    `json:"since_turn,omitempty"`
	MaxTurns  int    `json:"max_turns,omitempty"`
}

type transcriptTurn struct {
	TurnIndex int                  `json:"turn_index"`
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	ToolCalls []transcriptToolCall `json:"tool_calls"`
}

type transcriptToolCall struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

var listSessionsOutputSchema = json.RawMessage(`{
  "type":"array",
  "items":{
    "type":"object",
    "properties":{
      "id":{"type":"string"},
      "agent":{"type":"string"},
      "cwd":{"type":"string"},
      "started_at":{"type":"string"},
      "last_activity":{"type":"string"},
      "pending_approvals_count":{"type":"integer"},
      "tokens_used":{"type":"integer"},
      "cost_usd":{"type":"number"},
      "role_required":{"type":"string"},
      "workspace":{"type":"string"}
    },
    "required":["id","agent","cwd","started_at","last_activity","pending_approvals_count","tokens_used","cost_usd","role_required","workspace"]
  }
}`)

var fetchTranscriptOutputSchema = json.RawMessage(`{
  "type":"array",
  "items":{
    "type":"object",
    "properties":{
      "turn_index":{"type":"integer"},
      "role":{"type":"string"},
      "content":{"type":"string"},
      "tool_calls":{
        "type":"array",
        "items":{
          "type":"object",
          "properties":{
            "id":{"type":"string"},
            "name":{"type":"string"},
            "input":{}
          }
        }
      }
    },
    "required":["turn_index","role","content","tool_calls"]
  }
}`)

func listSessionsTool() mcp.Tool {
	return mcp.NewTool("onibi_list_sessions",
		mcp.WithDescription("List Onibi sessions for MCP clients."),
		mcp.WithBoolean("include_remote", mcp.Description("include remote sessions"), mcp.DefaultBool(false)),
		mcp.WithRawOutputSchema(listSessionsOutputSchema),
	)
}

func killSessionTool() mcp.Tool {
	return mcp.NewTool("onibi_kill_session",
		mcp.WithDescription("Kill a live Onibi-controlled session through the daemon."),
		mcp.WithString("session_id", mcp.Description("session id"), mcp.Required()),
		mcp.WithBoolean("force", mcp.Description("force kill"), mcp.DefaultBool(false)),
		mcp.WithOutputSchema[killSessionOutput](),
	)
}

func fetchTranscriptTool() mcp.Tool {
	return mcp.NewTool("onibi_fetch_transcript",
		mcp.WithDescription("Fetch scrubbed transcript turns for an Onibi session."),
		mcp.WithString("session_id", mcp.Description("session id"), mcp.Required()),
		mcp.WithInteger("since_turn", mcp.Description("return turns with turn_index greater than this value"), mcp.DefaultNumber(0)),
		mcp.WithInteger("max_turns", mcp.Description("maximum turns to return"), mcp.DefaultNumber(50)),
		mcp.WithRawOutputSchema(fetchTranscriptOutputSchema),
	)
}

func (s *Server) listSessions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in listSessionsInput
	if err := req.BindArguments(&in); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	rows, err := s.listSessionRows(ctx, in)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultStructuredOnly(rows), nil
}

func (s *Server) killSession(_ context.Context, in killSessionInput) (killSessionOutput, error) {
	if strings.TrimSpace(in.SessionID) == "" {
		return killSessionOutput{}, errors.New("session_id required")
	}
	resp, err := intake.Request(s.socketPath, intake.Event{
		Type:    intake.TypeSessionControl,
		Session: in.SessionID,
		Action:  "kill",
	}, 10*time.Second)
	if err != nil {
		return killSessionOutput{}, err
	}
	if resp.Reason != "" {
		return killSessionOutput{}, errors.New(resp.Reason)
	}
	return killSessionOutput{Killed: true, Signal: "SIGKILL"}, nil
}

func (s *Server) fetchTranscript(ctx context.Context, in fetchTranscriptInput) ([]transcriptTurn, error) {
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		return nil, errors.New("session_id required")
	}
	if s.db == nil {
		return nil, errors.New("session DB unavailable")
	}
	row, ok, err := s.db.SessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("session not found")
	}
	if !strings.EqualFold(row.Agent, "claude") {
		return nil, fmt.Errorf("transcript unavailable for agent %q", row.Agent)
	}
	path, err := budget.NewClaudeParser(s.claudeBaseDir).FindTranscript(budget.SessionRef{
		SessionID: sessionID,
		Agent:     row.Agent,
		CWD:       row.CWD,
	})
	if err != nil {
		return nil, err
	}
	maxTurns := in.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 50
	}
	if maxTurns > 200 {
		maxTurns = 200
	}
	return readTranscriptTurns(path, in.SinceTurn, maxTurns)
}

func (s *Server) listSessionRows(ctx context.Context, in listSessionsInput) ([]listSessionsRow, error) {
	if s.db == nil {
		return nil, errors.New("session DB unavailable")
	}
	rows, err := s.db.SessionsRecent(ctx, 200, false)
	if err != nil {
		return nil, err
	}
	pending, err := s.pendingApprovalCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]listSessionsRow, 0, len(rows))
	for _, row := range rows {
		if !in.IncludeRemote && strings.HasPrefix(row.ID, "remote:") {
			continue
		}
		out = append(out, listSessionsRow{
			ID:                    row.ID,
			Agent:                 row.Agent,
			CWD:                   row.CWD,
			StartedAt:             formatSessionTime(row.StartedAt),
			LastActivity:          formatSessionTime(row.LastActivity),
			PendingApprovalsCount: pending[row.ID],
			TokensUsed:            0,
			CostUSD:               0,
			RoleRequired:          "owner",
			Workspace:             "",
		})
	}
	return out, nil
}

func (s *Server) pendingApprovalCounts(ctx context.Context) (map[string]int, error) {
	if s.db == nil {
		return nil, errors.New("session DB unavailable")
	}
	pending, err := approval.New(s.db, approval.DefaultTTL).Pending(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(pending))
	for _, a := range pending {
		out[a.SessionID]++
	}
	return out, nil
}

func formatSessionTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func readTranscriptTurns(path string, sinceTurn, maxTurns int) ([]transcriptTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseTranscriptTurns(f, sinceTurn, maxTurns)
}

func parseTranscriptTurns(r io.Reader, sinceTurn, maxTurns int) ([]transcriptTurn, error) {
	if maxTurns <= 0 {
		maxTurns = 50
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	out := []transcriptTurn{}
	turnIndex := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var top map[string]any
		if err := json.Unmarshal([]byte(line), &top); err != nil {
			return nil, err
		}
		role := transcriptRole(top)
		if role == "" {
			continue
		}
		turnIndex++
		if turnIndex <= sinceTurn {
			continue
		}
		turn := transcriptTurn{
			TurnIndex: turnIndex,
			Role:      role,
			Content:   approval.Scrub(transcriptContent(top)),
			ToolCalls: transcriptToolCalls(top),
		}
		out = append(out, turn)
		if len(out) >= maxTurns {
			break
		}
	}
	return out, sc.Err()
}

func transcriptRole(top map[string]any) string {
	role := firstTranscriptString(nestedTranscriptString(top, "message", "role"), transcriptString(top, "role"))
	if role != "" {
		return role
	}
	switch normalizeTranscriptType(transcriptString(top, "type")) {
	case "user", "assistant", "system":
		return normalizeTranscriptType(transcriptString(top, "type"))
	default:
		return ""
	}
}

func transcriptContent(top map[string]any) string {
	return strings.Join(transcriptContentParts(transcriptMessageContent(top)), "\n")
}

func transcriptMessageContent(top map[string]any) any {
	if msg := transcriptObject(top, "message"); msg != nil {
		if content, ok := msg["content"]; ok {
			return content
		}
	}
	return top["content"]
}

func transcriptContentParts(v any) []string {
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{x}
	case []any:
		var out []string
		for _, item := range x {
			out = append(out, transcriptContentParts(item)...)
		}
		return out
	case map[string]any:
		switch normalizeTranscriptType(transcriptString(x, "type")) {
		case "text":
			return transcriptContentParts(x["text"])
		case "tool_result":
			return transcriptContentParts(x["content"])
		case "tool_use", "tool_call":
			return nil
		default:
			if s := firstTranscriptString(transcriptString(x, "text"), transcriptString(x, "content")); s != "" {
				return []string{s}
			}
		}
	}
	return nil
}

func transcriptToolCalls(top map[string]any) []transcriptToolCall {
	blocks := transcriptContentBlocks(transcriptMessageContent(top))
	if len(blocks) == 0 {
		switch normalizeTranscriptType(transcriptString(top, "type")) {
		case "tool_use", "tool_call":
			blocks = []map[string]any{top}
		}
	}
	var out []transcriptToolCall
	for _, block := range blocks {
		switch normalizeTranscriptType(transcriptString(block, "type")) {
		case "tool_use", "tool_call":
			call := transcriptToolCall{
				ID:   firstTranscriptString(transcriptString(block, "id"), transcriptString(block, "tool_use_id"), transcriptString(block, "tool_call_id")),
				Name: firstTranscriptString(transcriptString(block, "name"), transcriptString(block, "tool"), transcriptString(block, "tool_name")),
			}
			if input, ok := block["input"]; ok {
				call.Input = scrubTranscriptValue(input)
			} else if input, ok := block["arguments"]; ok {
				call.Input = scrubTranscriptValue(input)
			}
			out = append(out, call)
		}
	}
	if out == nil {
		return []transcriptToolCall{}
	}
	return out
}

func scrubTranscriptValue(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return approval.Scrub(fmt.Sprint(v))
	}
	scrubbed := approval.Scrub(string(b))
	var out any
	if err := json.Unmarshal([]byte(scrubbed), &out); err != nil {
		return approval.Scrub(fmt.Sprint(v))
	}
	return out
}

func transcriptContentBlocks(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{x}
	default:
		return nil
	}
}

func nestedTranscriptString(m map[string]any, first, second string) string {
	if child := transcriptObject(m, first); child != nil {
		return transcriptString(child, second)
	}
	return ""
}

func transcriptObject(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func transcriptString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstTranscriptString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeTranscriptType(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), ".", "_")
}
