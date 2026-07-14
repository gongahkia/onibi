package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
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
	proc := newFakeProcess("https://fast-demo.trycloudflare.com", "INF Registered tunnel connection connIndex=0")
	runner := &fakeProcessRunner{proc: proc}
	cf := &CloudflareQuick{Bin: "cloudflared", runner: runner}
	if err := cf.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	if got, err := cf.URL(context.Background()); err != nil || got != "https://fast-demo.trycloudflare.com" {
		t.Fatalf("url=%q err=%v", got, err)
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

func TestCloudflareNamedEnableWithKeychainAPIToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/account-1/cfd_tunnel/tunnel-id/token" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer cf-api-token-1234567890" {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": "cf-tunnel-token"})
	}))
	t.Cleanup(srv.Close)
	proc := newFakeProcess("INF Registered tunnel connection connIndex=0")
	pr := &fakeProcessRunner{proc: proc}
	cf := &CloudflareNamed{
		Bin:        "cloudflared",
		Tunnel:     "named",
		TunnelID:   "tunnel-id",
		Hostname:   "app.example.com",
		AccountID:  "account-1",
		APIToken:   "cf-api-token-1234567890",
		APIBaseURL: srv.URL,
		HTTPClient: srv.Client(),
		processes:  pr,
	}
	if err := cf.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	if len(pr.calls) != 0 {
		t.Fatalf("token leaked through args: %#v", pr.calls)
	}
	wantEnvCalls := []envProcessCall{{
		env:  []string{"TUNNEL_TOKEN=cf-tunnel-token"},
		call: []string{"cloudflared", "tunnel", "run"},
	}}
	if !reflect.DeepEqual(pr.envCalls, wantEnvCalls) {
		t.Fatalf("env calls = %#v", pr.envCalls)
	}
}

func TestCloudflareNamedReconnectRefreshesAPITunnelToken(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": "cf-tunnel-token-" + strconv.Itoa(requests)})
	}))
	t.Cleanup(srv.Close)
	runner := &rotatingProcessRunner{processes: []*fakeProcess{newFakeProcess("INF Registered tunnel connection connIndex=0"), newFakeProcess("INF Registered tunnel connection connIndex=1")}}
	provider := &CloudflareNamed{Bin: "cloudflared", Tunnel: "named", TunnelID: "tunnel-id", Hostname: "app.example.com", AccountID: "account-1", APIToken: "cf-api-token", APIBaseURL: srv.URL, HTTPClient: srv.Client(), processes: runner}
	session := NewLifecycle(ResolverOptions{Mode: string(ModeCloudflareNamed), Port: 8443, Providers: ProviderFactory{CloudflareNamed: func() Provider { return provider }}})
	if _, err := session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("token requests=%d", requests)
	}
	if got := runner.envCalls; len(got) != 2 || got[0].env[0] != "TUNNEL_TOKEN=cf-tunnel-token-1" || got[1].env[0] != "TUNNEL_TOKEN=cf-tunnel-token-2" {
		t.Fatalf("token env calls=%#v", got)
	}
	if _, err := session.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareNamedAPITokenRequiresAccountID(t *testing.T) {
	cf := &CloudflareNamed{Bin: "cloudflared", Tunnel: "named", TunnelID: "tunnel-id", Hostname: "app.example.com", APIToken: "cf-api-token-1234567890"}
	err := cf.Check(context.Background())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagAuthMissing || !strings.Contains(err.Error(), CloudflareAccountIDEnv) {
		t.Fatalf("err = %#v", err)
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
	proc     *fakeProcess
	calls    [][]string
	envCalls []envProcessCall
}

type rotatingProcessRunner struct {
	processes []*fakeProcess
	envCalls  []envProcessCall
}

func (r *rotatingProcessRunner) Start(context.Context, string, ...string) (managedProcess, error) {
	return nil, errors.New("unexpected non-token start")
}

func (r *rotatingProcessRunner) StartEnv(_ context.Context, env []string, name string, args ...string) (managedProcess, error) {
	if len(r.processes) == 0 {
		return nil, errors.New("missing rotating process")
	}
	p := r.processes[0]
	r.processes = r.processes[1:]
	r.envCalls = append(r.envCalls, envProcessCall{env: append([]string(nil), env...), call: append([]string{name}, args...)})
	return p, nil
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
