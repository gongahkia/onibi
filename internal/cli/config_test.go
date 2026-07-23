package cli

import (
	"strings"
	"testing"
)

func TestConfigActionFlagsSetAndGet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	executeRoot(t, "system", "config", "set", "shell.default", "zsh", "--color", "never")
	out, _ := executeRoot(t, "system", "config", "get", "shell.default", "--color", "never")
	if strings.TrimSpace(out.String()) != "zsh" {
		t.Fatalf("config get = %q", out.String())
	}
}
