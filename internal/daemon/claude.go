package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (d *Daemon) claudeArgs(args []string) ([]string, error) {
	for _, arg := range args {
		if arg == "--bare" || arg == "--settings" || strings.HasPrefix(arg, "--settings=") {
			return nil, errors.New("Claude --bare and --settings are incompatible with Onibi hooks")
		}
	}
	notify, err := onibiNotifyPath()
	if err != nil {
		return nil, err
	}
	path, err := d.writeClaudeHooks(notify)
	if err != nil {
		return nil, err
	}
	return append(args, "--settings", path), nil
}

func onibiNotifyPath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "onibi-notify")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	path, err := exec.LookPath("onibi-notify")
	if err != nil {
		return "", errors.New("onibi-notify not found beside onibi or in PATH")
	}
	return filepath.Abs(path)
}

func (d *Daemon) writeClaudeHooks(notify string) (string, error) {
	if strings.TrimSpace(d.Paths.StateDir) == "" {
		return "", errors.New("Onibi state directory required for Claude hooks")
	}
	if err := os.MkdirAll(d.Paths.StateDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(d.Paths.StateDir, "claude-hooks.json")
	data, err := json.MarshalIndent(claudeHookSettings(notify), "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func claudeHookSettings(notify string) map[string]any {
	command := shellQuote(notify) + " --agent claude --format claude"
	return map[string]any{"hooks": map[string]any{
		"PermissionRequest": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command + " --type approval_request --wait --response claude-json", "timeout": 305}}}},
		"Stop":              []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command + " --type agent_lifecycle", "timeout": 5}}}},
		"StopFailure":       []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command + " --type agent_lifecycle", "timeout": 5}}}},
	}}
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func claudeAgentTitle(agent string) string {
	if strings.EqualFold(agent, "claude") {
		return "Claude"
	}
	return "Pi"
}

func (d *Daemon) prepareClaudeSessionArgs(agent string, args []string) ([]string, error) {
	if strings.EqualFold(agent, "claude") {
		prepared, err := d.claudeArgs(args)
		if err != nil {
			return nil, fmt.Errorf("Claude hooks: %w", err)
		}
		return prepared, nil
	}
	return args, nil
}
