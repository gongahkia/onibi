package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (d *Daemon) claudeArgs(args []string) ([]string, error) {
	if err := validateClaudeArgs(args); err != nil {
		return nil, err
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
	d.claudeMu.Lock()
	defer d.claudeMu.Unlock()
	if strings.TrimSpace(d.Paths.StateDir) == "" {
		return "", errors.New("Onibi state directory required for Claude hooks")
	}
	if err := os.MkdirAll(d.Paths.StateDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(d.Paths.StateDir, "claude-hooks.json")
	data, err := json.MarshalIndent(claudeHookSettings(notify, d.ClaudeQuestionTimeout), "", "  ")
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

func claudeHookSettings(notify string, questionTimeout time.Duration) map[string]any {
	command := shellQuote(notify) + " --agent claude --format claude"
	if questionTimeout <= 0 {
		questionTimeout = 3 * time.Minute
	}
	questionSeconds := int(questionTimeout.Round(time.Second).Seconds()) + 5
	return map[string]any{"hooks": map[string]any{
		"PermissionRequest": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command + " --type approval_request --wait --response claude-json", "timeout": 305}}}},
		"PreToolUse":        []any{map[string]any{"matcher": "AskUserQuestion", "hooks": []any{map[string]any{"type": "command", "command": command + " --type question_request --wait --response claude-question-json", "timeout": questionSeconds}}}},
		"Stop":              []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command + " --type agent_lifecycle", "timeout": 5}}}},
		"StopFailure":       []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command + " --type agent_lifecycle", "timeout": 5}}}},
	}}
}

func validateClaudeArgs(args []string) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--bare" || arg == "--settings" || strings.HasPrefix(arg, "--settings=") {
			return errors.New("Claude --bare and --settings are incompatible with Onibi hooks")
		}
		if arg == "--dangerously-skip-permissions" || strings.HasPrefix(arg, "--dangerously-skip-permissions=") || arg == "--allow-dangerously-skip-permissions" || strings.HasPrefix(arg, "--allow-dangerously-skip-permissions=") {
			return errors.New("Claude permission-bypass flags are incompatible with Onibi")
		}
		value := ""
		if arg == "--permission-mode" {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				value = args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "--permission-mode=") {
			value = strings.TrimPrefix(arg, "--permission-mode=")
		}
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "bypasspermissions", "dontask":
			return errors.New("Claude permission mode " + value + " is incompatible with Onibi")
		}
	}
	return nil
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
