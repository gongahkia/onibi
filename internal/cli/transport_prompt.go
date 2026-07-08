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

type pairTransportCategory struct {
	key      string
	category string
	label    string
	detail   string
	status   string
	active   bool
}

type unavailableTransportChoice struct {
	label  string
	detail string
}

const (
	transportCategoryWeb    = "web"
	transportCategoryChat   = "chat"
	transportCategoryNotify = "notify"
)

func promptPairTransport(cmd *cobra.Command, current string) (string, bool, error) {
	if !shouldPromptPairTransport(cmd) {
		return current, false, nil
	}
	current = normalizePairTransport(current)
	style := styleFor(cmd)
	sc := bufio.NewScanner(cmd.InOrStdin())
	for {
		category, err := promptTransportCategory(cmd, sc, current, style)
		if err != nil {
			return "", true, err
		}
		selected, back, err := promptTransportProvider(cmd, sc, current, category, style)
		if err != nil {
			return "", true, err
		}
		if back {
			continue
		}
		return selected, true, nil
	}
}

func shouldPromptPairTransport(cmd *cobra.Command) bool {
	return !quiet(cmd) && inputIsTerminal(cmd.InOrStdin()) && outputIsTerminal(cmd.OutOrStdout())
}

func promptTransportCategory(cmd *cobra.Command, sc *bufio.Scanner, current string, style cliStyle) (string, error) {
	choices := pairTransportCategoryChoices(current)
	fmt.Fprintln(cmd.OutOrStdout(), style.bold("Connection category"))
	rows := [][]string{tableHeader(style, "#", "CATEGORY", "BEST FOR", "STATUS")}
	defaultKey := "1"
	for _, c := range choices {
		if c.active {
			defaultKey = c.key
		}
		rows = append(rows, []string{c.key, c.label, c.detail, c.status})
	}
	if err := renderTable(cmd.OutOrStdout(), rows); err != nil {
		return "", err
	}
	fmt.Fprintln(cmd.OutOrStdout())
	for {
		fmt.Fprintf(cmd.OutOrStdout(), "Select category [%s]: ", defaultKey)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return "", err
			}
			return categoryForTransportKey(choices, defaultKey), nil
		}
		raw := strings.ToLower(strings.TrimSpace(sc.Text()))
		if raw == "" {
			return categoryForTransportKey(choices, defaultKey), nil
		}
		if raw == "q" || raw == "quit" {
			return "", fmt.Errorf("transport selection cancelled")
		}
		for _, c := range choices {
			if raw == c.key || raw == c.category || raw == strings.ToLower(c.label) {
				return c.category, nil
			}
		}
		switch {
		case raw == "url" || raw == "browser" || strings.Contains(raw, "web") || strings.Contains(raw, "cloudflare") || raw == "ngrok":
			return transportCategoryWeb, nil
		case strings.Contains(raw, "chat") || raw == "telegram" || raw == "slack" || raw == "discord" || raw == "matrix":
			return transportCategoryChat, nil
		case strings.Contains(raw, "notify") || raw == "pushover":
			return transportCategoryNotify, nil
		default:
			fmt.Fprintln(cmd.OutOrStdout(), "Choose 1, 2, 3, or q.")
		}
	}
}

