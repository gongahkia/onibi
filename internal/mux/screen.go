package mux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type screenController struct {
	runner Runner
	bin    string
	config string
}

func NewScreen(bin, configPath string) Controller {
	return &screenController{runner: execRunner{}, bin: bin, config: configPath}
}

func newScreenWithRunner(bin, configPath string, runner Runner) Controller {
	return &screenController{runner: runner, bin: bin, config: configPath}
}

func (c *screenController) Kind() string { return Screen }

func (c *screenController) Start(ctx context.Context, name string, opts StartOptions) (Target, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(opts.Command) == "" {
		return Target{}, errors.New("GNU Screen session name and command required")
	}
	args := c.prefix("-dmS", name)
	args = append(args, shellCommand(opts)...)
	if _, err := c.run(ctx, args...); err != nil {
		return Target{}, err
	}
	return Target{Session: name}, nil
}

func (c *screenController) Has(ctx context.Context, target Target) (bool, error) {
	if target.Session == "" {
		return false, errors.New("GNU Screen session target required")
	}
	_, err := c.run(ctx, c.prefix("-S", target.Session, "-Q", "windows")...)
	if err == nil {
		return true, nil
	}
	if IsSessionGone(gone(err)) {
		return false, nil
	}
	return false, err
}

func (c *screenController) Capture(ctx context.Context, target Target, lines int) (string, error) {
	if target.Session == "" {
		return "", errors.New("GNU Screen session target required")
	}
	file, err := os.CreateTemp("", "onibi-screen-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	defer os.Remove(path)
	if _, err := c.run(ctx, c.prefix("-S", target.Session, "-p", "0", "-X", "hardcopy", "-h", path)...); err != nil {
		return "", gone(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return lastLines(string(b), lines), nil
}

func (c *screenController) Resize(ctx context.Context, target Target, cols, rows int) error {
	if target.Session == "" || cols < 1 || rows < 1 {
		return errors.New("GNU Screen target and dimensions required")
	}
	// `width cols lines` adjusts both dimensions of Screen's virtual terminal.
	// Keeping this one command avoids briefly advertising a mismatched size to
	// the child process between independent width and height changes.
	if _, err := c.run(ctx, c.prefix("-S", target.Session, "-p", "0", "-X", "width", strconv.Itoa(cols), strconv.Itoa(rows))...); err != nil {
		return gone(err)
	}
	return nil
}

func (c *screenController) SendText(ctx context.Context, target Target, text string, enter bool) error {
	if target.Session == "" {
		return errors.New("GNU Screen session target required")
	}
	if _, err := c.run(ctx, c.prefix("-S", target.Session, "-p", "0", "-X", "stuff", text)...); err != nil {
		return gone(err)
	}
	if !enter {
		return nil
	}
	_, err := c.run(ctx, c.prefix("-S", target.Session, "-p", "0", "-X", "stuff", "\r")...)
	return gone(err)
}

func (c *screenController) SendKey(ctx context.Context, target Target, key string) error {
	if target.Session == "" {
		return errors.New("GNU Screen session target required")
	}
	_, err := c.run(ctx, c.prefix("-S", target.Session, "-p", "0", "-X", "stuff", screenKey(key))...)
	return gone(err)
}

func (c *screenController) Kill(ctx context.Context, target Target) error {
	if target.Session == "" {
		return errors.New("GNU Screen session target required")
	}
	_, err := c.run(ctx, c.prefix("-S", target.Session, "-X", "quit")...)
	return gone(err)
}

func (c *screenController) prefix(args ...string) []string {
	if c.config == "" {
		return args
	}
	return append([]string{"-c", c.config}, args...)
}

func (c *screenController) run(ctx context.Context, args ...string) ([]byte, error) {
	runner := c.runner
	if runner == nil {
		runner = execRunner{}
	}
	bin := strings.TrimSpace(c.bin)
	if bin == "" {
		bin = Screen
	}
	out, err := runner.Run(ctx, bin, args...)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("GNU Screen executable not found (%s); set multiplexer.screen.bin or install screen: %w", bin, err)
	}
	if out = bytes.TrimSpace(out); len(out) > 0 {
		return nil, fmt.Errorf("screen %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil, fmt.Errorf("screen %s: %w", strings.Join(args, " "), err)
}

func screenKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if value, ok := map[string]string{
		"up": "\x1b[A", "down": "\x1b[B", "left": "\x1b[D", "right": "\x1b[C", "tab": "\t", "shift-tab": "\x1b[Z",
		"backspace": "\x7f", "delete": "\x1b[3~", "home": "\x1b[H", "end": "\x1b[F", "pgup": "\x1b[5~", "pgdn": "\x1b[6~",
		"esc": "\x1b", "enter": "\r", "ctrl-c": "\x03", "ctrl-d": "\x04", "ctrl-z": "\x1a", "ctrl-l": "\x0c", "ctrl-r": "\x12", "meta-enter": "\x1b\r",
		"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS", "f5": "\x1b[15~", "f6": "\x1b[17~", "f7": "\x1b[18~", "f8": "\x1b[19~", "f9": "\x1b[20~", "f10": "\x1b[21~", "f11": "\x1b[23~", "f12": "\x1b[24~",
	}[key]; ok {
		return value
	}
	if len(key) == 6 && strings.HasPrefix(key, "ctrl-") {
		return string(key[5] & 0x1f)
	}
	if len(key) == 6 && strings.HasPrefix(key, "meta-") {
		return "\x1b" + key[5:]
	}
	return key
}
