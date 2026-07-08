//go:build !onibi_remote

package transport

import (
	"os"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
)

var openNgrokSecretStore = secrets.Open

func ngrokAuthtoken() string {
	if token := strings.TrimSpace(os.Getenv(NgrokAuthtokenEnv)); token != "" {
		return token
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return ""
	}
	st, err := openNgrokSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return ""
	}
	token, ok, err := st.Get(NgrokSecretAuthtoken)
	if err != nil || !ok {
		return ""
	}
	return strings.TrimSpace(token)
}
