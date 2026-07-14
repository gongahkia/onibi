package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestNgrokCheckMissingBinary(t *testing.T) {
	n := &Ngrok{
		Bin: "ngrok",
		lookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	}
	err := n.Check(context.Background())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagBinaryMissing {
		t.Fatalf("err = %#v", err)
	}
}

func TestNgrokReservedDomainRequiresToken(t *testing.T) {
	n := &Ngrok{Bin: "ngrok", Domain: "demo.example.com"}
	err := n.Check(context.Background())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagAuthMissing {
		t.Fatalf("err = %#v", err)
	}
}

func TestSelectNgrokTunnelPrefersReservedDomain(t *testing.T) {
	got, ok, err := selectNgrokTunnel([]ngrokTunnel{
		{Name: "other", PublicURL: "https://random.ngrok-free.app", Config: ngrokTunnelConfig{Addr: "https://localhost:8443"}},
		{Name: "reserved", PublicURL: "https://demo.example.com", Config: ngrokTunnelConfig{Addr: "https://localhost:8443"}},
	}, 8443, "demo.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Name != "reserved" {
		t.Fatalf("tunnel = %#v ok=%v", got, ok)
	}
}

func TestSelectNgrokTunnelRefusesHTTPOnlyPublicURL(t *testing.T) {
	_, ok, err := selectNgrokTunnel([]ngrokTunnel{{
		Name:      "insecure",
		PublicURL: "http://demo.ngrok-free.app",
		Config:    ngrokTunnelConfig{Addr: "https://localhost:8443"},
	}}, 8443, "")
	var diag *DiagnosticError
	if ok || !errors.As(err, &diag) || diag.Code != DiagURLParse {
		t.Fatalf("ok=%v err=%#v", ok, err)
	}
}

func TestSelectNgrokTunnelAllowsHTTPCompanionWhenHTTPSPresent(t *testing.T) {
	got, ok, err := selectNgrokTunnel([]ngrokTunnel{
		{Name: "http", PublicURL: "http://demo.ngrok-free.app", Config: ngrokTunnelConfig{Addr: "https://localhost:8443"}},
		{Name: "https", PublicURL: "https://demo.ngrok-free.app", Config: ngrokTunnelConfig{Addr: "https://localhost:8443"}},
	}, 8443, "")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Name != "https" {
		t.Fatalf("tunnel = %#v ok=%v", got, ok)
	}
}

func TestNgrokEnableDiscoversAndDeletesTunnel(t *testing.T) {
	var deleted string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tunnels":
			_ = json.NewEncoder(w).Encode(ngrokTunnelList{Tunnels: []ngrokTunnel{{
				Name:      "command_line",
				PublicURL: "https://demo.ngrok-free.app",
				Config:    ngrokTunnelConfig{Addr: "https://localhost:8443"},
			}}})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/tunnels/"):
			deleted = strings.TrimPrefix(r.URL.Path, "/api/tunnels/")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	proc := newFakeProcess()
	runner := &fakeProcessRunner{proc: proc}
	n := &Ngrok{Bin: "ngrok", AgentAPI: api.URL, Client: api.Client(), runner: runner}
	if err := n.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	if got, err := n.URL(context.Background()); err != nil || got != "https://demo.ngrok-free.app" {
		t.Fatalf("url=%q err=%v", got, err)
	}
	if err := n.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if deleted != "command_line" || !proc.killed {
		t.Fatalf("deleted=%q killed=%v", deleted, proc.killed)
	}
}

func TestNgrokEnableUsesTokenEnv(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/tunnels" {
			_ = json.NewEncoder(w).Encode(ngrokTunnelList{Tunnels: []ngrokTunnel{{
				Name:      "command_line",
				PublicURL: "https://demo.ngrok-free.app",
				Config:    ngrokTunnelConfig{Addr: "https://localhost:8443"},
			}}})
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()
	runner := &fakeProcessRunner{proc: newFakeProcess()}
	n := &Ngrok{Bin: "ngrok", Authtoken: "ngrok-token-1234567890", AgentAPI: api.URL, Client: api.Client(), runner: runner}
	if err := n.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("token leaked through args: %#v", runner.calls)
	}
	want := []envProcessCall{{
		env:  []string{ngrokAgentTokenEnv + "=ngrok-token-1234567890"},
		call: []string{"ngrok", "http", "https://localhost:8443"},
	}}
	if !reflect.DeepEqual(runner.envCalls, want) {
		t.Fatalf("env calls = %#v", runner.envCalls)
	}
}

