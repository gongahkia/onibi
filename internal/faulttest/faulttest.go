package faulttest

import (
	"context"
	"sync"
)

type Gate struct {
	started     chan struct{}
	released    chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func NewGate() *Gate {
	return &Gate{started: make(chan struct{}), released: make(chan struct{})}
}

func (g *Gate) Wait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.startOnce.Do(func() { close(g.started) })
	select {
	case <-g.released:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Gate) Started() <-chan struct{} {
	if g == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return g.started
}

func (g *Gate) Release() {
	if g != nil {
		g.releaseOnce.Do(func() { close(g.released) })
	}
}

type Process struct {
	mu       sync.Mutex
	done     chan struct{}
	waited   chan struct{}
	doneOnce sync.Once
	waitOnce sync.Once
	writes   [][]byte
	writeErr error
	closeErr error
	waitErr  error
}

func NewProcess() *Process {
	return &Process{done: make(chan struct{}), waited: make(chan struct{})}
}

func (p *Process) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes = append(p.writes, append([]byte(nil), data...))
	return len(data), p.writeErr
}

func (p *Process) Close() error {
	p.mu.Lock()
	err := p.closeErr
	p.mu.Unlock()
	p.Exit(nil)
	return err
}

func (p *Process) Wait() error {
	p.waitOnce.Do(func() { close(p.waited) })
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *Process) WaitStarted() <-chan struct{} {
	return p.waited
}

func (p *Process) Exit(err error) {
	p.doneOnce.Do(func() {
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(p.done)
	})
}

func (p *Process) SetWriteError(err error) {
	p.mu.Lock()
	p.writeErr = err
	p.mu.Unlock()
}

func (p *Process) SetCloseError(err error) {
	p.mu.Lock()
	p.closeErr = err
	p.mu.Unlock()
}

func (p *Process) Writes() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]byte, len(p.writes))
	for i, data := range p.writes {
		out[i] = append([]byte(nil), data...)
	}
	return out
}

type Command struct {
	Name string
	Args []string
}

type Runner struct {
	Gate    *Gate
	RunFunc func(context.Context, string, ...string) ([]byte, error)

	mu    sync.Mutex
	calls []Command
}

func (r *Runner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, Command{Name: name, Args: append([]string(nil), args...)})
	r.mu.Unlock()
	if err := r.Gate.Wait(ctx); err != nil {
		return nil, err
	}
	if r.RunFunc == nil {
		return nil, nil
	}
	return r.RunFunc(ctx, name, args...)
}

func (r *Runner) Calls() []Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Command(nil), r.calls...)
}

type Provider struct {
	mu         sync.Mutex
	URLValue   string
	CheckErr   error
	EnableErr  error
	URLErr     error
	DisableErr error
	enables    int
	disables   int
	ports      []int
}

func (p *Provider) Check(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.CheckErr
}

func (p *Provider) Enable(_ context.Context, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enables++
	p.ports = append(p.ports, port)
	return p.EnableErr
}

func (p *Provider) URL(context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.URLValue, p.URLErr
}

func (p *Provider) Disable(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disables++
	return p.DisableErr
}

func (p *Provider) SetCheckError(err error) {
	p.mu.Lock()
	p.CheckErr = err
	p.mu.Unlock()
}

func (p *Provider) Counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enables, p.disables
}

func (p *Provider) Ports() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.ports...)
}
