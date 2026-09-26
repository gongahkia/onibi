package daemon

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

var terminalKeys = map[string]string{
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"tab": "Tab", "shift-tab": "BTab", "backspace": "BSpace", "delete": "DC",
	"home": "Home", "end": "End", "pgup": "PPage", "pgdn": "NPage",
	"esc": "Escape", "enter": "Enter", "ctrl-c": "C-c", "ctrl-d": "C-d",
	"ctrl-z": "C-z", "ctrl-l": "C-l", "ctrl-r": "C-r", "meta-enter": "M-Enter",
}

var terminalSizes = map[string][2]int{
	"small":  {80, 24},
	"medium": {100, 30},
	"large":  {120, 40},
}

func terminalKey(input string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(input))
	if value, ok := terminalKeys[key]; ok {
		return value, nil
	}
	if len(key) >= 3 && len(key) <= 10 && (strings.HasPrefix(key, "ctrl-") || strings.HasPrefix(key, "meta-")) {
		prefix, value := "C-", strings.TrimPrefix(key, "ctrl-")
		if strings.HasPrefix(key, "meta-") {
			prefix, value = "M-", strings.TrimPrefix(key, "meta-")
		}
		if len(value) == 1 && value[0] >= 'a' && value[0] <= 'z' {
			return prefix + value, nil
		}
	}
	if len(key) >= 2 && len(key) <= 4 && key[0] == 'f' {
		n, err := strconv.Atoi(key[1:])
		if err == nil && n >= 1 && n <= 12 {
			return strings.ToUpper(key), nil
		}
	}
	return "", errors.New("unsupported key; use /keys")
}

func (d *Daemon) ResizeSession(ctx context.Context, id, size string) (int, int, error) {
	dimensions, ok := terminalSizes[strings.ToLower(strings.TrimSpace(size))]
	if !ok {
		return 0, 0, errors.New("size must be small, medium, or large")
	}
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return 0, 0, err
	}
	if s.Transport != "tmux" {
		return 0, 0, errors.New("Codex app-server sessions cannot resize")
	}
	if err := newTmuxController().ResizeWindow(ctx, s.TmuxTarget, dimensions[0], dimensions[1]); err != nil {
		return 0, 0, d.tmuxSessionError(ctx, s, err)
	}
	d.touchSession(ctx, s)
	return dimensions[0], dimensions[1], nil
}