func promptTransportProvider(cmd *cobra.Command, sc *bufio.Scanner, current string, category string, style cliStyle) (string, bool, error) {
	choices := pairTransportChoices(current, category)
	title := "Web URL provider"
	if category == transportCategoryChat {
		title = "Chat provider"
	} else if category == transportCategoryNotify {
		title = "Notify-only provider"
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), style.bold(title))
	rows := [][]string{tableHeader(style, "#", "PROVIDER", "BEST FOR", "COVERAGE", "COMMAND")}
	defaultKey := "1"
	for _, c := range choices {
		if c.active {
			defaultKey = c.key
		}
		rows = append(rows, []string{c.key, c.label, c.detail, c.coverage, c.command})
	}
	for _, c := range unavailableTransportChoices(category) {
		rows = append(rows, []string{"-", c.label, c.detail, "-", "not in this build"})
	}
	if err := renderTable(cmd.OutOrStdout(), rows); err != nil {
		return "", false, err
	}
	fmt.Fprintln(cmd.OutOrStdout())
	for {
		fmt.Fprintf(cmd.OutOrStdout(), "Select provider [%s]: ", defaultKey)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return "", false, err
			}
			return modeForTransportKey(choices, defaultKey), false, nil
		}
		raw := strings.ToLower(strings.TrimSpace(sc.Text()))
		if raw == "" {
			return modeForTransportKey(choices, defaultKey), false, nil
		}
		if raw == "q" || raw == "quit" {
			return "", false, fmt.Errorf("transport selection cancelled")
		}
		if raw == "b" || raw == "back" {
			return "", true, nil
		}
		if unavailableProviderSelected(raw) {
			fmt.Fprintln(cmd.OutOrStdout(), "That provider is not enabled in this build; choose a supported provider, b, or q.")
			continue
		}
		for _, c := range choices {
			if raw == c.key || raw == c.mode || raw == strings.ToLower(c.label) {
				return c.mode, false, nil
			}
		}
		if category == transportCategoryWeb {
			fmt.Fprintln(cmd.OutOrStdout(), "Choose 1, 2, 3, 4, 5, 6, 7, 8, b, or q.")
		} else if category == transportCategoryChat {
			fmt.Fprintln(cmd.OutOrStdout(), "Choose 1, 2, 3, 4, b, or q.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Choose 1, 2, 3, b, or q.")
		}
	}
}

func pairTransportCategoryChoices(current string) []pairTransportCategory {
	active := categoryForTransport(current)
	return []pairTransportCategory{
		{
			key:      "1",
			category: transportCategoryWeb,
			label:    "Web URL",
			detail:   "browser cockpit + QR",
			status:   "LAN, Tailscale, WireGuard, ZeroTier, Cloudflare, ngrok, Auto",
			active:   active == transportCategoryWeb,
		},
		{
			key:      "2",
			category: transportCategoryChat,
			label:    "Chat",
			detail:   "natural text control",
			status:   "Telegram, Matrix, Slack, Discord",
			active:   active == transportCategoryChat,
		},
		{
			key:      "3",
			category: transportCategoryNotify,
			label:    "Notify-only",
			detail:   "approvals + alerts",
			status:   "Pushover, ntfy, Gotify",
			active:   active == transportCategoryNotify,
		},
	}
}

