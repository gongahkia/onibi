package transport

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/faulttest"
	"github.com/gongahkia/onibi/internal/fleet"
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

func TestTransportConformance(t *testing.T) {
	for _, tc := range []struct {
		mode Mode
		url  string
		kind fleet.EndpointKind
	}{
		{mode: ModeTailscale, url: "https://public.example.test", kind: fleet.EndpointRelay},
		{mode: ModeTailscalePrivate, url: "https://node.tail.ts.net", kind: fleet.EndpointMesh},
		{mode: ModeWireGuard, url: "https://100.64.0.2:8443", kind: fleet.EndpointMesh},
		{mode: ModeZeroTier, url: "https://10.147.20.4:8443", kind: fleet.EndpointMesh},
		{mode: ModeCloudflareQuick, url: "https://quick.trycloudflare.com", kind: fleet.EndpointRelay},
		{mode: ModeCloudflareNamed, url: "https://named.example.test", kind: fleet.EndpointRelay},
		{mode: ModeNgrok, url: "https://demo.ngrok-free.app", kind: fleet.EndpointRelay},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			provider := &faulttest.Provider{URLValue: tc.url}
			session := NewLifecycle(ResolverOptions{Mode: string(tc.mode), Port: 8443, Providers: conformanceProviderFactory(tc.mode, provider)})
			if _, err := session.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
				t.Fatalf("health=%#v err=%v", report, err)
			}
			if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != strings.TrimRight(tc.url, "/")+"/pair/pair-token" {
				t.Fatalf("urls=%#v err=%v", urls, err)
			}
			candidate, err := session.Enrollment()
			if err != nil || !candidate.RequiresOwnerProof || candidate.Endpoint.Kind != tc.kind {
				t.Fatalf("candidate=%#v err=%v", candidate, err)
			}
			provider.SetCheckError(Diagnostic(DiagActivationLag, string(tc.mode), "fault injected transport loss", errors.New("network reset")))
			if _, err := session.Health(t.Context()); err == nil {
				t.Fatal("expected health failure")
			}
			provider.SetCheckError(nil)
			if _, err := session.Reconnect(t.Context()); err != nil {
				t.Fatal(err)
			}
			if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
				t.Fatalf("recovered health=%#v err=%v", report, err)
			}
			if err := session.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			if enables, disables := provider.Counts(); enables != 2 || disables != 2 {
				t.Fatalf("enables=%d disables=%d", enables, disables)
			}
			if ports := provider.Ports(); len(ports) != 2 || ports[0] != 8443 || ports[1] != 8443 {
				t.Fatalf("ports=%#v", ports)
			}
			diagnostics := session.Diagnostics()
			if len(diagnostics) != 1 || diagnostics[0].Operation != "health" || diagnostics[0].Code != DiagActivationLag {
				t.Fatalf("diagnostics=%#v", diagnostics)
			}
		})
	}
}

func conformanceProviderFactory(mode Mode, provider Provider) ProviderFactory {
	switch mode {
	case ModeTailscale:
		return ProviderFactory{Tailscale: func() Provider { return provider }}
	case ModeTailscalePrivate:
		return ProviderFactory{TailscalePrivate: func() Provider { return provider }}
	case ModeWireGuard:
		return ProviderFactory{WireGuard: func() Provider { return provider }}
	case ModeZeroTier:
		return ProviderFactory{ZeroTier: func() Provider { return provider }}
	case ModeCloudflareQuick:
		return ProviderFactory{CloudflareQuick: func() Provider { return provider }}
	case ModeCloudflareNamed:
		return ProviderFactory{CloudflareNamed: func() Provider { return provider }}
	case ModeNgrok:
		return ProviderFactory{Ngrok: func() Provider { return provider }}
	default:
		panic("unsupported conformance transport " + string(mode))
	}
}

func TestTailscalePrivateAndFunnelLifecycleEnrollment(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode Mode
		url  string
		kind string
		new  func(*lifecycleProvider) ProviderFactory
	}{
		{
			name: "private serve mesh",
			mode: ModeTailscalePrivate,
			url:  "https://dev.tail.ts.net/",
			kind: "mesh",
			new: func(p *lifecycleProvider) ProviderFactory {
				return ProviderFactory{TailscalePrivate: func() Provider { return p }}
			},
		},
		{
			name: "public funnel relay",
			mode: ModeTailscale,
			url:  "https://dev.tail.ts.net/",
			kind: "relay",
			new: func(p *lifecycleProvider) ProviderFactory {
				return ProviderFactory{Tailscale: func() Provider { return p }}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &lifecycleProvider{url: tc.url}
			session := NewLifecycle(ResolverOptions{Mode: string(tc.mode), Port: 8443, Providers: tc.new(provider)})
			if _, err := session.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
				t.Fatalf("health=%#v err=%v", report, err)
			}
			if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != tc.url+"pair/pair-token" {
				t.Fatalf("urls=%#v err=%v", urls, err)
			}
			candidate, err := session.Enrollment()
			if err != nil || !candidate.RequiresOwnerProof || string(candidate.Endpoint.Kind) != tc.kind {
				t.Fatalf("candidate=%#v err=%v", candidate, err)
			}
			provider.checkErr = Diagnostic(DiagActivationLag, "tailscale", "fault injected transport loss", errors.New("network reset"))
			if _, err := session.Health(t.Context()); err == nil {
				t.Fatal("expected health failure")
			}
			provider.checkErr = nil
			if _, err := session.Reconnect(t.Context()); err != nil {
				t.Fatal(err)
			}
			if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
				t.Fatalf("recovered health=%#v err=%v", report, err)
			}
			if err := session.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			if provider.enables != 2 || provider.disables != 2 {
				t.Fatalf("enables=%d disables=%d", provider.enables, provider.disables)
			}
			diagnostics := session.Diagnostics()
			if len(diagnostics) != 1 || diagnostics[0].Operation != "health" || diagnostics[0].Code != DiagActivationLag {
				t.Fatalf("diagnostics=%#v", diagnostics)
			}
		})
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

func TestLifecycleRecoversFromFaultInjectedTransportLoss(t *testing.T) {
	provider := &faulttest.Provider{URLValue: "https://relay.example.test"}
	session := NewLifecycle(ResolverOptions{Mode: string(ModeCloudflareQuick), Port: 8443, Providers: ProviderFactory{CloudflareQuick: func() Provider { return provider }}})
	if _, err := session.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	provider.SetCheckError(Diagnostic(DiagActivationLag, "relay", "fault injected transport loss", errors.New("network reset")))
	if _, err := session.Health(t.Context()); err == nil {
		t.Fatal("expected transport health failure")
	}
	provider.SetCheckError(nil)
	if _, err := session.Reconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := session.Health(t.Context()); err != nil || !report.Healthy || report.State != LifecycleHealthy {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	if err := session.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	enables, disables := provider.Counts()
	if enables != 2 || disables != 2 {
		t.Fatalf("enables=%d disables=%d", enables, disables)
	}
	if diagnostics := session.Diagnostics(); len(diagnostics) != 1 || diagnostics[0].Operation != "health" || diagnostics[0].Code != DiagActivationLag {
		t.Fatalf("diagnostics=%#v", diagnostics)
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
