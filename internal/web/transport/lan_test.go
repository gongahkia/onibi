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
	if !strings.Contains(err.Error(), "iPhone hotspot") || !strings.Contains(err.Error(), "--transport=tailscale") {
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

func TestResolvedTargetURLsUseProviderBaseURL(t *testing.T) {
	pt := Resolved{Mode: ModeTailscale, BaseURL: "https://dev.tail.ts.net/"}
	got := pt.TargetURLs()
	if len(got) != 1 || got[0] != "https://dev.tail.ts.net" {
		t.Fatalf("urls = %#v", got)
	}
}
