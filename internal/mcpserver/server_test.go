package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gongahkia/kaypoh/internal/app"
	"github.com/gongahkia/kaypoh/internal/config"
)

func TestReadOnlyMCPServerServesSearch(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	service, err := app.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	server, err := New(service, cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Raw().Run(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kaypoh_search_availability", Arguments: map[string]any{"minimum_duration_minutes": 60}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("MCP tool error: %#v", result.Content)
	}
	cancel()
	select {
	case <-time.After(time.Second):
		t.Fatal("MCP server did not exit after cancellation")
	case <-serverDone:
	}
}
