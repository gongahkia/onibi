package approval

import (
	"strings"
	"testing"
)

func TestExtractDetailsCommandAndFileTargets(t *testing.T) {
	cases := []struct {
		tool string
		in   string
		want string
	}{
		{"Bash", `{"command":"go test ./..."}`, "go test ./..."},
		{"Write", `{"file_path":"internal/x.go","content":"x"}`, "internal/x.go"},
		{"Glob", `{"pattern":"**/*.go","path":"internal"}`, "**/*.go"},
		{"WebFetch", `{"url":"https://example.com","prompt":"summarize"}`, "https://example.com"},
		{"TodoWrite", `{"todos":[{"content":"a","status":"pending"},{"content":"b","status":"completed"}]}`, "todos: pending=1, completed=1"},
		{"mcp.tool", `{"server":"fs","tool":"read","arguments":{"path":"README.md"}}`, "fs read"},
	}
	for _, c := range cases {
		got := ExtractDetails(c.tool, c.in)
		if !strings.Contains(got.Target, c.want) {
			t.Fatalf("%s target = %q, want %q", c.tool, got.Target, c.want)
		}
	}
}
