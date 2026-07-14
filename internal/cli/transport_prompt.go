package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type pairTransportChoice struct {
	key      string
	mode     string
	label    string
	detail   string
	coverage string
	command  string
	active   bool
}

func promptPairTransport(cmd *cobra.Command, current string) (string, bool, error) {
	if !shouldPromptPairTransport(cmd) {
		return current, false, nil
	}
	selected, err := promptWebTransport(cmd, bufio.NewScanner(cmd.InOrStdin()), current, styleFor(cmd))
	return selected, true, err
}

func shouldPromptPairTransport(cmd *cobra.Command) bool {
	return !quiet(cmd) && inputIsTerminal(cmd.InOrStdin()) && outputIsTerminal(cmd.OutOrStdout())
}

func promptWebTransport(cmd *cobra.Command, sc *bufio.Scanner, current string, style cliStyle) (string, error) {
	current = normalizePairTransport(current)
	choices := pairTransportChoices(current)
	rows := [][]string{tableHeader(style, "#", "TRANSPORT", "BEST FOR", "COVERAGE", "COMMAND")}
	defaultKey := "1"
	for _, c := range choices {
		if c.active {
			defaultKey = c.key
		}
		rows = append(rows, []string{c.key, c.label, c.detail, c.coverage, c.command})
	}
	fmt.Fprintln(cmd.OutOrStdout(), style.bold("Web cockpit transport"))
	if err := renderTable(cmd.OutOrStdout(), rows); err != nil {
		return "", err
	}
	fmt.Fprintln(cmd.OutOrStdout())
	for {
		fmt.Fprintf(cmd.OutOrStdout(), "Select transport [%s]: ", defaultKey)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return "", err
			}
			return modeForTransportKey(choices, defaultKey), nil
		}
		raw := strings.ToLower(strings.TrimSpace(sc.Text()))
		if raw == "" {
			return modeForTransportKey(choices, defaultKey), nil
		}
		if raw == "q" || raw == "quit" {
			return "", fmt.Errorf("transport selection cancelled")
		}
		for _, c := range choices {
			if raw == c.key || raw == c.mode || raw == strings.ToLower(c.label) {
				return c.mode, nil
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Choose 1 through %d, or q.\n", len(choices))
	}
}

func pairTransportChoices(current string) []pairTransportChoice {
	return []pairTransportChoice{
		{key: "1", mode: "lan", label: "LAN / hotspot", detail: "same Wi-Fi or phone hotspot", coverage: "unit + local integration + manual device", command: "onibi up --transport=lan", active: current == "lan"},
		{key: "2", mode: "tailscale", label: "Tailscale Funnel", detail: "cellular via *.ts.net", coverage: "unit + fake runner + live opt-in", command: "onibi up --transport=tailscale", active: current == "tailscale"},
		{key: "3", mode: "tailscale-private", label: "Tailscale Serve", detail: "private tailnet HTTPS", coverage: "unit + fake runner", command: "onibi up --transport=tailscale-private", active: current == "tailscale-private"},
		{key: "4", mode: "wireguard", label: "WireGuard", detail: "self-hosted mesh VPN", coverage: "unit + interface probe + live opt-in", command: "onibi up --transport=wireguard", active: current == "wireguard"},
		{key: "5", mode: "zerotier", label: "ZeroTier", detail: "self-hosted mesh overlay", coverage: "unit + CLI probe + live opt-in", command: "onibi up --transport=zerotier", active: current == "zerotier"},
		{key: "6", mode: "cloudflare-quick", label: "Cloudflare Quick", detail: "temporary trycloudflare URL", coverage: "unit + fake process + live opt-in", command: "onibi up --transport=cloudflare-quick", active: current == "cloudflare-quick"},
		{key: "7", mode: "cloudflare-named", label: "Cloudflare Named", detail: "configured public hostname", coverage: "unit + fake process + live opt-in", command: "onibi up --transport=cloudflare-named", active: current == "cloudflare-named"},
		{key: "8", mode: "ngrok", label: "ngrok", detail: "dev/demo public URL", coverage: "unit + fake agent API + live opt-in", command: "onibi up --transport=ngrok", active: current == "ngrok"},
		{key: "9", mode: "auto", label: "Auto", detail: "try Tailscale Funnel, fallback LAN", coverage: "Tailscale Funnel -> LAN only", command: "onibi up --transport=auto", active: current == "auto"},
	}
}

func normalizePairTransport(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	for _, choice := range pairTransportChoices("") {
		if mode == choice.mode {
			return mode
		}
	}
	return "lan"
}

func modeForTransportKey(choices []pairTransportChoice, key string) string {
	for _, c := range choices {
		if c.key == key {
			return c.mode
		}
	}
	return "lan"
}
