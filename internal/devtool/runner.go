package devtool

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Runner interface {
	Run(context.Context, string, []string, string, []string, func(string)) (string, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, env []string, name string, args []string, line func(string)) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	// Build tools and compilers form one child process group. Cancellation must
	// not leave a compiler behind after the progress display stops.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	writer := &lineWriter{line: line}
	cmd.Stdout = writer
	cmd.Stderr = writer
	err := cmd.Run()
	writer.flush()
	if ctx.Err() != nil {
		return writer.tail, ctx.Err()
	}
	if err != nil {
		return writer.tail, fmt.Errorf("%s failed: %w", name, err)
	}
	return writer.tail, nil
}

type lineWriter struct {
	mu            sync.Mutex
	pending, tail string
	line          func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tail += string(p)
	if len(w.tail) > 256<<10 {
		w.tail = w.tail[len(w.tail)-(256<<10):]
	}
	w.pending += string(p)
	for {
		index := strings.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		w.emit(w.pending[:index])
		w.pending = w.pending[index+1:]
	}
	if len(w.pending) > 64<<10 {
		w.emit(w.pending[:64<<10])
		w.pending = w.pending[64<<10:]
	}
	return len(p), nil
}
func (w *lineWriter) emit(s string) {
	if w.line != nil {
		w.line(s)
	}
}
func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != "" {
		w.emit(w.pending)
		w.pending = ""
	}
}

var _ io.Writer = (*lineWriter)(nil)
