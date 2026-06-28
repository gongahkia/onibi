package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type pairTransportChoice struct {
	key     string
	mode    string
	label   string
	detail  string
	command string
	active  bool
}

func promptPairTransport(cmd *cobra.Command, current string) (string, bool, error) {
	if !shouldPromptPairTransport(cmd) {
		return current, false, nil
	}
	choices := pairTransportChoices(normalizePairTransport(current))
	style := styleFor(cmd)
	fmt.Fprintln(cmd.OutOrStdout(), style.bold("Connection"))
	rows := [][]string{tableHeader(style, "#", "TRANSPORT", "BEST FOR", "COMMAND")}
	defaultKey := "1"
	for _, c := range choices {
		if c.active {
			defaultKey = c.key
		}
		rows = append(rows, []string{c.key, c.label, c.detail, c.command})
	}
	rows = append(rows, []string{"-", "Cloudflare", "future relay transports", "not in this build"})
	if err := renderTable(cmd.OutOrStdout(), rows); err != nil {
		return "", false, err
	}
	fmt.Fprintln(cmd.OutOrStdout())
	sc := bufio.NewScanner(cmd.InOrStdin())
	for {
		fmt.Fprintf(cmd.OutOrStdout(), "Select transport [%s]: ", defaultKey)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return "", true, err
			}
			return modeForTransportKey(choices, defaultKey), true, nil
		}
		raw := strings.ToLower(strings.TrimSpace(sc.Text()))
		if raw == "" {
			return modeForTransportKey(choices, defaultKey), true, nil
		}
		if raw == "q" || raw == "quit" {
			return "", true, fmt.Errorf("transport selection cancelled")
		}
		if strings.Contains(raw, "cloudflare") || raw == "4" {
			fmt.Fprintln(cmd.OutOrStdout(), "Cloudflare transports are not enabled in this build; choose 1-3.")
			continue
		}
		for _, c := range choices {
			if raw == c.key || raw == c.mode {
				return c.mode, true, nil
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Choose 1, 2, 3, or q.")
	}
}

func shouldPromptPairTransport(cmd *cobra.Command) bool {
	return !quiet(cmd) && inputIsTerminal(cmd.InOrStdin()) && outputIsTerminal(cmd.OutOrStdout())
}

func pairTransportChoices(current string) []pairTransportChoice {
	return []pairTransportChoice{
		{
			key:     "1",
			mode:    "lan",
			label:   "LAN / hotspot",
			detail:  "same Wi-Fi or phone hotspot",
			command: "onibi up --transport=lan",
			active:  current == "lan",
		},
		{
			key:     "2",
			mode:    "tailscale",
			label:   "Tailscale Funnel",
			detail:  "cellular via *.ts.net",
			command: "onibi up --transport=tailscale",
			active:  current == "tailscale",
		},
		{
			key:     "3",
			mode:    "auto",
			label:   "Auto",
			detail:  "try Tailscale, fallback LAN",
			command: "onibi up --transport=auto",
			active:  current == "auto",
		},
	}
}

func normalizePairTransport(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "tailscale", "auto":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "lan"
	}
}

func modeForTransportKey(choices []pairTransportChoice, key string) string {
	for _, c := range choices {
		if c.key == key {
			return c.mode
		}
	}
	return "lan"
}
