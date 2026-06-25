package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var errAmbiguousTarget = errors.New("ambiguous session target")

func (d *Daemon) liveSessions() []*Session {
	var out []*Session
	for _, s := range d.Registry.List() {
		if !s.Ended() {
			out = append(out, s)
		}
	}
	return out
}

func (d *Daemon) sessionByID(id string) (*Session, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrUnknownSession
	}
	var nameMatches []*Session
	for _, s := range d.liveSessions() {
		if s.ID == id || strings.HasPrefix(s.ID, id) {
			return s, nil
		}
		if s.Name == id {
			nameMatches = append(nameMatches, s)
		}
	}
	if len(nameMatches) == 1 {
		return nameMatches[0], nil
	}
	if len(nameMatches) > 1 {
		return nil, errAmbiguousTarget
	}
	return nil, ErrUnknownSession
}

func (d *Daemon) clearDefaultTargetsForSession(ctx context.Context, sessionID string) {
	_ = ctx
	_ = sessionID
}

func agentCommand(agent string, args []string) (string, string, []string, bool) {
	if agent == "shell" {
		shell := defaultInteractiveShell()
		if len(args) > 0 {
			shell = args[0]
			args = args[1:]
		}
		bin, shellArgs, ok := shellCommand(shell, args)
		return bin, "shell", shellArgs, ok
	}
	bin, ok := agentBinary(agent)
	return bin, agent, args, ok
}

func agentBinary(agent string) (string, bool) {
	defaults := map[string]string{
		"amp":      "amp",
		"claude":   "claude",
		"codex":    "codex",
		"copilot":  "copilot",
		"gemini":   "gemini",
		"goose":    "goose",
		"opencode": "opencode",
		"pi":       "pi",
	}
	bin, ok := defaults[agent]
	if !ok {
		return "", false
	}
	env := "ONIBI_" + strings.ToUpper(agent) + "_BIN"
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v, true
	}
	return bin, true
}

func defaultInteractiveShell() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		return filepath.Base(sh)
	}
	if runtime.GOOS == "darwin" {
		return "zsh"
	}
	return "bash"
}

func shellCommand(shell string, extra []string) (string, []string, bool) {
	shell = strings.ToLower(strings.TrimSpace(filepath.Base(shell)))
	var args []string
	switch shell {
	case "zsh":
		args = []string{"-i"}
	case "bash":
		args = []string{"-i"}
	case "fish":
		args = []string{"--interactive"}
	case "sh", "dash", "ash":
		args = []string{"-i"}
	case "nu", "pwsh", "powershell", "ksh", "ksh93", "mksh", "oksh", "tcsh", "csh":
	default:
		return "", nil, false
	}
	return shell, append(args, extra...), true
}

func (d *Daemon) pingText(ctx context.Context, _ time.Duration) string {
	_ = ctx
	return fmt.Sprintf("pong\nuptime=%s\nsessions=%d", time.Since(d.started).Truncate(time.Second), len(d.liveSessions()))
}
