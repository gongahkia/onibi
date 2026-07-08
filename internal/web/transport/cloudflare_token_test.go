//go:build !onibi_remote

package transport

import (
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
)

func TestCloudflareAPITokenReadsNewAndLegacySecretKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	t.Setenv(CloudflareAPITokenEnv, "")
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	old := openCloudflareSecretStore
	openCloudflareSecretStore = func(opts secrets.Options) (*secrets.Store, error) {
		opts.PreferDotenv = true
		return secrets.Open(opts)
	}
	t.Cleanup(func() { openCloudflareSecretStore = old })
	st, err := openCloudflareSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(CloudflareLegacySecretAPIToken, "legacy-token"); err != nil {
		t.Fatal(err)
	}
	if got := cloudflareAPIToken(); got != "legacy-token" {
		t.Fatalf("legacy token = %q", got)
	}
	if err := st.Set(CloudflareSecretAPIToken, "new-token"); err != nil {
		t.Fatal(err)
	}
	if got := cloudflareAPIToken(); got != "new-token" {
		t.Fatalf("new token = %q", got)
	}
}
