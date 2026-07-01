package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPCommandStdioSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := []string{
		"ONIBI_MCP_STDIO_HELPER=1",
		"HOME=" + t.TempDir(),
	}
	client := mcpclient.NewClient(transport.NewCommandWithEnv(os.Args[0], env, "-test.run=TestMCPCommandStdioHelper"))
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "v0"}
	if _, err := client.Initialize(ctx, req); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	res, err := client.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "onibi_session_list",
		Arguments: map[string]any{"n": 1},
	}})
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

func TestMCPCommandStdioHelper(t *testing.T) {
	if os.Getenv("ONIBI_MCP_STDIO_HELPER") != "1" {
		return
	}
	root := Root()
	root.SetArgs([]string{"mcp"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
