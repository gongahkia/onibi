package buildinfo

import (
	"encoding/base64"
	"strings"
)

var (
	Version             = "v0.3.0"
	Commit              = "unknown"
	Date                = "unknown"
	ReleasePublicKeyB64 = ""
)

func ReleasePublicKey() string {
	raw := strings.TrimSpace(ReleasePublicKeyB64)
	if raw == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(decoded))
}
