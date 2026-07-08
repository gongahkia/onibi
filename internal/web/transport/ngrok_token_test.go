//go:build !onibi_remote

package transport

import (
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
)

func TestNgrokAuthtokenReadsSecretStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	t.Setenv(NgrokAuthtokenEnv, "")
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	old := openNgrokSecretStore
	openNgrokSecretStore = func(opts secrets.Options) (*secrets.Store, error) {
		opts.PreferDotenv = true
		return secrets.Open(opts)
	}
	t.Cleanup(func() { openNgrokSecretStore = old })
	st, err := openNgrokSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(NgrokSecretAuthtoken, "ngrok-token-1234567890"); err != nil {
		t.Fatal(err)
	}
	if got := NewNgrokFromEnv().Authtoken; got != "ngrok-token-1234567890" {
		t.Fatalf("authtoken = %q", got)
	}
}
