//go:build onibi_remote

package transport

import (
	"os"
	"strings"
)

func ngrokAuthtoken() string {
	return strings.TrimSpace(os.Getenv(NgrokAuthtokenEnv))
}
