package daemon

import (
	"context"
	"strings"
	"testing"
)

func TestLaunchTerminalNoneReturnsAttachCommand(t *testing.T) {
	msg, err := launchTerminal(context.Background(), "none", "onibi-abc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "tmux attach-session -t onibi-abc") {
		t.Fatalf("msg = %q", msg)
	}
}
