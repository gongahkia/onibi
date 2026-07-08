//go:build onibi_remote

package transport

import (
	"os"
	"strings"
)

func cloudflareAPIToken() string {
	return strings.TrimSpace(os.Getenv(CloudflareAPITokenEnv))
}
