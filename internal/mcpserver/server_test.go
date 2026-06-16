package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gongahkia/onibi/internal/intake"
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

func TestToolSchemasListed(t *testing.T) {
	session := connectMCPTest(t, New(Options{}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	want := map[string][]string{
		"onibi_notify":           {"session", "agent", "text"},
		"onibi_approval_request": {"session", "agent", "tool", "input_json", "timeout_seconds"},
		"onibi_session_list":     {"all", "n"},
		"onibi_session_input":    {"session", "text", "enter"},
		"onibi_session_peek":     {"session", "tail_bytes"},
	}
	for name, fields := range want {
		tool := got[name]
		if tool == nil {
			t.Fatalf("missing tool %s", name)
		}
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("%s missing schema: input=%v output=%v", name, tool.InputSchema, tool.OutputSchema)
		}
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range fields {
			if !strings.Contains(string(b), `"`+field+`"`) {
				t.Fatalf("%s schema missing %s: %s", name, field, b)
			}
		}
	}
}

func TestNotifyToolDelivers(t *testing.T) {
	got := make(chan intake.Event, 1)
	sock := startIntakeForMCPTest(t, func(_ context.Context, ev intake.Event) error {
		got <- ev
		return nil
	}, nil, nil)
	session := connectMCPTest(t, New(Options{SocketPath: sock}))

	out := callToolOK[output](t, session, "onibi_notify", map[string]any{
		"session": "s1",
		"agent":   "codex",
		"text":    "hello",
	})
	if !out.OK || out.Message != "queued" {
		t.Fatalf("output = %+v", out)
	}
	select {
	case ev := <-got:
		if ev.Type != intake.TypeAgentMessage || ev.Session != "s1" || !ev.Managed || ev.Agent != "codex" || ev.Text != "hello" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event not delivered")
	}
}

func TestNotifyToolDaemonDown(t *testing.T) {
	session := connectMCPTest(t, New(Options{SocketPath: filepath.Join(t.TempDir(), "missing.sock")}))
	out := callToolOK[output](t, session, "onibi_notify", map[string]any{"text": "hello"})
	if out.OK || !strings.Contains(out.Message, "daemon unavailable") {
		t.Fatalf("output = %+v", out)
	}
}

func TestApprovalRequestTimeout(t *testing.T) {
	sock := startIntakeForMCPTest(t, nil, func(_ context.Context, _ intake.Event) (intake.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return intake.Response{Decision: "cancelled", Reason: "approval timed out"}, nil
	}, nil)
	session := connectMCPTest(t, New(Options{SocketPath: sock}))

	out := callToolOK[approvalOutput](t, session, "onibi_approval_request", map[string]any{
		"tool":            "shell",
		"input_json":      `{"cmd":"date"}`,
		"timeout_seconds": 1,
	})
	if out.Decision != "cancelled" || out.Reason != "approval timed out" {
		t.Fatalf("output = %+v", out)
	}
}

func TestApprovalRequestApprove(t *testing.T) {
	got := make(chan intake.Event, 1)
	sock := startIntakeForMCPTest(t, nil, func(_ context.Context, ev intake.Event) (intake.Response, error) {
		got <- ev
		return intake.Response{Decision: "approve", DecidedBy: 42}, nil
	}, nil)
	session := connectMCPTest(t, New(Options{SocketPath: sock}))

	out := callToolOK[approvalOutput](t, session, "onibi_approval_request", map[string]any{
		"session":    "s1",
		"agent":      "codex",
		"tool":       "shell",
		"input_json": `{"cmd":"date"}`,
	})
	if out.Decision != "approve" || out.DecidedBy != 42 {
		t.Fatalf("output = %+v", out)
	}
	select {
	case ev := <-got:
		if ev.Type != intake.TypeApprovalRequest || ev.Session != "s1" || !ev.Managed || ev.Agent != "codex" || ev.Tool != "shell" || ev.InputJSON != `{"cmd":"date"}` {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval event not delivered")
	}
}

func TestApprovalRequestInvalidJSON(t *testing.T) {
	session := connectMCPTest(t, New(Options{}))
	res := callTool(t, session, "onibi_approval_request", map[string]any{
		"tool":       "shell",
		"input_json": `{`,
	})
	if !res.IsError || !strings.Contains(toolResultText(t, res), "input_json must be valid JSON") {
		t.Fatalf("result = %#v", res)
	}
}

func TestSessionListReadsDB(t *testing.T) {
	db := newMCPTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	if err := db.SessionUpsertStart(ctx, "active", "codex", "codex", "/tmp", "codex", "pty", "", now); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionUpsertStart(ctx, "ended", "claude", "claude", "/tmp", "claude", "pty", "", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionMarkEnded(ctx, "ended", now); err != nil {
		t.Fatal(err)
	}
	session := connectMCPTest(t, New(Options{DB: db}))

	out := callToolOK[sessionsOutput](t, session, "onibi_session_list", map[string]any{"all": true, "n": 10})
	if len(out.Sessions) != 2 {
		t.Fatalf("sessions = %+v", out.Sessions)
	}
	if out.Sessions[0].ID != "active" || out.Sessions[0].Ended {
		t.Fatalf("first session = %+v", out.Sessions[0])
	}
	if out.Sessions[1].ID != "ended" || !out.Sessions[1].Ended || out.Sessions[1].EndedAt == "" {
		t.Fatalf("second session = %+v", out.Sessions[1])
	}
}

func TestSessionListWithoutDaemon(t *testing.T) {
	db := newMCPTestDB(t)
	if err := db.SessionUpsertStart(context.Background(), "s1", "codex", "codex", "/tmp", "codex", "pty", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	session := connectMCPTest(t, New(Options{SocketPath: filepath.Join(t.TempDir(), "missing.sock"), DB: db}))

	out := callToolOK[sessionsOutput](t, session, "onibi_session_list", map[string]any{"n": 1})
	if len(out.Sessions) != 1 || out.Sessions[0].ID != "s1" {
		t.Fatalf("sessions = %+v", out.Sessions)
	}
}

func TestSessionInputDispatches(t *testing.T) {
	got := make(chan intake.Event, 1)
	sock := startIntakeForMCPTest(t, nil, nil, func(_ context.Context, ev intake.Event) (intake.Response, error) {
		got <- ev
		return intake.Response{Text: "sent"}, nil
	})
	session := connectMCPTest(t, New(Options{SocketPath: sock}))

	out := callToolOK[output](t, session, "onibi_session_input", map[string]any{
		"session": "s1",
		"text":    "hello",
		"enter":   true,
	})
	if !out.OK || out.Message != "sent" {
		t.Fatalf("output = %+v", out)
	}
	select {
	case ev := <-got:
		if ev.Type != intake.TypeSessionInput || ev.Session != "s1" || ev.Text != "hello" || !ev.Enter {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session input event not delivered")
	}
}

func TestSessionPeekReturnsTail(t *testing.T) {
	got := make(chan intake.Event, 1)
	sock := startIntakeForMCPTest(t, nil, nil, func(_ context.Context, ev intake.Event) (intake.Response, error) {
		got <- ev
		return intake.Response{Text: "tail"}, nil
	})
	session := connectMCPTest(t, New(Options{SocketPath: sock}))

	out := callToolOK[peekOutput](t, session, "onibi_session_peek", map[string]any{
		"session":    "s1",
		"tail_bytes": 4,
	})
	if out.Text != "tail" {
		t.Fatalf("output = %+v", out)
	}
	select {
	case ev := <-got:
		if ev.Type != intake.TypeSessionPeek || ev.Session != "s1" || ev.Limit != 4 {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session peek event not delivered")
	}
}

func connectMCPTest(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func startIntakeForMCPTest(t *testing.T, handler intake.Handler, approval intake.ApprovalHandler, rpc intake.RPCHandler) string {
	t.Helper()
	if handler == nil {
		handler = func(context.Context, intake.Event) error { return nil }
	}
	sock := filepath.Join(t.TempDir(), "onibi.sock")
	srv := intake.New(sock, handler, nil)
	if approval != nil {
		srv.SetApprovalHandler(approval)
	}
	if rpc != nil {
		srv.SetRPCHandler(rpc)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if intake.SocketActive(sock, 200*time.Millisecond) {
			return sock
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("intake server did not bind")
	return ""
}

func newMCPTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "d.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func callToolOK[T any](t *testing.T, session *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res := callTool(t, session, name, args)
	if res.IsError {
		t.Fatalf("tool error = %s", toolResultText(t, res))
	}
	return structuredContent[T](t, res)
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func structuredContent[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	var out T
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("structured content %s: %v", b, err)
	}
	return out
}

func toolResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	b, err := json.Marshal(res.Content)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
