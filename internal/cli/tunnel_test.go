package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/web/transport"
)

func TestTunnelExposesPortWithTransportAndPrintsTargetURL(t *testing.T) {
	withDefaultState(t)
	oldProviders := newTransportProviders
	fake := &fakePairTransport{url: "https://dev.tail.ts.net/"}
	newTransportProviders = func() transport.ProviderFactory {
		return transport.ProviderFactory{Tailscale: func() transport.Provider { return fake }}
	}
	t.Cleanup(func() { newTransportProviders = oldProviders })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	cmd := tunnelCmd()
	cmd.SetContext(ctx)
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"4173", "--transport", "tailscale", "--no-qr"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if fake.enablePort != 4173 {
		t.Fatalf("enable port = %d", fake.enablePort)
	}
	if !fake.disabled {
		t.Fatal("disable not called")
	}
	if !strings.Contains(got, "https://dev.tail.ts.net") || strings.Contains(got, "/pair/") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestTunnelRejectsInvalidPort(t *testing.T) {
	if _, err := parseTunnelPort("70000"); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestTunnelRejectsChatTransport(t *testing.T) {
	if tunnelTransportSupported("telegram") {
		t.Fatal("telegram should not support ad-hoc web tunnels")
	}
}

func TestTunnelRejectsNamedCloudflareTransport(t *testing.T) {
	if tunnelTransportSupported("cloudflare-named") {
		t.Fatal("cloudflare-named should not support ad-hoc web tunnels")
	}
}
