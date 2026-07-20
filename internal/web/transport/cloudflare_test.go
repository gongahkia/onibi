package transport

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCloudflareQuickCheckMissingBinary(t *testing.T) {
	cf := &CloudflareQuick{
		Bin: "cloudflared",
		lookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	}
	err := cf.Check(context.Background())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagBinaryMissing {
		t.Fatalf("err = %#v", err)
	}
}

func TestParseTryCloudflareURL(t *testing.T) {
	got, ok := parseTryCloudflareURL("ready at https://small-river-123.trycloudflare.com")
	if !ok || got != "https://small-river-123.trycloudflare.com" {
		t.Fatalf("url = %q ok=%v", got, ok)
	}
	if _, ok := parseTryCloudflareURL("https://example.com"); ok {
		t.Fatal("accepted non-trycloudflare URL")
	}
	for _, raw := range []string{"https://small-river-123.trycloudflare.com.evil", "https://small-river-123.trycloudflare.com/pair/token"} {
		if _, ok := parseTryCloudflareURL(raw); ok {
			t.Fatalf("accepted invalid quick URL %q", raw)
		}
	}
}

func TestCloudflareQuickEnableAndCleanup(t *testing.T) {
	proc := newFakeProcess("https://fast-demo.trycloudflare.com", "INF Registered tunnel connection connIndex=0")
	runner := &fakeProcessRunner{proc: proc}
	cf := &CloudflareQuick{Bin: "cloudflared", runner: runner}
	if err := cf.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	if got, err := cf.URL(context.Background()); err != nil || got != "https://fast-demo.trycloudflare.com" {
		t.Fatalf("url=%q err=%v", got, err)
	}
	if err := cf.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"cloudflared", "tunnel", "--url", "https://localhost:8443", "--no-tls-verify"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v", runner.calls)
	}
	if err := cf.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !proc.killed {
		t.Fatal("process not killed")
	}
}

func TestCloudflareQuickLifecycleDetectsExitAndReconnects(t *testing.T) {
	proc := newFakeProcess("https://fast-demo.trycloudflare.com", "INF Registered tunnel connection connIndex=0")
	replacement := newFakeProcess("https://fresh-demo.trycloudflare.com", "INF Registered tunnel connection connIndex=1")
	runner := &quickProcessRunner{processes: []*fakeProcess{proc, replacement}}
	cf := &CloudflareQuick{Bin: "cloudflared", runner: runner}
	session := NewLifecycle(ResolverOptions{Mode: string(ModeCloudflareQuick), Port: 8443, Providers: ProviderFactory{CloudflareQuick: func() Provider { return cf }}})
	if _, err := session.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		if _, err := session.Health(t.Context()); err != nil {
			break
		}
		select {
		case <-timer.C:
			t.Fatal("expected quick tunnel health failure")
		case <-ticker.C:
		}
	}
	if _, err := session.Reconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := session.Health(t.Context()); err != nil || !report.Healthy || len(report.Targets) != 1 || report.Targets[0] != "https://fresh-demo.trycloudflare.com" {
		t.Fatalf("health=%#v err=%v", report, err)
	}
	if err := session.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || !replacement.killed {
		t.Fatalf("starts=%d replacement_killed=%v", len(runner.calls), replacement.killed)
	}
	if diagnostics := session.Diagnostics(); len(diagnostics) != 1 || diagnostics[0].Operation != "health" || diagnostics[0].Code != DiagActivationLag {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}

type quickProcessRunner struct {
	processes []*fakeProcess
	calls     [][]string
}

func (r *quickProcessRunner) Start(_ context.Context, name string, args ...string) (managedProcess, error) {
	if len(r.processes) == 0 {
		return nil, errors.New("missing quick process")
	}
	p := r.processes[0]
	r.processes = r.processes[1:]
	r.calls = append(r.calls, append([]string{name}, args...))
	return p, nil
}

type fakeProcessRunner struct {
	proc     *fakeProcess
	calls    [][]string
	envCalls []envProcessCall
}

func (r *fakeProcessRunner) Start(_ context.Context, name string, args ...string) (managedProcess, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.proc, nil
}

func (r *fakeProcessRunner) StartEnv(_ context.Context, env []string, name string, args ...string) (managedProcess, error) {
	r.envCalls = append(r.envCalls, envProcessCall{env: append([]string(nil), env...), call: append([]string{name}, args...)})
	return r.proc, nil
}

type envProcessCall struct {
	env  []string
	call []string
}

type fakeProcess struct {
	lines  chan string
	done   chan struct{}
	err    error
	once   sync.Once
	killed bool
}

func newFakeProcess(lines ...string) *fakeProcess {
	p := &fakeProcess{lines: make(chan string, len(lines)), done: make(chan struct{})}
	for _, line := range lines {
		p.lines <- line
	}
	return p
}

func (p *fakeProcess) Lines() <-chan string { return p.lines }

func (p *fakeProcess) Kill() error {
	p.killed = true
	p.once.Do(func() {
		close(p.lines)
		close(p.done)
	})
	return nil
}

func (p *fakeProcess) Wait() error {
	<-p.done
	return p.err
}
