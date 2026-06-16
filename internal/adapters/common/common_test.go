package common

import (
	"strings"
	"testing"
)

func TestCommandRequiresManagedSession(t *testing.T) {
	cmd := Command("/tmp/onibi-notify", "codex", "codex", "approval_request", true, "provider")
	for _, want := range []string{"ONIBI_SESSION_ID", "exec", "--agent codex", "--wait"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q: %s", want, cmd)
		}
	}
}

func TestUnguardedCommandHasNoSessionGuard(t *testing.T) {
	cmd := UnguardedCommand("/tmp/onibi-notify", "shell", "shell", "cmd_done", false, "")
	if strings.Contains(cmd, "ONIBI_SESSION_ID") {
		t.Fatalf("unguarded command = %s", cmd)
	}
	if !strings.Contains(cmd, "--agent shell") {
		t.Fatalf("command = %s", cmd)
	}
}
