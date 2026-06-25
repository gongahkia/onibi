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
var findMacApp = macAppExists

func (d *Daemon) launchVisibleTerminal(ctx context.Context, target string) (string, error) {
	return launchTerminal(ctx, d.TerminalDefault, target)
}

func launchTerminal(ctx context.Context, preference, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("tmux target required")
	}
	attach := tmuxAttachShell(target)
	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "", "auto":
		if launchGhostty(ctx, target) == nil {
			return "Opened Ghostty for " + target + ".", nil
		}
		if launchITerm2(ctx, target) == nil {
			return "Opened iTerm2 for " + target + ".", nil
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
	case "iterm", "iterm2":
		if err := launchITerm2(ctx, target); err != nil {
			return "iTerm2 launch failed. Run: " + attach, err
		}
		return "Opened iTerm2 for " + target + ".", nil
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
		return errors.New("ghostty auto-launch is macOS-only")
	}
	if _, err := lookTerminalPath("ghostty"); err != nil {
		if _, statErr := os.Stat("/Applications/Ghostty.app"); statErr != nil {
			return err
		}
	}
	if err := launchGhosttyAppleScript(ctx, target); err == nil {
		return nil
	}
	return launchGhosttyFresh(ctx, target)
}

func launchGhosttyAppleScript(ctx context.Context, target string) error {
	script := `tell application "Ghostty"` + "\n" +
		`set cfg to new surface configuration` + "\n" +
		`set command of cfg to ` + appleScriptQuote(tmuxAttachShell(target)) + "\n" +
		`set wait after command of cfg to true` + "\n" +
		`set win to new window with configuration cfg` + "\n" +
		`activate` + "\n" +
		`end tell`
	return runTerminalCommand(ctx, "osascript", "-e", script)
}

func launchGhosttyFresh(ctx context.Context, target string) error {
	args := append([]string{"-Fna", "Ghostty.app", "--args", "-e"}, tmuxAttachArgs(target)...)
	return runTerminalCommand(ctx, "open", args...)
}

func launchITerm2(ctx context.Context, target string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("iTerm2 auto-launch is macOS-only")
	}
	if _, err := lookTerminalPath("osascript"); err != nil {
		return err
	}
	if !findMacApp("iTerm.app", "iTerm2.app") {
		return errors.New("iTerm2 not found")
	}
	script := `tell application "iTerm2"` + "\n" +
		`create window with default profile command ` + appleScriptQuote(tmuxAttachShell(target)) + "\n" +
		`activate` + "\n" +
		`end tell`
	return runTerminalCommand(ctx, "osascript", "-e", script)
}

func launchTerminalApp(ctx context.Context, target string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("terminal.app is macOS-only")
	}
	if _, err := lookTerminalPath("osascript"); err != nil {
		return err
	}
	script := `tell application "Terminal" to do script ` + appleScriptQuote(tmuxAttachShell(target)) + "\n" +
		`tell application "Terminal" to activate`
	return runTerminalCommand(ctx, "osascript", "-e", script)
}

func tmuxAttachArgs(target string) []string {
	return []string{tmuxPath(), "attach-session", "-t", target}
}

func tmuxAttachShell(target string) string {
	return shellQuote(tmuxPath()) + " attach-session -t " + shellQuote(target)
}

func tmuxPath() string {
	if path, err := lookTerminalPath("tmux"); err == nil && strings.TrimSpace(path) != "" {
		return path
	}
	return "tmux"
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '_' || r == '@' || r == '%' || r == '+' || r == '=' || r == ':' || r == ',' || r == '.' || r == '/' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func macAppExists(names ...string) bool {
	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, home+"/Applications")
	}
	for _, root := range roots {
		for _, name := range names {
			if _, err := os.Stat(root + "/" + name); err == nil {
				return true
			}
		}
	}
	return false
}

func appleScriptQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
