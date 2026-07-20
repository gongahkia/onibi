package transport

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestWireGuardEnableUsesConfiguredRoutableInterfaceIP(t *testing.T) {
	wg := testWireGuard("wg0 utun7\n", map[string][]net.Addr{
		"wg0":   {mustAddr(t, "fe80::1/64"), mustAddr(t, "10.8.0.2/24")},
		"utun7": {mustAddr(t, "fd00::2/64")},
	})
	wg.Interface = "wg0"
	if err := wg.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	got, err := wg.URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://10.8.0.2:8443" || wg.InterfaceName() != "wg0" {
		t.Fatalf("url=%q iface=%q", got, wg.InterfaceName())
	}
}

func TestWireGuardRequiresInterfaceOverrideWhenMultipleRoutable(t *testing.T) {
	wg := testWireGuard("utun7 wg0\n", map[string][]net.Addr{
		"wg0":   {mustAddr(t, "10.8.0.2/24")},
		"utun7": {mustAddr(t, "fd00::2/64")},
	})
	err := wg.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), WireGuardInterfaceEnv) {
		t.Fatalf("err=%v", err)
	}
}

func TestWireGuardUsesDeterministicPreferredAddress(t *testing.T) {
	wg := testWireGuard("wg0\n", map[string][]net.Addr{
		"wg0": {mustAddr(t, "10.8.0.9/24"), mustAddr(t, "fd00::9/64"), mustAddr(t, "10.8.0.2/24")},
	})
	host, err := wg.BindHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.8.0.2" {
		t.Fatalf("host=%q", host)
	}
}

func TestWireGuardUsesConfiguredInterface(t *testing.T) {
	wg := testWireGuard("wg0 utun7\n", map[string][]net.Addr{
		"wg0":   {mustAddr(t, "10.8.0.2/24")},
		"utun7": {mustAddr(t, "fd00::2/64")},
	})
	wg.Interface = "utun7"
	host, err := wg.BindHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if host != "fd00::2" || wg.InterfaceName() != "utun7" {
		t.Fatalf("host=%q iface=%q", host, wg.InterfaceName())
	}
}

func TestWireGuardRejectsMissingConfiguredInterface(t *testing.T) {
	wg := testWireGuard("wg0\n", map[string][]net.Addr{"wg0": {mustAddr(t, "10.8.0.2/24")}})
	wg.Interface = "utun7"
	err := wg.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not reported") {
		t.Fatalf("err = %v", err)
	}
}

func TestWireGuardRejectsNoRoutableAddress(t *testing.T) {
	wg := testWireGuard("wg0\n", map[string][]net.Addr{"wg0": {mustAddr(t, "127.0.0.1/8"), mustAddr(t, "fe80::1/64")}})
	err := wg.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no WireGuard interface") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveWireGuardStartsProvider(t *testing.T) {
	provider := &fakeProvider{url: "https://10.8.0.2:8443"}
	pt, err := Resolve(context.Background(), ResolverOptions{
		Mode: "wireguard",
		Port: 8443,
		Providers: ProviderFactory{
			WireGuard: func() Provider { return provider },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pt.Mode != ModeWireGuard || provider.enablePort != 8443 {
		t.Fatalf("mode=%q enable=%d", pt.Mode, provider.enablePort)
	}
	if got := pt.URLs("tok"); len(got) != 1 || got[0] != "https://10.8.0.2:8443/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
}

func TestWireGuardLifecycleFailsClosedWhenSelectedEndpointChanges(t *testing.T) {
	addrs := map[string][]net.Addr{
		"wg0":   {mustAddr(t, "10.8.0.2/24")},
		"utun7": {mustAddr(t, "fd00::2/64")},
	}
	wg := testWireGuard("wg0 utun7\n", addrs)
	wg.Interface = "wg0"
	session := NewLifecycle(ResolverOptions{Mode: string(ModeWireGuard), Port: 8443, Providers: ProviderFactory{WireGuard: func() Provider { return wg }}})
	if _, err := session.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
		t.Fatalf("health=%#v err=%v", report, err)
	}
	if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != "https://10.8.0.2:8443/pair/pair-token" {
		t.Fatalf("urls=%#v err=%v", urls, err)
	}
	candidate, err := session.Enrollment()
	if err != nil || !candidate.RequiresOwnerProof || candidate.Endpoint.Kind != "mesh" {
		t.Fatalf("candidate=%#v err=%v", candidate, err)
	}
	delete(addrs, "wg0")
	if _, err := session.Health(t.Context()); err == nil {
		t.Fatal("expected endpoint health failure")
	}
	if _, err := session.Reconnect(t.Context()); err == nil {
		t.Fatal("expected reconnect failure")
	}
	if err := session.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if wg.InterfaceName() != "" {
		t.Fatalf("interface after shutdown=%q", wg.InterfaceName())
	}
	diagnostics := session.Diagnostics()
	if len(diagnostics) != 2 || diagnostics[0].Operation != "health" || diagnostics[1].Operation != "reconnect" {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}

func testWireGuard(interfaces string, addrs map[string][]net.Addr) *WireGuard {
	return &WireGuard{
		Bin: "wg",
		runner: &fakeTSRunner{outputs: map[string][]byte{
			"wg show interfaces": []byte(interfaces),
		}},
		lookPath: func(string) (string, error) { return "/usr/bin/wg", nil },
		interfaceAddrs: func(name string) ([]net.Addr, error) {
			got, ok := addrs[name]
			if !ok {
				return nil, errors.New("unknown interface")
			}
			return got, nil
		},
	}
}

func mustAddr(t *testing.T, raw string) net.Addr {
	t.Helper()
	ip, n, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatal(err)
	}
	n.IP = ip
	return n
}

type fakeProvider struct {
	url        string
	enablePort int
	disabled   bool
}

func (p *fakeProvider) Check(context.Context) error { return nil }
func (p *fakeProvider) Enable(_ context.Context, port int) error {
	p.enablePort = port
	return nil
}
func (p *fakeProvider) URL(context.Context) (string, error) { return p.url, nil }
func (p *fakeProvider) Disable(context.Context) error {
	p.disabled = true
	return nil
}
