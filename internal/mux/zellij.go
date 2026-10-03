package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type zellijController struct {
	runner Runner
	bin    string
	config string
}

func NewZellij(bin, configPath string) Controller {
	return &zellijController{runner: execRunner{}, bin: bin, config: configPath}
}

func newZellijWithRunner(bin, configPath string, runner Runner) Controller {
	return &zellijController{runner: runner, bin: bin, config: configPath}
}

func (c *zellijController) Kind() string { return Zellij }

func (c *zellijController) Start(ctx context.Context, name string, opts StartOptions) (Target, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(opts.Command) == "" {
		return Target{}, errors.New("Zellij session name and command required")
	}
	args := c.prefix("attach", "--create-background", name, "--")
	args = append(args, shellCommand(opts)...)
	if _, err := c.run(ctx, args...); err != nil {
		return Target{}, err
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		panes, err := c.panes(ctx, name)
		if err != nil {
			_ = c.Kill(ctx, Target{Session: name})
			return Target{}, err
		}
		for _, pane := range panes {
			if pane.ID != "" && !pane.Exited && !pane.Plugin {
				return Target{Session: name, Pane: pane.ID}, nil
			}
		}
		select {
		case <-ctx.Done():
			_ = c.Kill(context.Background(), Target{Session: name})
			return Target{}, ctx.Err()
		case <-deadline.C:
			_ = c.Kill(context.Background(), Target{Session: name})
			return Target{}, errors.New("Zellij did not report a live terminal pane")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (c *zellijController) Has(ctx context.Context, target Target) (bool, error) {
	if target.Session == "" || target.Pane == "" {
		return false, errors.New("Zellij session and pane target required")
	}
	panes, err := c.panes(ctx, target.Session)
	if err != nil {
		if IsSessionGone(err) {
			return false, nil
		}
		return false, err
	}
	for _, pane := range panes {
		if pane.ID == target.Pane && !pane.Exited {
			return true, nil
		}
	}
	return false, nil
}

func (c *zellijController) Capture(ctx context.Context, target Target, lines int) (string, error) {
	if target.Session == "" || target.Pane == "" {
		return "", errors.New("Zellij session and pane target required")
	}
	out, err := c.run(ctx, c.sessionArgs(target.Session, "action", "dump-screen", "--pane-id", target.Pane, "--full")...)
	if err != nil {
		return "", gone(err)
	}
	return lastLines(string(out), lines), nil
}

// Zellij can resize pane splits relatively but cannot set the terminal viewport
// to an exact columns×rows preset, so Onibi intentionally refuses /size rather
// than giving the user a misleading success response.
func (c *zellijController) Resize(context.Context, Target, int, int) error {
	return errors.New("Zellij does not support exact Onibi viewport presets")
}

func (c *zellijController) SendText(ctx context.Context, target Target, text string, enter bool) error {
	if target.Session == "" || target.Pane == "" {
		return errors.New("Zellij session and pane target required")
	}
	if _, err := c.run(ctx, c.sessionArgs(target.Session, "action", "paste", "--pane-id", target.Pane, text)...); err != nil {
		return gone(err)
	}
	if !enter {
		return nil
	}
	_, err := c.run(ctx, c.sessionArgs(target.Session, "action", "send-keys", "--pane-id", target.Pane, "Enter")...)
	return gone(err)
}

func (c *zellijController) SendKey(ctx context.Context, target Target, key string) error {
	if target.Session == "" || target.Pane == "" {
		return errors.New("Zellij session and pane target required")
	}
	_, err := c.run(ctx, c.sessionArgs(target.Session, "action", "send-keys", "--pane-id", target.Pane, zellijKey(key))...)
	return gone(err)
}

func (c *zellijController) Kill(ctx context.Context, target Target) error {
	if target.Session == "" {
		return errors.New("Zellij session target required")
	}
	_, err := c.run(ctx, c.prefix("kill-session", target.Session)...)
	return gone(err)
}

type zellijPane struct {
	ID     string
	Exited bool
	Plugin bool
}

func (c *zellijController) panes(ctx context.Context, session string) ([]zellijPane, error) {
	out, err := c.run(ctx, c.sessionArgs(session, "action", "list-panes", "--json")...)
	if err != nil {
		return nil, gone(err)
	}
	var raw []struct {
		ID     json.RawMessage `json:"id"`
		Exited bool            `json:"exited"`
		Plugin bool            `json:"is_plugin"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse Zellij pane list: %w", err)
	}
	panes := make([]zellijPane, 0, len(raw))
	for _, pane := range raw {
		id := parseZellijPaneID(pane.ID)
		if id != "" {
			panes = append(panes, zellijPane{ID: id, Exited: pane.Exited, Plugin: pane.Plugin})
		}
	}
	return panes, nil
}

func parseZellijPaneID(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number int
	if json.Unmarshal(raw, &number) == nil && number >= 0 {
		return "terminal_" + strconv.Itoa(number)
	}
	return ""
}

func (c *zellijController) sessionArgs(session string, args ...string) []string {
	return c.prefix(append([]string{"--session", session}, args...)...)
}

func (c *zellijController) prefix(args ...string) []string {
	if c.config == "" {
		return args
	}
	return append([]string{"--config", c.config}, args...)
}

func (c *zellijController) run(ctx context.Context, args ...string) ([]byte, error) {
	runner := c.runner
	if runner == nil {
		runner = execRunner{}
	}
	bin := strings.TrimSpace(c.bin)
	if bin == "" {
		bin = Zellij
	}
	out, err := runner.Run(ctx, bin, args...)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("Zellij executable not found (%s); set multiplexer.zellij.bin or install zellij: %w", bin, err)
	}
	if out = bytes.TrimSpace(out); len(out) > 0 {
		return nil, fmt.Errorf("zellij %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil, fmt.Errorf("zellij %s: %w", strings.Join(args, " "), err)
}

func zellijKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if value, ok := map[string]string{
		"up": "Up", "down": "Down", "left": "Left", "right": "Right", "tab": "Tab", "shift-tab": "Shift Tab",
		"backspace": "Backspace", "delete": "Delete", "home": "Home", "end": "End", "pgup": "PageUp", "pgdn": "PageDown",
		"esc": "Esc", "enter": "Enter", "ctrl-c": "Ctrl c", "ctrl-d": "Ctrl d", "ctrl-z": "Ctrl z", "ctrl-l": "Ctrl l", "ctrl-r": "Ctrl r", "meta-enter": "Alt Enter",
	}[key]; ok {
		return value
	}
	if len(key) == 6 && strings.HasPrefix(key, "ctrl-") {
		return "Ctrl " + strings.ToUpper(key[5:])
	}
	if len(key) == 6 && strings.HasPrefix(key, "meta-") {
		return "Alt " + strings.ToUpper(key[5:])
	}
	if len(key) >= 2 && key[0] == 'f' {
		return strings.ToUpper(key)
	}
	return key
}
