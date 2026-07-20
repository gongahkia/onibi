package transport

import (
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
