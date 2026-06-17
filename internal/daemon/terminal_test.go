package daemon

import (
	"context"
	"errors"
	"os"
	"runtime"
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

func withTerminalStubs(t *testing.T) *[]string {
	t.Helper()
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	oldFind := findMacApp
	var calls []string
	t.Cleanup(func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
		findMacApp = oldFind
	})
	runTerminalCommand = func(_ context.Context, n string, a ...string) error {
		calls = append(calls, strings.Join(append([]string{n}, a...), "\x00"))
		return nil
	}
	lookTerminalPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	findMacApp = func(...string) bool { return true }
	return &calls
}

func TestLaunchGhosttyUsesAppleScriptWindow(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only launcher")
	}
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	oldFind := findMacApp
	defer func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
		findMacApp = oldFind
	}()
	lookTerminalPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	var name string
	var args []string
	runTerminalCommand = func(_ context.Context, n string, a ...string) error {
		name = n
		args = append([]string(nil), a...)
		return nil
	}
	if err := launchGhostty(context.Background(), "onibi-abc"); err != nil {
		t.Fatal(err)
	}
	if name != "osascript" {
		t.Fatalf("command = %q", name)
	}
	if len(args) != 2 || args[0] != "-e" {
		t.Fatalf("args = %#v", args)
	}
	script := args[1]
	for _, want := range []string{
		`new surface configuration`,
		`set command of cfg to "/usr/bin/tmux attach-session -t onibi-abc"`,
		`new window with configuration cfg`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q: %s", want, script)
		}
	}
}

func TestLaunchTerminalAutoStopsAtGhostty(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only launcher")
	}
	calls := withTerminalStubs(t)
	msg, err := launchTerminal(context.Background(), "auto", "onibi-abc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Opened Ghostty") {
		t.Fatalf("msg = %q", msg)
	}
	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "osascript\x00-e\x00tell application \"Ghostty\"") {
		t.Fatalf("calls = %#v", *calls)
	}
}

func TestLaunchTerminalAutoFallsBackToITerm2ThenTerminal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only launcher")
	}
	tests := []struct {
		name       string
		failGhost  bool
		failITerm  bool
		wantMsg    string
		wantScript string
	}{
		{"iterm", true, false, "Opened iTerm2", `tell application "iTerm2"`},
		{"terminal", true, true, "Opened Terminal.app", `tell application "Terminal"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldRun := runTerminalCommand
			oldLook := lookTerminalPath
			oldFind := findMacApp
			t.Cleanup(func() {
				runTerminalCommand = oldRun
				lookTerminalPath = oldLook
				findMacApp = oldFind
			})
			lookTerminalPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
			findMacApp = func(...string) bool { return true }
			var scripts []string
			runTerminalCommand = func(_ context.Context, n string, a ...string) error {
				if n == "osascript" && len(a) == 2 {
					scripts = append(scripts, a[1])
					if strings.Contains(a[1], `tell application "Ghostty"`) && tt.failGhost {
						return errors.New("ghostty failed")
					}
					if strings.Contains(a[1], `tell application "iTerm2"`) && tt.failITerm {
						return errors.New("iterm failed")
					}
				}
				if n == "open" && tt.failGhost {
					return errors.New("ghostty open failed")
				}
				return nil
			}
			msg, err := launchTerminal(context.Background(), "auto", "onibi-abc")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(msg, tt.wantMsg) {
				t.Fatalf("msg = %q", msg)
			}
			if len(scripts) == 0 || !strings.Contains(scripts[len(scripts)-1], tt.wantScript) {
				t.Fatalf("scripts = %#v", scripts)
			}
		})
	}
}

func TestLaunchTerminalAutoPrintsManualCommandWhenAllLaunchersFail(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only launcher")
	}
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	oldFind := findMacApp
	t.Cleanup(func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
		findMacApp = oldFind
	})
	lookTerminalPath = func(string) (string, error) { return "", os.ErrNotExist }
	findMacApp = func(...string) bool { return false }
	runTerminalCommand = func(context.Context, string, ...string) error { return errors.New("nope") }
	msg, err := launchTerminal(context.Background(), "auto", "onibi-abc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "No terminal launcher available") || !strings.Contains(msg, "tmux attach-session -t onibi-abc") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestLaunchTerminalRejectsEmptyTargetAndUnsupportedPreference(t *testing.T) {
	if _, err := launchTerminal(context.Background(), "none", " "); err == nil {
		t.Fatal("expected empty target error")
	}
	if _, err := launchTerminal(context.Background(), "xterm", "onibi-abc"); err == nil {
		t.Fatal("expected unsupported terminal.default error")
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

func TestLaunchGhosttyFallsBackToFreshOpen(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only launcher")
	}
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	oldFind := findMacApp
	defer func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
		findMacApp = oldFind
	}()
	lookTerminalPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	var calls [][]string
	runTerminalCommand = func(_ context.Context, n string, a ...string) error {
		call := append([]string{n}, a...)
		calls = append(calls, call)
		if n == "osascript" {
			return errors.New("automation denied")
		}
		return nil
	}
	if err := launchGhostty(context.Background(), "onibi-abc"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
	want := []string{"open", "-Fna", "Ghostty.app", "--args", "-e", "/usr/bin/tmux", "attach-session", "-t", "onibi-abc"}
	if strings.Join(calls[1], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("fallback = %#v", calls[1])
	}
}

func TestLaunchITerm2UsesAppleScriptWindow(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only launcher")
	}
	oldRun := runTerminalCommand
	oldLook := lookTerminalPath
	oldFind := findMacApp
	defer func() {
		runTerminalCommand = oldRun
		lookTerminalPath = oldLook
		findMacApp = oldFind
	}()
	lookTerminalPath = func(name string) (string, error) {
		if name != "osascript" && name != "tmux" {
			t.Fatalf("looked up %q", name)
		}
		return "/usr/bin/" + name, nil
	}
	findMacApp = func(names ...string) bool {
		if strings.Join(names, ",") != "iTerm.app,iTerm2.app" {
			t.Fatalf("app names = %#v", names)
		}
		return true
	}
	var name string
	var args []string
	runTerminalCommand = func(_ context.Context, n string, a ...string) error {
		name = n
		args = append([]string(nil), a...)
		return nil
	}
	msg, err := launchTerminal(context.Background(), "iterm2", "onibi-abc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Opened iTerm2") {
		t.Fatalf("msg = %q", msg)
	}
	if name != "osascript" || len(args) != 2 || args[0] != "-e" {
		t.Fatalf("command = %q %#v", name, args)
	}
	script := args[1]
	for _, want := range []string{
		`tell application "iTerm2"`,
		`create window with default profile command "/usr/bin/tmux attach-session -t onibi-abc"`,
		`activate`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q: %s", want, script)
		}
	}
}
