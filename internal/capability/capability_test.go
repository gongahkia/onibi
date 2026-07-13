package capability

import "testing"

func TestV1AgentsAreExactAndNormalized(t *testing.T) {
	for _, agent := range []string{"claude", "Codex", " PI "} {
		if !IsV1Agent(agent) {
			t.Fatalf("IsV1Agent(%q) = false", agent)
		}
	}
	for _, agent := range []string{"amp", "copilot", "gemini", "goose", "opencode", "shell"} {
		if IsV1Agent(agent) {
			t.Fatalf("IsV1Agent(%q) = true", agent)
		}
	}
}

func TestV1WebTransportsExcludeProviders(t *testing.T) {
	for _, mode := range []string{"lan", "Tailscale", "wireguard", "zerotier", "cloudflare-quick", "cloudflare-named", "ngrok", "auto"} {
		if !IsV1WebTransport(mode) {
			t.Fatalf("IsV1WebTransport(%q) = false", mode)
		}
	}
	for _, mode := range DeferredProviderTransports() {
		if IsV1WebTransport(mode) {
			t.Fatalf("IsV1WebTransport(%q) = true", mode)
		}
		if !IsDeferredProviderTransport(mode) {
			t.Fatalf("IsDeferredProviderTransport(%q) = false", mode)
		}
	}
}

func TestCapabilityListsAreCopies(t *testing.T) {
	agents := V1Agents()
	agents[0] = "changed"
	if !IsV1Agent(AgentClaude) {
		t.Fatal("V1Agents mutated package state")
	}
	transports := V1WebTransports()
	transports[0] = "changed"
	if !IsV1WebTransport("lan") {
		t.Fatal("V1WebTransports mutated package state")
	}
}
