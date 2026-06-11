package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gongahkia/onibi/internal/store"
)

func TestNewDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New panic: %v", r)
		}
	}()
	if New(Options{}) == nil {
		t.Fatal("nil server")
	}
}

func TestInMemorySessionListSmoke(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "d.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	session := connectMCPTest(t, New(Options{DB: db}))
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "onibi_session_list",
		Arguments: map[string]any{"n": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error = %#v", res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "sessions") {
		t.Fatalf("structured content = %s", b)
	}
}

func TestInMemoryNotifyMissingDaemonSmoke(t *testing.T) {
	session := connectMCPTest(t, New(Options{SocketPath: filepath.Join(t.TempDir(), "missing.sock")}))
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "onibi_notify",
		Arguments: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error = %#v", res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ok":false`) {
		t.Fatalf("structured content = %s", b)
	}
}

func connectMCPTest(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}
