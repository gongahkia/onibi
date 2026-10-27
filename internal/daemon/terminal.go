package daemon

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gongahkia/onibi/internal/mux"
)

var terminalKeys = map[string]string{
	"up": "up", "down": "down", "left": "left", "right": "right",
	"tab": "tab", "shift-tab": "shift-tab", "backspace": "backspace", "delete": "delete",
	"home": "home", "end": "end", "pgup": "pgup", "pgdn": "pgdn",
	"esc": "esc", "enter": "enter", "ctrl-c": "ctrl-c", "ctrl-d": "ctrl-d",
	"ctrl-z": "ctrl-z", "ctrl-l": "ctrl-l", "ctrl-r": "ctrl-r", "meta-enter": "meta-enter", "space": "space",
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
	// A plain letter or digit is a single key press, for menus that select
	// with "1" or "y" and may ignore the same character delivered as a paste.
	if len(key) == 1 && ((key[0] >= 'a' && key[0] <= 'z') || (key[0] >= '0' && key[0] <= '9')) {
		return key, nil
	}
	if len(key) >= 3 && len(key) <= 10 && (strings.HasPrefix(key, "ctrl-") || strings.HasPrefix(key, "meta-")) {
		prefix, value := "ctrl-", strings.TrimPrefix(key, "ctrl-")
		if strings.HasPrefix(key, "meta-") {
			prefix, value = "meta-", strings.TrimPrefix(key, "meta-")
		}
		if len(value) == 1 && value[0] >= 'a' && value[0] <= 'z' {
			return prefix + value, nil
		}
	}
	if len(key) >= 2 && len(key) <= 4 && key[0] == 'f' {
		n, err := strconv.Atoi(key[1:])
		if err == nil && n >= 1 && n <= 12 {
			return key, nil
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
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			return 0, 0, err
		}
		if err := ctrl.Resize(ctx, mux.ParseTarget(s.TmuxTarget), dimensions[0], dimensions[1]); err != nil {
			return 0, 0, d.muxSessionError(ctx, s, err)
		}
		d.touchSession(ctx, s)
		return dimensions[0], dimensions[1], nil
	}
	ctrl, err := d.tmuxControllerForCWD(s.CWD)
	if err != nil {
		return 0, 0, err
	}
	if err := ctrl.ResizeWindow(ctx, s.TmuxTarget, dimensions[0], dimensions[1]); err != nil {
		return 0, 0, d.tmuxSessionError(ctx, s, err)
	}
	d.touchSession(ctx, s)
	return dimensions[0], dimensions[1], nil
}