func TestNgrokLifecycleDetectsLossRecoversAndEnrollsRelay(t *testing.T) {
	var state struct {
		sync.Mutex
		active  bool
		deletes int
	}
	setActive := func(active bool) {
		state.Lock()
		state.active = active
		state.Unlock()
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tunnels":
			body := ngrokTunnelList{}
			if state.active {
				body.Tunnels = []ngrokTunnel{{Name: "command_line", PublicURL: "https://demo.ngrok-free.app", Config: ngrokTunnelConfig{Addr: "https://localhost:8443"}}}
			}
			_ = json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/tunnels/command_line":
			state.active = false
			state.deletes++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	runner := &ngrokLifecycleRunner{processes: []*fakeProcess{newFakeProcess(), newFakeProcess()}, onStart: func() { setActive(true) }}
	provider := &Ngrok{Bin: "ngrok", AgentAPI: api.URL, Client: api.Client(), runner: runner}
	session := NewLifecycle(ResolverOptions{Mode: string(ModeNgrok), Port: 8443, Providers: ProviderFactory{Ngrok: func() Provider { return provider }}})
	if _, err := session.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
		t.Fatalf("health=%#v err=%v", report, err)
	}
	if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != "https://demo.ngrok-free.app/pair/pair-token" {
		t.Fatalf("urls=%#v err=%v", urls, err)
	}
	candidate, err := session.Enrollment()
	if err != nil || !candidate.RequiresOwnerProof || candidate.Endpoint.Kind != "relay" {
		t.Fatalf("candidate=%#v err=%v", candidate, err)
	}
	setActive(false)
	if _, err := session.Health(t.Context()); err == nil {
		t.Fatal("expected health failure")
	}
	if _, err := session.Reconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
		t.Fatalf("recovered health=%#v err=%v", report, err)
	}
	if err := session.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	state.Lock()
	deletes := state.deletes
	state.Unlock()
	if deletes != 2 || len(runner.calls) != 2 {
		t.Fatalf("deletes=%d starts=%d", deletes, len(runner.calls))
	}
	diagnostics := session.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Operation != "health" || diagnostics[0].Code != DiagActivationLag {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}

func TestNgrokDisableReportsAgentCleanupFailure(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tunnels":
			_ = json.NewEncoder(w).Encode(ngrokTunnelList{Tunnels: []ngrokTunnel{{Name: "command_line", PublicURL: "https://demo.ngrok-free.app", Config: ngrokTunnelConfig{Addr: "https://localhost:8443"}}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/tunnels/command_line":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	proc := newFakeProcess()
	n := &Ngrok{Bin: "ngrok", AgentAPI: api.URL, Client: api.Client(), runner: &fakeProcessRunner{proc: proc}}
	if err := n.Enable(t.Context(), 8443); err != nil {
		t.Fatal(err)
	}
	err := n.Disable(t.Context())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagCleanup || !proc.killed {
		t.Fatalf("err=%#v killed=%v", err, proc.killed)
	}
}

type ngrokLifecycleRunner struct {
	processes []*fakeProcess
	onStart   func()
	calls     [][]string
}

func (r *ngrokLifecycleRunner) Start(_ context.Context, name string, args ...string) (managedProcess, error) {
	if len(r.processes) == 0 {
		return nil, errors.New("missing ngrok process")
	}
	if r.onStart != nil {
		r.onStart()
	}
	p := r.processes[0]
	r.processes = r.processes[1:]
	r.calls = append(r.calls, append([]string{name}, args...))
	return p, nil
}
