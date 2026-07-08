package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/secrets"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
)

func TestCloudflareSetupStatusDisableCLI(t *testing.T) {
	paths := withDefaultState(t)
	withDotenvSecretStore(t)
	t.Setenv(webtransport.CloudflareAccountIDEnv, "account-1")
	t.Setenv(webtransport.CloudflareTunnelIDEnv, "tunnel-id")
	t.Setenv(webtransport.CloudflareTunnelNameEnv, "named")
	t.Setenv(webtransport.CloudflareHostnameEnv, "app.example.com")

	out, _ := executeRoot(t, "cloudflare", "setup", "--api-token", "cf-api-token-1234567890", "--color", "never")
	if !strings.Contains(out.String(), "Stored Cloudflare API token") {
		t.Fatalf("setup output:\n%s", out.String())
	}
	out, _ = executeRoot(t, "cloudflare", "status", "--json", "--color", "never")
	var status cloudflareStatusReport
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("status json: %v\n%s", err, out.String())
	}
	if !status.APIToken || !status.AccountID || !status.TunnelID || !status.TunnelName || !status.Hostname {
		t.Fatalf("status = %+v", status)
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := st.Get(webtransport.CloudflareSecretAPIToken); err != nil || !ok || got != "cf-api-token-1234567890" {
		t.Fatalf("stored token got=%q ok=%v err=%v", got, ok, err)
	}
	if err := st.Set(webtransport.CloudflareLegacySecretAPIToken, "legacy-token"); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "cloudflare", "disable", "--color", "never")
	for _, key := range []string{webtransport.CloudflareSecretAPIToken, webtransport.CloudflareLegacySecretAPIToken} {
		if _, ok, err := st.Get(key); err != nil || ok {
			t.Fatalf("%s after disable ok=%v err=%v", key, ok, err)
		}
	}
}
