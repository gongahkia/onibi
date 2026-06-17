package daemon

import (
	"context"
	"errors"
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
	lookTerminalPath = func(string) (string, error) { return "/usr/bin/ghostty", nil }
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
		`set command of cfg to "tmux attach-session -t onibi-abc"`,
		`new window with configuration cfg`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q: %s", want, script)
		}
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
	lookTerminalPath = func(string) (string, error) { return "/usr/bin/ghostty", nil }
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
	want := []string{"open", "-Fna", "Ghostty.app", "--args", "-e", "tmux", "attach-session", "-t", "onibi-abc"}
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
		if name != "osascript" {
			t.Fatalf("looked up %q", name)
		}
		return "/usr/bin/osascript", nil
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
		`create window with default profile command "tmux attach-session -t onibi-abc"`,
		`activate`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q: %s", want, script)
		}
	}
}
