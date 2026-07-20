package cli

import (
	"strings"
	"testing"
)

func TestRecordingsCommandIsAbsent(t *testing.T) {
	_, _, err := executeRootAllowError(t, "recordings", "list", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestMCPCommandIsAbsent(t *testing.T) {
	_, _, err := executeRootAllowError(t, "mcp", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected err: %v", err)
	}
}
