package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Controller struct {
	Runner Runner
	Bin    string
	Config string
}

type Session struct{ Name string }

type StartOptions struct {
	WindowName string
	CWD        string
	Env        []string
	Command    string
	Args       []string
}

func New() *Controller                   { return &Controller{Runner: execRunner{}, Bin: DefaultBin()} }
func NewWithRunner(r Runner) *Controller { return &Controller{Runner: r, Bin: "tmux"} }
func NewWithOptions(bin, configPath string) *Controller {
	if strings.TrimSpace(bin) == "" {
		bin = DefaultBin()
	}
	return &Controller{Runner: execRunner{}, Bin: bin, Config: strings.TrimSpace(configPath)}
}

func DefaultBin() string {
	if v := strings.TrimSpace(os.Getenv("ONIBI_TMUX_BIN")); v != "" {
		return v
	}
	if path, err := exec.LookPath("tmux"); err == nil {
		return path
	}
	candidates := []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/opt/local/bin/tmux"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".nix-profile/bin/tmux"), filepath.Join(home, ".local/bin/tmux"))
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return "tmux"
}

func (c *Controller) ListSessions(ctx context.Context) ([]Session, error) {
	out, err := c.run(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var sessions []Session
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !seen[name] {
			seen[name] = true
			sessions = append(sessions, Session{Name: name})
		}
	}
	return sessions, nil
}

func (c *Controller) HasSession(ctx context.Context, target string) (bool, error) {
	if strings.TrimSpace(target) == "" {
		return false, errors.New("tmux target required")
	}
	_, err := c.run(ctx, "has-session", "-t", target)
	if err == nil {
		return true, nil
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "no server running") || strings.Contains(text, "can't find session") || strings.Contains(text, "no such session") {
		return false, nil
	}
	return false, err
}

func (c *Controller) PaneSize(ctx context.Context, target string) (int, int, error) {
	if strings.TrimSpace(target) == "" {
		return 0, 0, errors.New("tmux target required")
	}
	out, err := c.run(ctx, "display-message", "-p", "-t", target, "#{pane_width} #{pane_height}")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0, errors.New("tmux pane dimensions unavailable")
	}
	cols, err := strconv.Atoi(fields[0])
	if err != nil || cols < 1 {
		return 0, 0, errors.New("tmux pane width unavailable")
	}
	rows, err := strconv.Atoi(fields[1])
	if err != nil || rows < 1 {
		return 0, 0, errors.New("tmux pane height unavailable")
	}
	return cols, rows, nil
}

func (c *Controller) ResizeWindow(ctx context.Context, target string, cols, rows int) error {
	if strings.TrimSpace(target) == "" || cols < 1 || rows < 1 {
		return errors.New("tmux target and dimensions required")
	}
	_, err := c.run(ctx, "resize-window", "-t", target, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows))
	return err
}

func (c *Controller) Capture(ctx context.Context, target string, lines int) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("tmux target required")
	}
	if lines <= 0 {
		lines = 50
	}
	out, err := c.run(ctx, "capture-pane", "-e", "-p", "-t", target, "-S", "-"+strconv.Itoa(lines))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (c *Controller) StartSession(ctx context.Context, target string, opts StartOptions) error {
	if strings.TrimSpace(target) == "" || strings.TrimSpace(opts.Command) == "" {
		return errors.New("tmux target and command required")
	}
	args := []string{"new-session", "-d", "-s", target}
	if strings.TrimSpace(opts.WindowName) != "" {
		args = append(args, "-n", opts.WindowName)
	}
	if strings.TrimSpace(opts.CWD) != "" {
		args = append(args, "-c", opts.CWD)
	}
	for _, env := range opts.Env {
		if strings.TrimSpace(env) != "" {
			args = append(args, "-e", env)
		}
	}
	args = append(args, "sh", "-lc", "exec "+shellJoin(append([]string{opts.Command}, opts.Args...)...))
	_, err := c.run(ctx, args...)
	return err
}

func (c *Controller) SendText(ctx context.Context, target, text string, enter bool) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("tmux target required")
	}
	if _, err := c.run(ctx, "send-keys", "-t", target, "-l", "--", text); err != nil {
		return err
	}
	if !enter {
		return nil
	}
	_, err := c.run(ctx, "send-keys", "-t", target, "Enter")
	return err
}

func (c *Controller) SendKey(ctx context.Context, target, key string) error {
	if strings.TrimSpace(target) == "" || strings.TrimSpace(key) == "" {
		return errors.New("tmux target and key required")
	}
	key = normalizeKey(key)
	_, err := c.run(ctx, "send-keys", "-t", target, key)
	return err
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	// Keep the historic tmux spellings accepted by the public controller API.
	// The daemon now uses backend-neutral spellings (for example, "ctrl-c"),
	// but callers that already use tmux's "C-c" / "M-x" notation should not
	// have their modifier prefix lower-cased.
	if strings.HasPrefix(key, "C-") || strings.HasPrefix(key, "M-") {
		return key
	}
	key = strings.ToLower(key)
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

func (c *Controller) KillSession(ctx context.Context, target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("tmux target required")
	}
	_, err := c.run(ctx, "kill-session", "-t", target)
	return err
}

func shellJoin(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, "'"+strings.ReplaceAll(part, "'", "'\\''")+"'")
	}
	return strings.Join(quoted, " ")
}

func (c *Controller) run(ctx context.Context, args ...string) ([]byte, error) {
	if c == nil {
		return nil, errors.New("tmux controller nil")
	}
	r := c.Runner
	if r == nil {
		r = execRunner{}
	}
	bin := c.Bin
	if bin == "" {
		bin = "tmux"
	}
	if configPath := strings.TrimSpace(c.Config); configPath != "" {
		args = append([]string{"-f", configPath}, args...)
	}
	out, err := r.Run(ctx, bin, args...)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("tmux executable not found (%s); set ONIBI_TMUX_BIN or install tmux: %w", bin, err)
	}
	if out = bytes.TrimSpace(out); len(out) > 0 {
		return nil, fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil, fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
}
