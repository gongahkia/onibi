package transport

import (
	"context"
	"errors"
	"net"
	"strings"
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
