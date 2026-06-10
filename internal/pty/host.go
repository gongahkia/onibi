package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	cpty "github.com/creack/pty"
)

// DefaultRows/Cols approximates a typical terminal. PTY size matters for
// TUI agents (Claude Code) that draw based on the reported dimensions.
const (
	DefaultRows = 40
	DefaultCols = 100
)

// Host owns a child process and its controlling PTY. Construct via Spawn.
type Host struct {
	Cmd    *exec.Cmd
	Master *os.File // bidirectional PTY (read child output, write to child)

	mu     sync.Mutex
	closed bool
}

// SpawnOptions configures Spawn.
type SpawnOptions struct {
	Name string            // executable name (e.g. "claude")
	Args []string          // CLI args
	Env  []string          // KEY=VALUE env entries (in addition to os.Environ unless ReplaceEnv)
	Dir  string            // optional cwd
	Rows uint16
	Cols uint16
	// ReplaceEnv discards os.Environ and uses Env exclusively.
	ReplaceEnv bool
}

// Spawn forks `name` with args under a fresh PTY. The returned Host's
// Master is read+write; the child sees a real tty on stdin/stdout/stderr.
func Spawn(ctx context.Context, opts SpawnOptions) (*Host, error) {
	if opts.Name == "" {
		return nil, errors.New("pty: empty command name")
	}
	if opts.Rows == 0 {
		opts.Rows = DefaultRows
	}
	if opts.Cols == 0 {
		opts.Cols = DefaultCols
	}

	cmd := exec.CommandContext(ctx, opts.Name, opts.Args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if opts.ReplaceEnv {
		cmd.Env = append([]string{}, opts.Env...)
	} else {
		cmd.Env = append(os.Environ(), opts.Env...)
	}

	master, err := cpty.StartWithSize(cmd, &cpty.Winsize{Rows: opts.Rows, Cols: opts.Cols})
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}
	return &Host{Cmd: cmd, Master: master}, nil
}

// Wait blocks until the child exits and returns its error (nil on rc=0).
func (h *Host) Wait() error {
	if h.Cmd == nil {
		return errors.New("pty: not started")
	}
	return h.Cmd.Wait()
}

// Pipe copies PTY output into w until EOF or error. Typically called in a
// goroutine that fans out to the ring buffer + idle detector + (later) tmux
// mirror. The return signals when the child closes its tty (a clean exit).
func (h *Host) Pipe(w io.Writer) (int64, error) {
	return io.Copy(w, h.Master)
}

// Write sends bytes to the child's stdin (the PTY master end). Used by the
// Telegram reply path to inject text.
func (h *Host) Write(p []byte) (int, error) {
	return h.Master.Write(p)
}

// Close releases the PTY master. The child receives SIGHUP on its tty and
// typically exits shortly thereafter; callers should also call Wait or
// rely on the context to terminate the process.
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	return h.Master.Close()
}
