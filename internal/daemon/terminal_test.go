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

func TestLaunchTerminalLinuxReturnsManualAttachWithoutLauncher(t *testing.T) {
	setTerminalGOOS(t, "linux")
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	t.Cleanup(func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
	})
	var launches int
	runTerminalCommand = func(context.Context, string, ...string) error {
		launches++
		return nil
	}
	lookTerminalPath = func(name string) (string, error) {
		if name == "tmux" {
			return "/usr/bin/tmux", nil
		}
		return "", os.ErrNotExist
	}
	msg, err := launchTerminal(context.Background(), "auto", "onibi-abc")
	if err != nil {
		t.Fatal(err)
	}
	want := "Automatic Ghostty handover is macOS-only. Run: /usr/bin/tmux attach-session -t onibi-abc"
	if msg != want || launches != 0 {
		t.Fatalf("msg=%q launches=%d", msg, launches)
	}
}

func TestLaunchTerminalMacGhosttyFailureReturnsManualAttach(t *testing.T) {
	setTerminalGOOS(t, "darwin")
	oldLook := lookTerminalPath
	oldFind := findMacAppPath
	t.Cleanup(func() {
		lookTerminalPath = oldLook
		findMacAppPath = oldFind
	})
	lookTerminalPath = func(string) (string, error) { return "", os.ErrNotExist }
	findMacAppPath = func(...string) (string, bool) { return "", false }
	_, err := launchTerminal(context.Background(), "ghostty", "onibi-abc")
	if err == nil || !strings.Contains(err.Error(), "Ghostty handover failed. Run: tmux attach-session -t onibi-abc") {
		t.Fatalf("err=%v", err)
	}
}

func TestLaunchTerminalMacGhosttyLauncherFailureReturnsManualAttach(t *testing.T) {
	setTerminalGOOS(t, "darwin")
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	t.Cleanup(func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
	})
	lookTerminalPath = func(name string) (string, error) {
		if name == "ghostty" || name == "tmux" {
			return "/usr/bin/" + name, nil
		}
		return "", os.ErrNotExist
	}
	var names []string
	runTerminalCommand = func(_ context.Context, name string, _ ...string) error {
		names = append(names, name)
		return os.ErrPermission
	}
	_, err := launchTerminal(context.Background(), "auto", "onibi-abc")
	if err == nil || !strings.Contains(err.Error(), "Ghostty handover failed. Run: /usr/bin/tmux attach-session -t onibi-abc") || strings.Join(names, ",") != "osascript,open" {
		t.Fatalf("err=%v commands=%v", err, names)
	}
}

func TestLaunchTerminalMacGhosttySuccessUsesOnlyGhostty(t *testing.T) {
	setTerminalGOOS(t, "darwin")
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	t.Cleanup(func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
	})
	lookTerminalPath = func(name string) (string, error) {
		if name == "ghostty" || name == "tmux" {
			return "/usr/bin/" + name, nil
		}
		return "", os.ErrNotExist
	}
	var names []string
	runTerminalCommand = func(_ context.Context, name string, _ ...string) error {
		names = append(names, name)
		return nil
	}
	msg, err := launchTerminal(context.Background(), "auto", "onibi-abc")
	if err != nil || msg != "Opened Ghostty for onibi-abc." || strings.Join(names, ",") != "osascript" {
		t.Fatalf("msg=%q err=%v commands=%v", msg, err, names)
	}
}

func TestProbeGhosttyLinuxReportsManualAttach(t *testing.T) {
	setTerminalGOOS(t, "linux")
	cap := ProbeGhostty(context.Background())
	if cap.Supported || cap.Installed || cap.Detail != "Ghostty handoff is macOS-only; use tmux attach manually" {
		t.Fatalf("cap=%#v", cap)
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

func setTerminalGOOS(t *testing.T, goos string) {
	t.Helper()
	old := terminalGOOS
	terminalGOOS = goos
	t.Cleanup(func() { terminalGOOS = old })
}
