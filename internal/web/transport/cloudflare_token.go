//go:build !onibi_remote

package transport

import (
	"os"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
)

var openCloudflareSecretStore = secrets.Open

func cloudflareAPIToken() string {
	if token := strings.TrimSpace(os.Getenv(CloudflareAPITokenEnv)); token != "" {
		return token
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return ""
	}
	st, err := openCloudflareSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return ""
	}
	for _, key := range []string{CloudflareSecretAPIToken, CloudflareLegacySecretAPIToken} {
		token, ok, err := st.Get(key)
		if err == nil && ok {
			return strings.TrimSpace(token)
		}
	}
	return ""
}
