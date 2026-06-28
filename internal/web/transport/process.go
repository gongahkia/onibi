package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

type processRunner interface {
	Start(context.Context, string, ...string) (managedProcess, error)
}

type managedProcess interface {
	Lines() <-chan string
	Kill() error
	Wait() error
}

type execProcessRunner struct{}

func (execProcessRunner) Start(ctx context.Context, name string, args ...string) (managedProcess, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execManagedProcess{cmd: cmd, lines: make(chan string, 32), done: make(chan error, 1)}
	var wg sync.WaitGroup
	wg.Add(2)
	go scanProcessLines(stdout, p.lines, &wg)
	go scanProcessLines(stderr, p.lines, &wg)
	go func() {
		err := cmd.Wait()
		wg.Wait()
		close(p.lines)
		p.done <- err
		close(p.done)
	}()
	return p, nil
}

type execManagedProcess struct {
	cmd   *exec.Cmd
	lines chan string
	done  chan error
}

func (p *execManagedProcess) Lines() <-chan string { return p.lines }

func (p *execManagedProcess) Kill() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *execManagedProcess) Wait() error {
	if p == nil {
		return nil
	}
	err, ok := <-p.done
	if !ok {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return err
	}
	return err
}

func scanProcessLines(r io.Reader, out chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		out <- sc.Text()
	}
}

func processExitError(provider string, err error) error {
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return Diagnostic(DiagActivationLag, provider, fmt.Sprintf("process exited with status %d before URL activation", exit.ExitCode()), err)
	}
	return Diagnostic(DiagActivationLag, provider, "process exited before URL activation", err)
}
