package mux

import (
	"context"
	"strings"

	internalTmux "github.com/gongahkia/onibi/internal/tmux"
)

type tmuxController struct{ control *internalTmux.Controller }

func NewTmux(bin, configPath string) Controller {
	return &tmuxController{control: internalTmux.NewWithOptions(bin, configPath)}
}

func newTmuxWithRunner(bin, configPath string, runner Runner) Controller {
	return &tmuxController{control: &internalTmux.Controller{Bin: bin, Config: configPath, Runner: runner}}
}

func (c *tmuxController) Kind() string { return Tmux }

func (c *tmuxController) Start(ctx context.Context, name string, opts StartOptions) (Target, error) {
	err := c.control.StartSession(ctx, name, internalTmux.StartOptions{WindowName: opts.WindowName, CWD: opts.CWD, Env: opts.Env, Command: opts.Command, Args: opts.Args})
	if err != nil {
		return Target{}, err
	}
	return Target{Session: name}, nil
}

func (c *tmuxController) Has(ctx context.Context, target Target) (bool, error) {
	return c.control.HasSession(ctx, target.Session)
}

func (c *tmuxController) Capture(ctx context.Context, target Target, lines int) (string, error) {
	return c.control.Capture(ctx, target.Session, lines)
}

func (c *tmuxController) Resize(ctx context.Context, target Target, cols, rows int) error {
	return c.control.ResizeWindow(ctx, target.Session, cols, rows)
}

func (c *tmuxController) SendText(ctx context.Context, target Target, text string, enter bool) error {
	return c.control.SendText(ctx, target.Session, text, enter)
}

func (c *tmuxController) SendKey(ctx context.Context, target Target, key string) error {
	return c.control.SendKey(ctx, target.Session, tmuxKey(key))
}

func (c *tmuxController) Kill(ctx context.Context, target Target) error {
	return c.control.KillSession(ctx, target.Session)
}

func tmuxKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if value, ok := map[string]string{
		"up": "Up", "down": "Down", "left": "Left", "right": "Right", "tab": "Tab", "shift-tab": "BTab",
		"backspace": "BSpace", "delete": "DC", "home": "Home", "end": "End", "pgup": "PPage", "pgdn": "NPage",
		"esc": "Escape", "enter": "Enter", "ctrl-c": "C-c", "ctrl-d": "C-d", "ctrl-z": "C-z", "ctrl-l": "C-l", "ctrl-r": "C-r", "meta-enter": "M-Enter",
	}[key]; ok {
		return value
	}
	if len(key) == 6 && strings.HasPrefix(key, "ctrl-") {
		return "C-" + key[5:]
	}
	if len(key) == 6 && strings.HasPrefix(key, "meta-") {
		return "M-" + key[5:]
	}
	if len(key) >= 2 && key[0] == 'f' {
		return strings.ToUpper(key)
	}
	return key
}
