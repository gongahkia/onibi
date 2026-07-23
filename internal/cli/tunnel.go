package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/web"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
)

const tunnelTransportModes = "lan, lan-loopback, tailscale-private, wireguard, zerotier, cloudflare-quick, ngrok, auto"

func tunnelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tunnel <port>",
		Short: "Expose an arbitrary local HTTPS port",
		Args:  cobra.ExactArgs(1),
		RunE:  runTunnel,
	}
	cmd.Flags().String("transport", "", "tunnel transport: "+tunnelTransportModes)
	cmd.Flags().Bool("no-qr", false, "print URL without QR")
	return cmd
}

func runTunnel(cmd *cobra.Command, args []string) error {
	port, err := parseTunnelPort(args[0])
	if err != nil {
		return err
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	if transport, _ := cmd.Flags().GetString("transport"); strings.TrimSpace(transport) != "" {
		if err := config.Set(&cfg, "transport.mode", transport); err != nil {
			return err
		}
	} else {
		selected, prompted, err := promptTunnelTransport(cmd, cfg.Transport.Mode)
		if err != nil {
			return err
		}
		if prompted {
			if err := config.Set(&cfg, "transport.mode", selected); err != nil {
				return err
			}
		}
	}
	if !tunnelTransportSupported(cfg.Transport.Mode) {
		return fmt.Errorf("transport %q does not support ad-hoc web tunnels", cfg.Transport.Mode)
	}
	logger := logging.New(cmd.ErrOrStderr(), commandLogLevel(cmd)).With("component", "tunnel")
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	lanHosts := web.LANHosts()
	preferredHost := web.PreferredHost()
	started := time.Now()
	logger.Info("onibi tunnel starting", "port", port, "transport", cfg.Transport.Mode)
	tunnel, err := resolvePairTransport(ctx, cfg.Transport.Mode, port, lanHosts, preferredHost, logger)
	if err != nil {
		return err
	}
	defer cleanupTunnelTransport(logger, tunnel)
	urls := tunnel.TargetURLs()
	if len(urls) == 0 {
		return errors.New("tunnel produced no URL")
	}
	url := urls[0]
	logger.Info("onibi tunnel ready", "transport", tunnel.Mode, "url", url, "uptime_ms", time.Since(started).Milliseconds())
	if quiet(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), url)
	} else {
		printCLIHeader(cmd, "Onibi tunnel")
		fmt.Fprintln(cmd.OutOrStdout(), "Tunnel URL:", url)
		fmt.Fprintln(cmd.OutOrStdout(), "Local port:", port)
		fmt.Fprintln(cmd.OutOrStdout(), "Transport:", tunnel.Mode)
		if debug(cmd) {
			for _, alt := range urls[1:] {
				fmt.Fprintln(cmd.OutOrStdout(), "Fallback:", alt)
			}
		}
		if noQR, _ := cmd.Flags().GetBool("no-qr"); !noQR {
			if err := setup.PrintQR(cmd.OutOrStdout(), url); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Forwarding. Press Ctrl-C to stop.")
	<-ctx.Done()
	return nil
}

func parseTunnelPort(raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q", raw)
	}
	return port, nil
}

func tunnelTransportSupported(mode string) bool {
	switch webtransport.NormalizeMode(mode) {
	case webtransport.ModeLAN, webtransport.ModeLANLoopback, webtransport.ModeTailscalePrivate, webtransport.ModeWireGuard, webtransport.ModeZeroTier, webtransport.ModeCloudflareQuick, webtransport.ModeNgrok, webtransport.ModeAuto:
		return true
	default:
		return false
	}
}

func promptTunnelTransport(cmd *cobra.Command, current string) (string, bool, error) {
	if !shouldPromptPairTransport(cmd) {
		return current, false, nil
	}
	current = normalizeTunnelTransport(current)
	choices := tunnelTransportChoices(current)
	style := styleFor(cmd)
	rows := [][]string{tableHeader(style, "#", "PROVIDER", "BEST FOR", "COVERAGE", "COMMAND")}
	defaultKey := "1"
	for _, c := range choices {
		if c.active {
			defaultKey = c.key
		}
		rows = append(rows, []string{c.key, c.label, c.detail, c.coverage, strings.Replace(c.command, "onibi start", "onibi transport expose <port>", 1)})
	}
	fmt.Fprintln(cmd.OutOrStdout(), style.bold("Tunnel provider"))
	if err := renderTable(cmd.OutOrStdout(), rows); err != nil {
		return "", true, err
	}
	sc := bufio.NewScanner(cmd.InOrStdin())
	for {
		fmt.Fprintf(cmd.OutOrStdout(), "Select provider [%s]: ", defaultKey)
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
		for _, c := range choices {
			if raw == c.key || raw == c.mode || raw == strings.ToLower(c.label) {
				return c.mode, true, nil
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Choose 1 through %d, or q.\n", len(choices))
	}
}

func tunnelTransportChoices(current string) []pairTransportChoice {
	var out []pairTransportChoice
	key := 1
	for _, c := range pairTransportChoices(current) {
		if !tunnelTransportSupported(c.mode) {
			continue
		}
		c.key = strconv.Itoa(key)
		out = append(out, c)
		key++
	}
	return out
}

func normalizeTunnelTransport(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if tunnelTransportSupported(mode) {
		return mode
	}
	return "lan"
}

func cleanupTunnelTransport(logger *slog.Logger, tunnel webtransport.Resolved) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tunnel.Disable(ctx); err != nil {
		logger.Warn("tunnel cleanup failed", "transport", tunnel.Mode, "err", err)
	}
}
