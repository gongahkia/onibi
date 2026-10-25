// Package process runs cancellable, noninteractive tools with bounded output.
package process

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type capture struct {
	mu        sync.Mutex
	data      strings.Builder
	limit     int
	truncated bool
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	keep := min(n, max(0, c.limit-c.data.Len()))
	c.data.Write(p[:keep])
	c.truncated = c.truncated || keep < n
	return n, nil
}
func (c *capture) snapshot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.data.String()
	if c.truncated {
		s += "\n[output truncated at configured bound]\n"
	}
	return strings.ToValidUTF8(s, "�")
}
func Run(ctx context.Context, argv []string, dir string, limit int, update func(string)) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	out := &capture{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case err := <-done:
			return out.snapshot(), err
		case <-ticker.C:
			if update != nil {
				s := out.snapshot()
				if s != last {
					update(s)
					last = s
				}
			}
		}
	}
}
