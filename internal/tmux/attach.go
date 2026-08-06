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
	_, err := c.run(ctx, "send-keys", "-t", target, key)
	return err
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
