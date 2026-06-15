package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

var runTerminalCommand = func(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

var lookTerminalPath = exec.LookPath

func (d *Daemon) launchVisibleTerminal(ctx context.Context, target string) (string, error) {
	return launchTerminal(ctx, d.TerminalDefault, target)
}

func launchTerminal(ctx context.Context, preference, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("tmux target required")
	}
	attach := "tmux attach-session -t " + target
	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "", "auto":
		if launchGhostty(ctx, target) == nil {
			return "Opened Ghostty for " + target + ".", nil
		}
		if launchTerminalApp(ctx, target) == nil {
			return "Opened Terminal.app for " + target + ".", nil
		}
		return "No terminal launcher available. Run: " + attach, nil
	case "ghostty":
		if err := launchGhostty(ctx, target); err != nil {
			return "Ghostty launch failed. Run: " + attach, err
		}
		return "Opened Ghostty for " + target + ".", nil
	case "terminal":
		if err := launchTerminalApp(ctx, target); err != nil {
			return "Terminal.app launch failed. Run: " + attach, err
		}
		return "Opened Terminal.app for " + target + ".", nil
	case "none":
		return "Run: " + attach, nil
	default:
		return "", errors.New("unsupported terminal.default")
	}
}

func launchGhostty(ctx context.Context, target string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("Ghostty auto-launch currently uses macOS open")
	}
	if _, err := lookTerminalPath("ghostty"); err != nil {
		if _, statErr := os.Stat("/Applications/Ghostty.app"); statErr != nil {
			return err
		}
	}
	return runTerminalCommand(ctx, "open", "-na", "Ghostty.app", "--args", "-e", "tmux", "attach-session", "-t", target)
}

func launchTerminalApp(ctx context.Context, target string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("Terminal.app is macOS-only")
	}
	script := `tell application "Terminal" to do script ` + appleScriptQuote("tmux attach-session -t "+target) + "\n" +
		`tell application "Terminal" to activate`
	return runTerminalCommand(ctx, "osascript", "-e", script)
}

func appleScriptQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
