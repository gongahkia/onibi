package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCommandStdioSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=TestMCPCommandStdioHelper")
	cmd.Env = append(os.Environ(),
		"ONIBI_MCP_STDIO_HELPER=1",
		"HOME="+t.TempDir(),
	)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{
		Command:           cmd,
		TerminateDuration: time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
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
