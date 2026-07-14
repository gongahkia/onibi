package transport

import (
	"context"
	"errors"
	"testing"
)

func TestLifecycleCoversProviderStartHealthPairReconnectAndShutdown(t *testing.T) {
	provider := &lifecycleProvider{url: "https://relay.example.test"}
	session := NewLifecycle(ResolverOptions{Mode: string(ModeCloudflareQuick), Port: 8443, Providers: ProviderFactory{CloudflareQuick: func() Provider { return provider }}})
	if _, err := session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	health, err := session.Health(context.Background())
	if err != nil || !health.Healthy || health.State != LifecycleHealthy {
		t.Fatalf("health=%#v err=%v", health, err)
	}
	if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != "https://relay.example.test/pair/pair-token" {
		t.Fatalf("urls=%#v err=%v", urls, err)
	}
	candidate, err := session.Enrollment()
	if err != nil || !candidate.RequiresOwnerProof || candidate.Endpoint.Kind != "relay" {
		t.Fatalf("candidate=%#v err=%v", candidate, err)
	}
	if _, err := session.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.enables != 2 || provider.disables != 2 {
		t.Fatalf("enables=%d disables=%d", provider.enables, provider.disables)
	}
}

func TestLifecycleRecordsHealthDiagnostics(t *testing.T) {
	provider := &lifecycleProvider{url: "https://relay.example.test", checkErr: Diagnostic(DiagActivationLag, "relay", "unavailable", errors.New("down"))}
	session := NewLifecycle(ResolverOptions{Mode: string(ModeCloudflareQuick), Port: 8443, Providers: ProviderFactory{CloudflareQuick: func() Provider { return provider }}})
	if _, err := session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Health(context.Background()); err == nil {
		t.Fatal("expected health error")
	}
	diagnostics := session.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Operation != "health" || diagnostics[0].Code != DiagActivationLag {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}

func TestLifecycleCoversStaticLANWithoutProviderBypass(t *testing.T) {
	session := NewLifecycle(ResolverOptions{Mode: string(ModeLAN), Port: 8443, FallbackHost: "host.example.test"})
	if _, err := session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Pair("pair-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type lifecycleProvider struct {
	url      string
	checkErr error
	enables  int
	disables int
}

func (p *lifecycleProvider) Check(context.Context) error { return p.checkErr }
func (p *lifecycleProvider) Enable(context.Context, int) error {
	p.enables++
	return nil
}
func (p *lifecycleProvider) URL(context.Context) (string, error) { return p.url, nil }
func (p *lifecycleProvider) Disable(context.Context) error {
	p.disables++
	return nil
}
