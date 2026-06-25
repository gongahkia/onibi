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

// phase 01 design: Spawn owns the sole PTY read loop, feeds Hub, and exposes
// Subscribe so slow consumers cannot block the master reader; Hub coalesces
// output, keeps replay bytes, and injects drop/resize control frames.

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

	WriteFunc func([]byte) (int, error)
	CloseFunc func() error
	WaitFunc  func() error

	hub    *Hub
	mu     sync.Mutex
	closed bool
}

// SpawnOptions configures Spawn.
type SpawnOptions struct {
	Name  string   // executable name (e.g. "claude")
	Args  []string // CLI args
	Argv0 string   // optional argv[0] override for login-shell emulation
	Env   []string // KEY=VALUE env entries (in addition to os.Environ unless ReplaceEnv)
	Dir   string   // optional cwd
	Rows  uint16
	Cols  uint16
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
	if opts.Argv0 != "" {
		cmd.Args[0] = opts.Argv0
	}
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
	h := &Host{Cmd: cmd, Master: master, hub: NewHub(DefaultRingSize)}
	go h.readMaster()
	return h, nil
}

func NewVirtualHost(write func([]byte) (int, error), close func() error, wait func() error) *Host {
	return &Host{WriteFunc: write, CloseFunc: close, WaitFunc: wait}
}

func (h *Host) readMaster() {
	buf := make([]byte, 4096)
	for {
		n, err := h.Master.Read(buf)
		if n > 0 && h.hub != nil {
			_, _ = h.hub.Write(buf[:n])
		}
		if err != nil {
			if h.hub != nil {
				h.hub.Close()
			}
			return
		}
	}
}

// Wait blocks until the child exits and returns its error (nil on rc=0).
func (h *Host) Wait() error {
	if h.WaitFunc != nil {
		return h.WaitFunc()
	}
	if h.Cmd == nil {
		return errors.New("pty: not started")
	}
	return h.Cmd.Wait()
}

// Pipe copies PTY output into w until EOF or error. Typically called in a
// goroutine that fans out to the ring buffer + idle detector + (later) tmux
// mirror. The return signals when the child closes its tty (a clean exit).
func (h *Host) Pipe(w io.Writer) (int64, error) {
	_, ch, unsub := h.Subscribe(context.Background(), DefaultSubscriberBuffer)
	defer unsub()
	var written int64
	for p := range ch {
		n, err := w.Write(p)
		written += int64(n)
		if err != nil {
			return written, err
		}
		if n != len(p) {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (h *Host) Subscribe(ctx context.Context, bufCap int) (uint64, <-chan []byte, func()) {
	if h == nil || h.hub == nil {
		ch := make(chan []byte)
		close(ch)
		return 0, ch, func() {}
	}
	return h.hub.Subscribe(ctx, bufCap)
}

func (h *Host) SubscribeLive(ctx context.Context, bufCap int) (uint64, <-chan []byte, func()) {
	if h == nil || h.hub == nil {
		ch := make(chan []byte)
		close(ch)
		return 0, ch, func() {}
	}
	return h.hub.SubscribeLive(ctx, bufCap)
}

func (h *Host) SubscribeFrom(ctx context.Context, bufCap int, seq uint64) (Replay, <-chan []byte, func()) {
	if h == nil || h.hub == nil {
		ch := make(chan []byte)
		close(ch)
		return Replay{}, ch, func() {}
	}
	return h.hub.SubscribeFrom(ctx, bufCap, seq)
}

func (h *Host) ReplaySince(seq uint64) Replay {
	if h == nil || h.hub == nil {
		return Replay{}
	}
	return h.hub.ReplaySince(seq)
}

func (h *Host) Resize(rows, cols uint16) error {
	if rows == 0 || cols == 0 {
		return errors.New("pty: rows and cols must be non-zero")
	}
	if h == nil || h.Master == nil {
		return errors.New("pty: not started")
	}
	if err := cpty.Setsize(h.Master, &cpty.Winsize{Rows: rows, Cols: cols}); err != nil {
		return fmt.Errorf("pty resize: %w", err)
	}
	if h.hub != nil {
		h.hub.BroadcastResize(rows, cols)
	}
	return nil
}

// Write sends bytes to the child's stdin (the PTY master end).
func (h *Host) Write(p []byte) (int, error) {
	if h.WriteFunc != nil {
		return h.WriteFunc(p)
	}
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
	if h.CloseFunc != nil {
		return h.CloseFunc()
	}
	err := h.Master.Close()
	if h.hub != nil {
		h.hub.Close()
	}
	return err
}
