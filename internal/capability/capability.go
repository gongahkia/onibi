// Package capability defines the supported v1 product surface.
package capability

import "strings"

const (
	AgentClaude = "claude"
	AgentCodex  = "codex"
	AgentPi     = "pi"
)

var v1Agents = []string{AgentClaude, AgentCodex, AgentPi}

var v1WebTransports = []string{
	"lan",
	"tailscale",
	"tailscale-private",
	"wireguard",
	"zerotier",
	"cloudflare-quick",
	"cloudflare-named",
	"ngrok",
	"auto",
}

var deferredProviderTransports = []string{
	"telegram",
	"matrix",
	"slack",
	"discord",
	"zulip",
}

func V1Agents() []string { return append([]string(nil), v1Agents...) }

func V1WebTransports() []string { return append([]string(nil), v1WebTransports...) }

func DeferredProviderTransports() []string {
	return append([]string(nil), deferredProviderTransports...)
}

func IsV1Agent(name string) bool { return contains(v1Agents, name) }

func IsV1WebTransport(mode string) bool { return contains(v1WebTransports, mode) }

func IsInternalWebTransport(mode string) bool { return contains([]string{"lan-loopback"}, mode) }

func IsDeferredProviderTransport(mode string) bool {
	return contains(deferredProviderTransports, mode)
}

func contains(values []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