func pairTransportChoices(current string, category string) []pairTransportChoice {
	if category == transportCategoryChat {
		return []pairTransportChoice{
			{
				key:      "1",
				mode:     "telegram",
				label:    "Telegram",
				detail:   "chat-native text control",
				coverage: "unit + fake API + live opt-in",
				command:  "onibi up --transport=telegram",
				active:   current == "telegram",
			},
			{
				key:      "2",
				mode:     "matrix",
				label:    "Matrix",
				detail:   "federated room control",
				coverage: "unit + daemon conformance + live opt-in",
				command:  "onibi up --transport=matrix",
				active:   current == "matrix",
			},
			{
				key:      "3",
				mode:     "slack",
				label:    "Slack",
				detail:   "Socket Mode workspace control",
				coverage: "unit + fake socket + live opt-in",
				command:  "onibi up --transport=slack",
				active:   current == "slack",
			},
			{
				key:      "4",
				mode:     "discord",
				label:    "Discord",
				detail:   "DM/guild bot control",
				coverage: "unit + fake gateway + live opt-in",
				command:  "onibi up --transport=discord",
				active:   current == "discord",
			},
		}
	}
	if category == transportCategoryNotify {
		return []pairTransportChoice{
			{
				key:      "1",
				mode:     "pushover",
				label:    "Pushover",
				detail:   "approval push notifications",
				coverage: "unit + receipt audit + live opt-in",
				command:  "onibi up --transport=pushover",
				active:   current == "pushover",
			},
			{
				key:      "2",
				mode:     "ntfy",
				label:    "ntfy",
				detail:   "topic publish/subscribe",
				coverage: "unit + fake WS + live opt-in",
				command:  "onibi up --transport=ntfy",
				active:   current == "ntfy",
			},
			{
				key:      "3",
				mode:     "gotify",
				label:    "Gotify",
				detail:   "self-hosted notifications",
				coverage: "unit + REST/WS fake + live opt-in",
				command:  "onibi up --transport=gotify",
				active:   current == "gotify",
			},
		}
	}
	return []pairTransportChoice{
		{
			key:      "1",
			mode:     "lan",
			label:    "LAN / hotspot",
			detail:   "same Wi-Fi or phone hotspot",
			coverage: "unit + local integration + manual device",
			command:  "onibi up --transport=lan",
			active:   current == "lan",
		},
		{
			key:      "2",
			mode:     "tailscale",
			label:    "Tailscale Funnel",
			detail:   "cellular via *.ts.net",
			coverage: "unit + fake runner + live opt-in",
			command:  "onibi up --transport=tailscale",
			active:   current == "tailscale",
		},
		{
			key:      "3",
			mode:     "wireguard",
			label:    "WireGuard",
			detail:   "self-hosted mesh VPN",
			coverage: "unit + interface probe + live opt-in",
			command:  "onibi up --transport=wireguard",
			active:   current == "wireguard",
		},
		{
			key:      "4",
			mode:     "zerotier",
			label:    "ZeroTier",
			detail:   "self-hosted mesh overlay",
			coverage: "unit + CLI probe + live opt-in",
			command:  "onibi up --transport=zerotier",
			active:   current == "zerotier",
		},
		{
			key:      "5",
			mode:     "cloudflare-quick",
			label:    "Cloudflare Quick",
			detail:   "temporary trycloudflare URL",
			coverage: "unit + fake process + live opt-in",
			command:  "onibi up --transport=cloudflare-quick",
			active:   current == "cloudflare-quick",
		},
		{
			key:      "6",
			mode:     "cloudflare-named",
			label:    "Cloudflare Named",
			detail:   "configured public hostname",
			coverage: "unit + fake process + live opt-in",
			command:  "onibi up --transport=cloudflare-named",
			active:   current == "cloudflare-named",
		},
		{
			key:      "7",
			mode:     "ngrok",
			label:    "ngrok",
			detail:   "dev/demo public URL",
			coverage: "unit + fake agent API + live opt-in",
			command:  "onibi up --transport=ngrok",
			active:   current == "ngrok",
		},
		{
			key:      "8",
			mode:     "auto",
			label:    "Auto",
			detail:   "try Tailscale, fallback LAN",
			coverage: "Tailscale -> LAN only",
			command:  "onibi up --transport=auto",
			active:   current == "auto",
		},
	}
}

func unavailableTransportChoices(category string) []unavailableTransportChoice {
	switch category {
	case transportCategoryWeb:
		return nil
	case transportCategoryChat:
		return nil
	case transportCategoryNotify:
		return nil
	default:
		return nil
	}
}

func normalizePairTransport(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "tailscale", "wireguard", "zerotier", "cloudflare-quick", "cloudflare-named", "ngrok", "telegram", "matrix", "slack", "discord", "pushover", "ntfy", "gotify", "auto":
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

func categoryForTransport(mode string) string {
	switch normalizePairTransport(mode) {
	case "telegram", "matrix", "slack", "discord":
		return transportCategoryChat
	case "pushover", "ntfy", "gotify":
		return transportCategoryNotify
	default:
		return transportCategoryWeb
	}
}

func categoryForTransportKey(choices []pairTransportCategory, key string) string {
	for _, c := range choices {
		if c.key == key {
			return c.category
		}
	}
	return transportCategoryWeb
}

func unavailableProviderSelected(raw string) bool {
	_ = raw
	return false
}
