package transport

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

func TestZerotierEnableUsesJSONAssignedAddress(t *testing.T) {
	zt := testZeroTier(`[{"id":"8056c2e21c000001","name":"dev","status":"OK","type":"PRIVATE","portDeviceName":"ztdev","assignedAddresses":["fe80::1/64","10.147.20.4/24"]}]`, nil)
	if err := zt.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	got, err := zt.URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://10.147.20.4:8443" || zt.NetworkID() != "8056c2e21c000001 (dev)" || zt.InterfaceName() != "ztdev" {
		t.Fatalf("url=%q network=%q iface=%q", got, zt.NetworkID(), zt.InterfaceName())
	}
}

func TestZerotierUsesConfiguredNetwork(t *testing.T) {
	zt := testZeroTier(`[
{"id":"8056c2e21c000001","name":"dev","status":"OK","portDeviceName":"ztdev","assignedAddresses":["10.147.20.4/24"]},
{"id":"8056c2e21c000002","name":"prod","status":"OK","portDeviceName":"ztprod","assignedAddresses":["fd00:147::4/64"]}
]`, nil)
	zt.Network = "prod"
	host, err := zt.BindHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if host != "fd00:147::4" || zt.NetworkID() != "8056c2e21c000002 (prod)" || zt.InterfaceName() != "ztprod" {
		t.Fatalf("host=%q network=%q iface=%q", host, zt.NetworkID(), zt.InterfaceName())
	}
}

func TestZerotierRequiresNetworkOverrideWhenMultipleNetworksAreReady(t *testing.T) {
	zt := testZeroTier(`[
{"id":"8056c2e21c000002","name":"prod","status":"OK","portDeviceName":"ztprod","assignedAddresses":["10.147.20.5/24"]},
{"id":"8056c2e21c000001","name":"dev","status":"OK","portDeviceName":"ztdev","assignedAddresses":["10.147.20.4/24"]}
]`, nil)
	_, err := zt.BindHost(context.Background())
	if err == nil || !strings.Contains(err.Error(), ZeroTierNetworkEnv) || !strings.Contains(err.Error(), "8056c2e21c000001 (dev)") {
		t.Fatalf("err=%v", err)
	}
}

func TestZerotierSelectsAssignedAddressDeterministically(t *testing.T) {
	zt := testZeroTier(`[{"id":"8056c2e21c000001","name":"dev","status":"OK","portDeviceName":"ztdev","assignedAddresses":["10.147.20.9/24","fd00:147::9/64","10.147.20.4/24"]}]`, nil)
	host, err := zt.BindHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.147.20.4" {
		t.Fatalf("host=%q", host)
	}
}

func TestZerotierFallsBackToInterfaceAddress(t *testing.T) {
	zt := testZeroTier(`[{"id":"8056c2e21c000001","name":"dev","status":"OK","portDeviceName":"ztdev","assignedAddresses":[]}]`, map[string][]net.Addr{
		"ztdev": {mustAddr(t, "10.99.0.5/24")},
	})
	host, err := zt.BindHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.99.0.5" || zt.InterfaceName() != "ztdev" {
		t.Fatalf("host=%q iface=%q", host, zt.InterfaceName())
	}
}

func TestZerotierRejectsOfflineDaemon(t *testing.T) {
	zt := testZeroTier(`[{"id":"8056c2e21c000001","name":"dev","status":"OK","assignedAddresses":["10.147.20.4/24"]}]`, nil)
	zt.runner = &fakeTSRunner{outputs: map[string][]byte{
		"zerotier-cli info": []byte("200 info deadbeef 1.14.2 OFFLINE\n"),
	}}
	err := zt.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "OFFLINE") {
		t.Fatalf("err = %v", err)
	}
}

func TestZerotierAcceptsTunneledDaemon(t *testing.T) {
	zt := testZeroTier(`[{"id":"8056c2e21c000001","name":"dev","status":"OK","assignedAddresses":["10.147.20.4/24"]}]`, nil)
	zt.runner = &fakeTSRunner{outputs: map[string][]byte{
		"zerotier-cli info":            []byte("200 info deadbeef 1.14.2 TUNNELED\n"),
		"zerotier-cli listnetworks -j": []byte(`[{"id":"8056c2e21c000001","name":"dev","status":"OK","assignedAddresses":["10.147.20.4/24"]}]`),
	}}
	if err := zt.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestZerotierParsesTextListNetworks(t *testing.T) {
	r := &fakeTSRunner{
		outputs: map[string][]byte{
			"zerotier-cli info":         []byte("200 info deadbeef 1.14.2 ONLINE\n"),
			"zerotier-cli listnetworks": []byte("200 listnetworks 8056c2e21c000001 my cool network 92:99:aa:bb:cc:dd OK PRIVATE ztdev 10.147.20.4/24\n"),
		},
		errs: map[string]error{
			"zerotier-cli listnetworks -j": errors.New("unknown option"),
		},
	}
	zt := &ZeroTier{
		Bin:            "zerotier-cli",
		runner:         r,
		lookPath:       func(string) (string, error) { return "/usr/bin/zerotier-cli", nil },
		interfaceAddrs: func(string) ([]net.Addr, error) { return nil, errors.New("unused") },
	}
	host, err := zt.BindHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.147.20.4" || zt.NetworkID() != "8056c2e21c000001 (my cool network)" || zt.InterfaceName() != "ztdev" {
		t.Fatalf("host=%q network=%q iface=%q", host, zt.NetworkID(), zt.InterfaceName())
	}
}

