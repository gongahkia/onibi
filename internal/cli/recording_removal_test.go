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

func TestRemovedCommandSurfacesAreAbsent(t *testing.T) {
	for _, args := range [][]string{{"profile"}, {"up", "legacy"}, {"workspace"}, {"project"}, {"share"}} {
		_, _, err := executeRootAllowError(t, append(args, "--color", "never")...)
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

func TestViewerUnpairFlagsAreAbsent(t *testing.T) {
	for _, flag := range []string{"--viewer", "--all-viewers"} {
		_, _, err := executeRootAllowError(t, "unpair", flag, "device", "--color", "never")
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("flag=%s err=%v", flag, err)
		}
	}
}
