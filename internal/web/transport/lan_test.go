package transport

import (
	"errors"
	"strings"
	"testing"
)

func TestWebPairURLsFormatsHostsAndSuppressesDuplicates(t *testing.T) {
	got := WebPairURLs("tok", 8443, []string{"192.168.1.20", "fd00::1", "192.168.1.20"}, "host.local")
	want := []string{
		"https://192.168.1.20:8443/pair/tok",
		"https://[fd00::1]:8443/pair/tok",
		"https://host.local:8443/pair/tok",
	}
	if len(got) != len(want) {
		t.Fatalf("urls = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("urls = %#v", got)
		}
	}
}

func TestWebPairURLsFallsBackToLocalhost(t *testing.T) {
	got := WebPairURLs("tok", 9443, nil, "")
	if len(got) != 1 || got[0] != "https://localhost:9443/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
}

func TestWebURLsFormatTargetRoot(t *testing.T) {
	got := WebURLs(3000, []string{"192.168.1.20", "fd00::1", "192.168.1.20"}, "host.local")
	want := []string{
		"https://192.168.1.20:3000",
		"https://[fd00::1]:3000",
		"https://host.local:3000",
	}
	if len(got) != len(want) {
		t.Fatalf("urls = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("urls = %#v", got)
		}
	}
}

func TestLANLoopbackResolved(t *testing.T) {
	pt, err := Resolve(t.Context(), ResolverOptions{Mode: "lan-loopback", Port: 18443})
	if err != nil {
		t.Fatal(err)
	}
	got := pt.URLs("tok")
	if pt.Mode != ModeLANLoopback || len(got) != 1 || got[0] != "https://127.0.0.1:18443/pair/tok" {
		t.Fatalf("mode=%q urls=%#v", pt.Mode, got)
	}
}

func TestLANResolveFailsEarlyWithoutRoutableHost(t *testing.T) {
	_, err := Resolve(t.Context(), ResolverOptions{
		Mode:         "lan",
		Port:         18443,
		LANHosts:     []string{"127.0.0.1", "::1"},
		FallbackHost: "localhost",
	})
	if err == nil {
		t.Fatal("expected LAN reachability error")
	}
	var diag *DiagnosticError
	if !strings.Contains(err.Error(), "iPhone hotspot") || !strings.Contains(err.Error(), "--transport=tailscale-private") {
		t.Fatalf("err = %v", err)
	}
	if !errors.As(err, &diag) || diag.Code != DiagLANUnreachable {
		t.Fatalf("diagnostic = %#v err=%v", diag, err)
	}
}

func TestLANResolveAllowsHostnameFallback(t *testing.T) {
	pt, err := Resolve(t.Context(), ResolverOptions{
		Mode:         "lan",
		Port:         18443,
		FallbackHost: "host.local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pt.URLs("tok"); len(got) != 1 || got[0] != "https://host.local:18443/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
}

func TestLANResolveFiltersUnroutableTargets(t *testing.T) {
	pt, err := Resolve(t.Context(), ResolverOptions{
		Mode:         "lan",
		Port:         18443,
		LANHosts:     []string{"127.0.0.1", "169.254.1.2", "172.20.10.2"},
		FallbackHost: "localhost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pt.URLs("tok"); len(got) != 1 || got[0] != "https://172.20.10.2:18443/pair/tok" {
		t.Fatalf("urls=%#v", got)
	}
}

func TestLANLifecycleIPv4IPv6AndHotspotContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
		pair string
	}{
		{name: "ipv4", host: "192.168.1.20", pair: "https://192.168.1.20:18443/pair/pair-token"},
		{name: "ipv6", host: "fd00::20", pair: "https://[fd00::20]:18443/pair/pair-token"},
		{name: "hotspot", host: "172.20.10.2", pair: "https://172.20.10.2:18443/pair/pair-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := NewLifecycle(ResolverOptions{Mode: "lan", Port: 18443, LANHosts: []string{tc.host}})
			if _, err := session.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if health, err := session.Health(t.Context()); err != nil || !health.Healthy || health.State != LifecycleHealthy {
				t.Fatalf("health=%#v err=%v", health, err)
			}
			if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != tc.pair {
				t.Fatalf("urls=%#v err=%v", urls, err)
			}
			candidate, err := session.Enrollment()
			if err != nil || !candidate.RequiresOwnerProof || candidate.Endpoint.Kind != "mesh" || candidate.Endpoint.URL != strings.TrimSuffix(tc.pair, "/pair/pair-token") {
				t.Fatalf("candidate=%#v err=%v", candidate, err)
			}
			if _, err := session.Reconnect(t.Context()); err != nil {
				t.Fatal(err)
			}
			if health, err := session.Health(t.Context()); err != nil || !health.Healthy || health.State != LifecycleHealthy {
				t.Fatalf("restarted health=%#v err=%v", health, err)
			}
			if urls, err := session.Pair("pair-token"); err != nil || len(urls) != 1 || urls[0] != tc.pair {
				t.Fatalf("restarted urls=%#v err=%v", urls, err)
			}
			if err := session.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := session.Health(t.Context()); err == nil {
				t.Fatal("expected stopped lifecycle health failure")
			}
		})
	}
}

func TestLANClientIsolationDiagnosticFailsClosed(t *testing.T) {
	_, err := Resolve(t.Context(), ResolverOptions{
		Mode:         "lan",
		Port:         18443,
		LANHosts:     []string{"127.0.0.1", "::1", "169.254.1.2"},
		FallbackHost: "localhost",
	})
	var diagnostic *DiagnosticError
	if err == nil || !errors.As(err, &diagnostic) || diagnostic.Code != DiagLANUnreachable || !strings.Contains(err.Error(), "client isolation") {
		t.Fatalf("err=%v diagnostic=%#v", err, diagnostic)
	}
}

func TestResolvedTargetURLsUseProviderBaseURL(t *testing.T) {
	pt := Resolved{Mode: ModeNgrok, BaseURL: "https://dev.tail.ts.net/"}
	got := pt.TargetURLs()
	if len(got) != 1 || got[0] != "https://dev.tail.ts.net" {
		t.Fatalf("urls = %#v", got)
	}
}
