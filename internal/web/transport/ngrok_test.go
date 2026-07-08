package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
