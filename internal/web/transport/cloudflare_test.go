package transport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
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
}

func TestCloudflareQuickEnableAndCleanup(t *testing.T) {
	proc := newFakeProcess("https://fast-demo.trycloudflare.com")
	runner := &fakeProcessRunner{proc: proc}
	cf := &CloudflareQuick{Bin: "cloudflared", runner: runner}
	if err := cf.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	if got, err := cf.URL(context.Background()); err != nil || got != "https://fast-demo.trycloudflare.com" {
		t.Fatalf("url=%q err=%v", got, err)
	}
	want := [][]string{{"cloudflared", "tunnel", "--url", "https://localhost:8443"}}
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

func TestCloudflareNamedConfigValidation(t *testing.T) {
	cf := &CloudflareNamed{Bin: "cloudflared", Hostname: "app.example.com"}
	err := cf.Check(context.Background())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagAuthMissing || !strings.Contains(err.Error(), CloudflareTunnelNameEnv) {
		t.Fatalf("err = %#v", err)
	}
}

func TestCloudflareNamedRouteCollision(t *testing.T) {
	err := refuseCloudflareRouteCollision([]byte(`{
		"id":"target-tunnel",
		"routes":[{"hostname":"app.example.com","tunnel_id":"other-tunnel"}]
	}`), "target", "app.example.com")
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagAuthMissing || !strings.Contains(err.Error(), "already routed") {
		t.Fatalf("err = %#v", err)
	}
}

func TestCloudflareNamedEnableRunState(t *testing.T) {
	proc := newFakeProcess("INF Registered tunnel connection connIndex=0")
	pr := &fakeProcessRunner{proc: proc}
	cr := &fakeCommandRunner{outputs: map[string][]byte{
		"cloudflared tunnel info named": []byte(`{"id":"named","routes":[{"hostname":"app.example.com","tunnel_id":"named"}]}`),
	}}
	cf := &CloudflareNamed{Bin: "cloudflared", Tunnel: "named", Hostname: "app.example.com", runner: cr, processes: pr}
	if err := cf.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	if got, err := cf.URL(context.Background()); err != nil || got != "https://app.example.com" {
		t.Fatalf("url=%q err=%v", got, err)
	}
	want := [][]string{{"cloudflared", "tunnel", "run", "named"}}
	if !reflect.DeepEqual(pr.calls, want) {
		t.Fatalf("process calls = %#v", pr.calls)
	}
}

type fakeCommandRunner struct {
	outputs map[string][]byte
	errs    map[string]error
	calls   [][]string
}

func (r *fakeCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	key := strings.Join(call, " ")
	if err := r.errs[key]; err != nil {
		return nil, err
	}
	out, ok := r.outputs[key]
	if !ok {
		return nil, errors.New("unexpected command: " + key)
	}
	return out, nil
}

type fakeProcessRunner struct {
	proc  *fakeProcess
	calls [][]string
}

func (r *fakeProcessRunner) Start(_ context.Context, name string, args ...string) (managedProcess, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.proc, nil
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
	p.once.Do(func() { close(p.done) })
	return nil
}

func (p *fakeProcess) Wait() error {
	<-p.done
	return p.err
}
