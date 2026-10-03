// Package mux provides the common control surface Onibi needs from supported
// terminal multiplexers. Each backend owns its command syntax while the daemon
// uses the same lifecycle, capture, input, key, resize, and liveness calls.
package mux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
)

const (
	Tmux   = "tmux"
	Zellij = "zellij"
	Screen = "screen"
)

var ErrSessionGone = errors.New("multiplexer session no longer exists")

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Target identifies an Onibi-owned mux session. Zellij additionally persists
// its controlled pane ID; other backends use the session name alone.
type Target struct {
	Session string
	Pane    string
}

func (t Target) String() string {
	if t.Pane == "" {
		return t.Session
	}
	return t.Session + "|" + t.Pane
}

func ParseTarget(raw string) Target {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 2)
	if len(parts) == 2 {
		return Target{Session: parts[0], Pane: parts[1]}
	}
	return Target{Session: strings.TrimSpace(raw)}
}

type StartOptions struct {
	WindowName string
	CWD        string
	Env        []string
	Command    string
	Args       []string
}

type Controller interface {
	Kind() string
	Start(context.Context, string, StartOptions) (Target, error)
	Has(context.Context, Target) (bool, error)
	Capture(context.Context, Target, int) (string, error)
	Resize(context.Context, Target, int, int) error
	SendText(context.Context, Target, string, bool) error
	SendKey(context.Context, Target, string) error
	Kill(context.Context, Target) error
}

// Open resolves a backend's executable/configuration and returns a controller.
// Config is resolved per session CWD so a checked-out .onibi.yaml can opt into
// Zellij or a project .tmux.conf without changing the user's global default.
func Open(kind string, backend config.MuxBackend, cwd string) (Controller, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case Tmux:
		configPath, err := config.ResolveTmuxConfig(cwd, backend.Config)
		if err != nil {
			return nil, err
		}
		return NewTmux(defaultBin(Tmux, backend.Bin), configPath), nil
	case Zellij:
		configPath, err := config.ResolveConfigPath(cwd, backend.Config)
		if err != nil {
			return nil, err
		}
		return NewZellij(defaultBin(Zellij, backend.Bin), configPath), nil
	case Screen:
		configPath, err := config.ResolveScreenConfig(cwd, backend.Config)
		if err != nil {
			return nil, err
		}
		return NewScreen(defaultBin(Screen, backend.Bin), configPath), nil
	default:
		return nil, fmt.Errorf("unsupported multiplexer %q", kind)
	}
}

// Choose resolves an explicit backend or chooses the first available backend
// in the stable order tmux, Zellij, GNU Screen.
func Choose(requested string, cfg config.Multiplexer, cwd string) (Controller, string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		requested = cfg.Default
	}
	if requested == "" {
		requested = "auto"
	}
	if requested != "auto" {
		backend, err := cfg.Backend(requested)
		if err != nil {
			return nil, "", err
		}
		if !available(requested, backend.Bin) {
			return nil, "", fmt.Errorf("%s executable not found; install it or set multiplexer.%s.bin", requested, requested)
		}
		ctrl, err := Open(requested, backend, cwd)
		return ctrl, requested, err
	}
	for _, kind := range []string{Tmux, Zellij, Screen} {
		backend, _ := cfg.Backend(kind)
		if !available(kind, backend.Bin) {
			continue
		}
		ctrl, err := Open(kind, backend, cwd)
		if err == nil {
			return ctrl, kind, nil
		}
		return nil, "", err
	}
	return nil, "", errors.New("no supported terminal multiplexer found; install tmux, zellij, or screen")
}

func available(kind, configured string) bool {
	bin := defaultBin(kind, configured)
	if strings.ContainsRune(bin, filepath.Separator) {
		info, err := os.Stat(bin)
		return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func defaultBin(kind, configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	if value := strings.TrimSpace(os.Getenv("ONIBI_" + strings.ToUpper(kind) + "_BIN")); value != "" {
		return value
	}
	if path, err := exec.LookPath(kind); err == nil {
		return path
	}
	return kind
}

func shellCommand(opts StartOptions) []string {
	parts := make([]string, 0, len(opts.Env)+3)
	if strings.TrimSpace(opts.CWD) != "" {
		parts = append(parts, "cd "+shellQuote(opts.CWD))
	}
	for _, env := range opts.Env {
		if strings.TrimSpace(env) == "" {
			continue
		}
		parts = append(parts, "export "+shellQuoteEnv(env))
	}
	parts = append(parts, "exec "+shellJoin(append([]string{opts.Command}, opts.Args...)...))
	return []string{"sh", "-lc", strings.Join(parts, " && ")}
}

func shellQuoteEnv(value string) string {
	if at := strings.IndexByte(value, '='); at > 0 {
		return value[:at] + "=" + shellQuote(value[at+1:])
	}
	return shellQuote(value)
}

func shellJoin(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func lastLines(text string, lines int) string {
	text = strings.TrimRight(text, "\r\n")
	if lines <= 0 {
		return text
	}
	parts := strings.Split(text, "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

func gone(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	for _, phrase := range []string{"no server running", "can't find session", "no such session", "no screen session found", "no screen to be resumed", "session not found", "not found", "no active session", "pane does not exist"} {
		if strings.Contains(text, phrase) {
			return fmt.Errorf("%w: %v", ErrSessionGone, err)
		}
	}
	return err
}

func IsSessionGone(err error) bool { return errors.Is(err, ErrSessionGone) }
