package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/gongahkia/onibi/internal/transcript"
)

const transcriptResourceTemplate = "onibi://sessions/{id}/transcript"

func registerResources(srv *server.MCPServer, s *Server) {
	srv.AddResourceTemplate(
		mcp.NewResourceTemplate(
			transcriptResourceTemplate,
			"Onibi session transcript",
			mcp.WithTemplateDescription("Scrubbed transcript turns for an Onibi session."),
			mcp.WithTemplateMIMEType("application/json"),
		),
		s.readTranscriptResource,
	)
}

func (s *Server) readTranscriptResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	sessionID := transcriptResourceSessionID(req)
	if sessionID == "" {
		return nil, errors.New("session id required")
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
		return nil, errors.New("transcript unavailable for agent " + row.Agent)
	}
	path, err := transcript.FindClaude(s.claudeBaseDir, "", row.CWD)
	if err != nil {
		return nil, err
	}
	turns, err := readTranscriptTurns(path, 0, 10000)
	if err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(turns, "", "  ")
	if err != nil {
		return nil, err
	}
	uri := req.Params.URI
	if uri == "" {
		uri = "onibi://sessions/" + url.PathEscape(sessionID) + "/transcript"
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(body),
		},
	}, nil
}

func transcriptResourceSessionID(req mcp.ReadResourceRequest) string {
	if req.Params.Arguments != nil {
		if ids, ok := req.Params.Arguments["id"].([]string); ok && len(ids) > 0 {
			return strings.TrimSpace(ids[0])
		}
		if id, ok := req.Params.Arguments["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	}
	const prefix = "onibi://sessions/"
	const suffix = "/transcript"
	uri := strings.TrimSpace(req.Params.URI)
	if !strings.HasPrefix(uri, prefix) || !strings.HasSuffix(uri, suffix) {
		return ""
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(uri, prefix), suffix)
	id, err := url.PathUnescape(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(id)
}
