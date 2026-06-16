package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/store"
)

type Options struct {
	SocketPath string
	DB         *store.DB
}

type Server struct {
	socketPath string
	db         *store.DB
}

type output struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type notifyInput struct {
	Session string `json:"session,omitempty" jsonschema:"session id"`
	Agent   string `json:"agent,omitempty" jsonschema:"agent name"`
	Text    string `json:"text" jsonschema:"message text"`
}

type approvalInput struct {
	Session        string `json:"session,omitempty" jsonschema:"session id"`
	Agent          string `json:"agent,omitempty" jsonschema:"agent name"`
	Tool           string `json:"tool" jsonschema:"tool name"`
	InputJSON      string `json:"input_json" jsonschema:"raw tool input JSON"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"wait timeout in seconds"`
}

type approvalOutput struct {
	Decision     string `json:"decision"`
	UpdatedInput string `json:"updated_input,omitempty"`
	Reason       string `json:"reason,omitempty"`
	DecidedBy    int64  `json:"decided_by,omitempty"`
}

type sessionsInput struct {
	All bool `json:"all,omitempty" jsonschema:"include ended sessions"`
	N   int  `json:"n,omitempty" jsonschema:"maximum sessions to return"`
}

type sessionRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Agent      string `json:"agent"`
	CWD        string `json:"cwd,omitempty"`
	Command    string `json:"command,omitempty"`
	Transport  string `json:"transport"`
	TmuxTarget string `json:"tmux_target,omitempty"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at,omitempty"`
	Ended      bool   `json:"ended"`
}

type sessionsOutput struct {
	Sessions []sessionRow `json:"sessions"`
}

type sessionInputInput struct {
	Session string `json:"session,omitempty" jsonschema:"session id; optional when exactly one live session exists"`
	Text    string `json:"text" jsonschema:"text to write"`
	Enter   bool   `json:"enter,omitempty" jsonschema:"append newline"`
}

type peekInput struct {
	Session   string `json:"session,omitempty" jsonschema:"session id; optional when exactly one live session exists"`
	TailBytes int    `json:"tail_bytes,omitempty" jsonschema:"maximum bytes to return"`
}

type peekOutput struct {
	Text string `json:"text"`
}

func New(opts Options) *mcp.Server {
	s := &Server{socketPath: opts.SocketPath, db: opts.DB}
	srv := mcp.NewServer(&mcp.Implementation{Name: "onibi", Version: buildinfo.Version}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "onibi_notify", Description: "Send a fail-open status message to the Onibi daemon."}, s.notify)
	mcp.AddTool(srv, &mcp.Tool{Name: "onibi_approval_request", Description: "Request owner approval through Onibi and wait for the decision."}, s.approvalRequest)
	mcp.AddTool(srv, &mcp.Tool{Name: "onibi_session_list", Description: "List Onibi sessions recorded by the daemon."}, s.sessionList)
	mcp.AddTool(srv, &mcp.Tool{Name: "onibi_session_input", Description: "Write text into a live Onibi-controlled PTY session."}, s.sessionInput)
	mcp.AddTool(srv, &mcp.Tool{Name: "onibi_session_peek", Description: "Read recent output from a live Onibi-controlled session."}, s.sessionPeek)
	return srv
}

func Run(ctx context.Context, opts Options) error {
	return New(opts).Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) notify(_ context.Context, _ *mcp.CallToolRequest, in notifyInput) (*mcp.CallToolResult, output, error) {
	if in.Text == "" {
		return nil, output{}, errors.New("text required")
	}
	err := intake.Send(s.socketPath, intake.Event{
		Type:    intake.TypeAgentMessage,
		Session: in.Session,
		Managed: in.Session != "",
		Agent:   in.Agent,
		Text:    in.Text,
	})
	if err != nil {
		return nil, output{OK: false, Message: "daemon unavailable: " + err.Error()}, nil
	}
	return nil, output{OK: true, Message: "queued"}, nil
}

func (s *Server) approvalRequest(ctx context.Context, _ *mcp.CallToolRequest, in approvalInput) (*mcp.CallToolResult, approvalOutput, error) {
	if in.Tool == "" {
		return nil, approvalOutput{}, errors.New("tool required")
	}
	if !json.Valid([]byte(in.InputJSON)) {
		return nil, approvalOutput{}, errors.New("input_json must be valid JSON")
	}
	timeout := time.Duration(in.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 6 * time.Minute
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	resp, err := intake.Request(s.socketPath, intake.Event{
		Type:      intake.TypeApprovalRequest,
		Session:   in.Session,
		Managed:   in.Session != "",
		Agent:     in.Agent,
		Tool:      in.Tool,
		InputJSON: in.InputJSON,
	}, timeout)
	if err != nil {
		return nil, approvalOutput{}, err
	}
	_ = ctx
	return nil, approvalOutput{
		Decision:     resp.Decision,
		UpdatedInput: resp.UpdatedInput,
		Reason:       resp.Reason,
		DecidedBy:    resp.DecidedBy,
	}, nil
}

func (s *Server) sessionList(ctx context.Context, _ *mcp.CallToolRequest, in sessionsInput) (*mcp.CallToolResult, sessionsOutput, error) {
	if s.db == nil {
		return nil, sessionsOutput{}, errors.New("session DB unavailable")
	}
	n := in.N
	if n <= 0 {
		n = 50
	}
	if n > 200 {
		n = 200
	}
	rows, err := s.db.SessionsRecent(ctx, n, in.All)
	if err != nil {
		return nil, sessionsOutput{}, err
	}
	out := sessionsOutput{Sessions: make([]sessionRow, 0, len(rows))}
	for _, r := range rows {
		row := sessionRow{
			ID:         r.ID,
			Name:       r.Name,
			Agent:      r.Agent,
			CWD:        r.CWD,
			Command:    r.Command,
			Transport:  r.Transport,
			TmuxTarget: r.TmuxTarget,
			StartedAt:  r.StartedAt.Format(time.RFC3339),
			Ended:      r.Ended,
		}
		if r.Ended {
			row.EndedAt = r.EndedAt.Format(time.RFC3339)
		}
		out.Sessions = append(out.Sessions, row)
	}
	return nil, out, nil
}

func (s *Server) sessionInput(_ context.Context, _ *mcp.CallToolRequest, in sessionInputInput) (*mcp.CallToolResult, output, error) {
	if in.Text == "" {
		return nil, output{}, errors.New("text required")
	}
	resp, err := intake.Request(s.socketPath, intake.Event{
		Type:    intake.TypeSessionInput,
		Session: in.Session,
		Text:    in.Text,
		Enter:   in.Enter,
	}, 10*time.Second)
	if err != nil {
		return nil, output{}, err
	}
	if resp.Reason != "" {
		return nil, output{}, errors.New(resp.Reason)
	}
	return nil, output{OK: true, Message: resp.Text}, nil
}

func (s *Server) sessionPeek(_ context.Context, _ *mcp.CallToolRequest, in peekInput) (*mcp.CallToolResult, peekOutput, error) {
	resp, err := intake.Request(s.socketPath, intake.Event{
		Type:    intake.TypeSessionPeek,
		Session: in.Session,
		Limit:   in.TailBytes,
	}, 10*time.Second)
	if err != nil {
		return nil, peekOutput{}, err
	}
	if resp.Reason != "" {
		return nil, peekOutput{}, errors.New(resp.Reason)
	}
	return nil, peekOutput{Text: resp.Text}, nil
}
