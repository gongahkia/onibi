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
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

type Controller struct {
	Runner Runner
	Bin    string
}

type Pane struct {
	ID      string
	Session string
	Window  string
	Command string
	Title   string
}

func New() *Controller { return &Controller{Runner: execRunner{}, Bin: DefaultBin()} }

func NewWithRunner(r Runner) *Controller { return &Controller{Runner: r, Bin: "tmux"} }

func DefaultBin() string {
	if v := strings.TrimSpace(os.Getenv("ONIBI_TMUX_BIN")); v != "" {
		return v
	}
	if path, err := exec.LookPath("tmux"); err == nil {
		return path
	}
	candidates := []string{
		"/opt/homebrew/bin/tmux",
		"/usr/local/bin/tmux",
		"/opt/local/bin/tmux",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".nix-profile/bin/tmux"),
			filepath.Join(home, ".local/bin/tmux"),
		)
	}
	for _, path := range candidates {
		if isExecutable(path) {
			return path
		}
	}
	return "tmux"
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0111 != 0
}

func (c *Controller) ListPanes(ctx context.Context) ([]Pane, error) {
	out, err := c.run(ctx, "list-panes", "-a", "-F", "#{pane_id}\t#{session_name}\t#{window_name}\t#{pane_current_command}\t#{pane_title}")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	panes := make([]Pane, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		for len(parts) < 5 {
			parts = append(parts, "")
		}
		panes = append(panes, Pane{
			ID:      parts[0],
			Session: parts[1],
			Window:  parts[2],
			Command: parts[3],
			Title:   parts[4],
		})
	}
	return panes, nil
}

func (c *Controller) Capture(ctx context.Context, target string, lines int) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("tmux target required")
	}
	if lines <= 0 {
		lines = 50
	}
	out, err := c.run(ctx, "capture-pane", "-p", "-e", "-t", target, "-S", "-"+strconv.Itoa(lines))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (c *Controller) SendText(ctx context.Context, target, text string, enter bool) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("tmux target required")
	}
	if _, err := c.run(ctx, "send-keys", "-t", target, "-l", "--", text); err != nil {
		return err
	}
	if enter {
		if _, err := c.run(ctx, "send-keys", "-t", target, "Enter"); err != nil {
			return err
		}
		return c.verifyMultilineSend(ctx, target, text)
	}
	return nil
}

func (c *Controller) verifyMultilineSend(ctx context.Context, target, text string) error {
	if !strings.ContainsAny(text, "\r\n") {
		return nil
	}
	want := finalNonEmptyLine(text)
	if want == "" {
		return nil
	}
	captured, err := c.Capture(ctx, target, 50)
	if err != nil {
		return nil
	}
	if strings.Contains(captured, want) {
		return nil
	}
	_, err = c.run(ctx, "send-keys", "-t", target, "Enter")
	return err
}

func finalNonEmptyLine(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func (c *Controller) SendKey(ctx context.Context, target, key string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("tmux target required")
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("tmux key required")
	}
	_, err := c.run(ctx, "send-keys", "-t", target, key)
	return err
}

func (c *Controller) KillPane(ctx context.Context, target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("tmux target required")
	}
	_, err := c.run(ctx, "kill-pane", "-t", target)
	return err
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
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("tmux executable not found (%s); set ONIBI_TMUX_BIN or install tmux in /opt/homebrew/bin or /usr/local/bin: %w", bin, err)
		}
		out = bytes.TrimSpace(out)
		if len(out) > 0 {
			return nil, fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, out)
		}
		return nil, fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
