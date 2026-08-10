package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSourcesListJSONIsClean(t *testing.T) {
	database := t.TempDir() + "/courtsg.db"
	var stdout, stderr bytes.Buffer
	exit := Execute(context.Background(), "test", []string{"--db", database, "--json", "sources", "list"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %s", exit, stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") || !strings.HasPrefix(strings.TrimSpace(stdout.String()), "[") {
		t.Fatalf("unexpected JSON output: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}
