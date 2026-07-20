package transport

import (
	"context"
	"strings"
	"testing"
)

func TestSupportedModeListExcludesDeferredProviders(t *testing.T) {
	got := SupportedModeList()
	for _, provider := range []string{"telegram"} {
		if strings.Contains(got, provider) {
			t.Fatalf("supported modes expose deferred provider %q: %s", provider, got)
		}
	}
}

func TestResolveRejectsRemovedCloudflareNamed(t *testing.T) {
	_, err := Resolve(context.Background(), ResolverOptions{Mode: "cloudflare-named", Port: 8443})
	if err == nil || !strings.Contains(err.Error(), "has been removed") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRejectsRemovedPublicTailscale(t *testing.T) {
	_, err := Resolve(context.Background(), ResolverOptions{Mode: "tailscale", Port: 8443})
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("err = %v", err)
	}
}