func TestResolveZerotierStartsProvider(t *testing.T) {
	provider := &fakeProvider{url: "https://10.147.20.4:8443"}
	pt, err := Resolve(context.Background(), ResolverOptions{
		Mode: "zerotier",
		Port: 8443,
		Providers: ProviderFactory{
			ZeroTier: func() Provider { return provider },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pt.Mode != ModeZeroTier || provider.enablePort != 8443 {
		t.Fatalf("mode=%q enable=%d", pt.Mode, provider.enablePort)
	}
	if got := pt.URLs("tok"); len(got) != 1 || got[0] != "https://10.147.20.4:8443/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
}

func TestZeroTierLifecycleDetectsNetworkChangeAndRecovers(t *testing.T) {
	runner := &zeroTierStateRunner{networks: `[{"id":"8056c2e21c000001","name":"dev","status":"OK","portDeviceName":"ztdev","assignedAddresses":["10.147.20.4/24"]}]`}
	zt := &ZeroTier{
		Bin:            "zerotier-cli",
		runner:         runner,
		lookPath:       func(string) (string, error) { return "/usr/bin/zerotier-cli", nil },
		interfaceAddrs: func(string) ([]net.Addr, error) { return nil, errors.New("unused") },
	}
	session := NewLifecycle(ResolverOptions{Mode: string(ModeZeroTier), Port: 8443, Providers: ProviderFactory{ZeroTier: func() Provider { return zt }}})
	if _, err := session.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := session.Health(t.Context()); err != nil || !report.Healthy {
		t.Fatalf("health=%#v err=%v", report, err)
	}
	if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != "https://10.147.20.4:8443/pair/pair-token" {
		t.Fatalf("urls=%#v err=%v", urls, err)
	}
	candidate, err := session.Enrollment()
	if err != nil || !candidate.RequiresOwnerProof || candidate.Endpoint.Kind != "mesh" {
		t.Fatalf("candidate=%#v err=%v", candidate, err)
	}
	runner.SetNetworks(`[{"id":"8056c2e21c000002","name":"prod","status":"OK","portDeviceName":"ztprod","assignedAddresses":["fd00:147::4/64"]}]`)
	if _, err := session.Health(t.Context()); err == nil {
		t.Fatal("expected network health failure")
	}
	if _, err := session.Reconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if report, err := session.Health(t.Context()); err != nil || !report.Healthy || len(report.Targets) != 1 || report.Targets[0] != "https://[fd00:147::4]:8443" {
		t.Fatalf("recovered health=%#v err=%v", report, err)
	}
	if err := session.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if zt.NetworkID() != "" || zt.InterfaceName() != "" {
		t.Fatalf("network=%q interface=%q", zt.NetworkID(), zt.InterfaceName())
	}
	diagnostics := session.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Operation != "health" || diagnostics[0].Code != DiagActivationLag {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
}

type zeroTierStateRunner struct {
	mu       sync.Mutex
	networks string
}

func (r *zeroTierStateRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	switch strings.Join(args, " ") {
	case "info":
		return []byte("200 info deadbeef 1.14.2 ONLINE\n"), nil
	case "listnetworks -j":
		r.mu.Lock()
		defer r.mu.Unlock()
		return []byte(r.networks), nil
	default:
		return nil, errors.New("unexpected zerotier command")
	}
}

func (r *zeroTierStateRunner) SetNetworks(networks string) {
	r.mu.Lock()
	r.networks = networks
	r.mu.Unlock()
}

func testZeroTier(networks string, addrs map[string][]net.Addr) *ZeroTier {
	if addrs == nil {
		addrs = map[string][]net.Addr{}
	}
	return &ZeroTier{
		Bin: "zerotier-cli",
		runner: &fakeTSRunner{outputs: map[string][]byte{
			"zerotier-cli info":            []byte("200 info deadbeef 1.14.2 ONLINE\n"),
			"zerotier-cli listnetworks -j": []byte(networks),
		}},
		lookPath: func(string) (string, error) { return "/usr/bin/zerotier-cli", nil },
		interfaceAddrs: func(name string) ([]net.Addr, error) {
			got, ok := addrs[name]
			if !ok {
				return nil, errors.New("unknown interface")
			}
			return got, nil
		},
	}
}
