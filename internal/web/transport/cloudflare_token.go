//go:build !onibi_remote

package transport

import (
	"os"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
)

func cloudflareAPIToken() string {
	if token := strings.TrimSpace(os.Getenv(CloudflareAPITokenEnv)); token != "" {
		return token
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return ""
	}
	st, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return ""
	}
	token, ok, err := st.Get(CloudflareSecretAPIToken)
	if err != nil || !ok {
		return ""
	}
	return strings.TrimSpace(token)
}
