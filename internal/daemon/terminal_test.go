package daemon

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLaunchTerminalNoneReturnsAttachCommand(t *testing.T) {
	msg, err := launchTerminal(context.Background(), "none", "onibi-abc")
	if err != nil || !strings.Contains(msg, "tmux attach-session -t onibi-abc") {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
}

func TestLaunchTerminalRejectsNonGhosttyPreferences(t *testing.T) {
	for _, preference := range []string{"iterm", "iterm2", "terminal", "xterm"} {
		t.Run(preference, func(t *testing.T) {
			if _, err := launchTerminal(context.Background(), preference, "onibi-abc"); err == nil {
				t.Fatal("expected unsupported terminal preference")
			}
		})
	}
}

func TestTmuxAttachShellUsesAbsolutePathAndQuotesUnsafeTarget(t *testing.T) {
	oldLook := lookTerminalPath
	t.Cleanup(func() { lookTerminalPath = oldLook })
	lookTerminalPath = func(name string) (string, error) {
		if name != "tmux" {
			return "", os.ErrNotExist
		}
		return "/opt/homebrew/bin/tmux", nil
	}
	got := tmuxAttachShell("onibi weird'target")
	want := `/opt/homebrew/bin/tmux attach-session -t 'onibi weird'\''target'`
	if got != want {
		t.Fatalf("attach = %q want %q", got, want)
	}
}

func TestTmuxWebAttachArgsEnableSixel(t *testing.T) {
	oldLook := lookTerminalPath
	t.Cleanup(func() { lookTerminalPath = oldLook })
	lookTerminalPath = func(name string) (string, error) { return "/opt/homebrew/bin/tmux", nil }
	if got := strings.Join(tmuxWebAttachArgs("onibi-abc"), " "); got != "/opt/homebrew/bin/tmux -T RGB,sixel attach-session -t onibi-abc" {
		t.Fatalf("attach = %q", got)
	}
}
