package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/gongahkia/onibi/internal/approval"
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

func listSessionsTool() mcp.Tool {
	return mcp.NewTool("onibi_list_sessions",
		mcp.WithDescription("List Onibi sessions for MCP clients."),
		mcp.WithBoolean("include_remote", mcp.Description("include remote sessions"), mcp.DefaultBool(false)),
		mcp.WithRawOutputSchema(listSessionsOutputSchema),
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
