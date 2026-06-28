package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
)

var installServiceRun = runInstallService
var webPairRun = runWebPairUp
var newTransportProviders = func() webtransport.ProviderFactory {
	return webtransport.ProviderFactory{
		Tailscale:       func() webtransport.Provider { return webtransport.NewTailscale() },
		CloudflareQuick: func() webtransport.Provider { return webtransport.NewCloudflareQuick() },
		CloudflareNamed: func() webtransport.Provider { return webtransport.NewCloudflareNamedFromEnv() },
		Ngrok:           func() webtransport.Provider { return webtransport.NewNgrokFromEnv() },
	}
}

func upCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "up",
		Aliases: []string{"start"},
		Short:   "Start the local web cockpit and print a pairing QR",
		RunE:    runUp,
	}
	cmd.Flags().String("transport", "", "pairing transport: "+webtransport.SupportedModeList())
	cmd.Flags().String("shell", "", "shell executable for local cockpit session")
	cmd.Flags().Bool("no-login-shell", false, "start shell without login argv")
	cmd.Flags().String("cwd", "", "working directory for spawned shell")
	cmd.Flags().Bool("visible", false, "open the managed session in a Mac terminal immediately")
	cmd.Flags().Bool("no-qr", false, "print pairing URL without QR")
	cmd.Flags().String("log-file", "", "also write up logs to this file")
	return cmd
}

func runUp(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()

	return webPairRun(cmd, paths, db)
}

func runWebPairUp(cmd *cobra.Command, paths config.Paths, db *store.DB) error {
	started := time.Now()
	debugMode := debug(cmd)
	level := commandLogLevel(cmd)
	logWriter := cmd.ErrOrStderr()
	if logFile, _ := cmd.Flags().GetString("log-file"); strings.TrimSpace(logFile) != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		logWriter = io.MultiWriter(logWriter, f)
	}
	logger := logging.New(logWriter, level).With("component", "up")
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cwd, _ := os.Getwd()
	logger.Info("onibi up starting",
		"version", buildinfo.Version,
		"commit", buildinfo.Commit,
		"build_date", buildinfo.Date,
		"pid", os.Getpid(),
		"goos", runtime.GOOS,
		"goarch", runtime.GOARCH,
		"cwd", cwd,
		"state_dir", paths.StateDir,
		"config_path", paths.Config,
		"db_path", paths.DBFile,
		"socket_path", paths.Socket,
	)

	phase := time.Now()
	cfg, meta, err := config.Load(paths)
	if err != nil {
		return err
	}
	logPhase(logger, "config_loaded", phase,
		"config_path", meta.Path,
		"config_exists", meta.Exists,
		"listen_addr", cfg.Web.ListenAddr,
		"transport", cfg.Transport.Mode,
		"shell_default", cfg.Shell.Default,
		"shell_login", cfg.Shell.Login,
		"approval_ttl", approval.DefaultTTL.String(),
	)
	headerPrinted := false
	if transport, _ := cmd.Flags().GetString("transport"); strings.TrimSpace(transport) != "" {
		if err := config.Set(&cfg, "transport.mode", transport); err != nil {
			return err
		}
		logger.Info("transport override applied", "transport", cfg.Transport.Mode)
	} else {
		if shouldPromptPairTransport(cmd) {
			printCLIHeader(cmd, "Onibi up")
			headerPrinted = true
		}
		selected, prompted, err := promptPairTransport(cmd, cfg.Transport.Mode)
		if err != nil {
			return err
		}
		if prompted {
			if err := config.Set(&cfg, "transport.mode", selected); err != nil {
				return err
			}
			logger.Info("transport selected", "transport", cfg.Transport.Mode)
		}
	}
	if shell, _ := cmd.Flags().GetString("shell"); strings.TrimSpace(shell) != "" {
		cfg.Shell.Default = strings.TrimSpace(shell)
	}
	if noLogin, _ := cmd.Flags().GetBool("no-login-shell"); noLogin {
		cfg.Shell.Login = false
	}
	shellCWD, err := shellWorkingDir(cmd)
	if err != nil {
		return err
	}
	if cfg.Transport.Mode == "telegram" {
		return runTelegramUp(cmd, paths, db, cfg, logger, started, shellCWD)
	}
	port, err := listenPort(cfg.Web.ListenAddr)
	if err != nil {
		return err
	}
	certDir := certDir(paths, cfg)
	phase = time.Now()
	cert, err := web.GenerateOrLoadCert(certDir)
	if err != nil {
		return err
	}
	certPaths := web.LocalCertPaths(certDir)
	logPhase(logger, "cert_ready", phase, certLogAttrs(cert, certPaths, certDir)...)
	lanHosts := web.LANHosts()
	preferredHost := web.PreferredHost()
	logger.Info("web pair server starting",
		"addr", cfg.Web.ListenAddr,
		"port", port,
		"transport", cfg.Transport.Mode,
		"state_dir", paths.StateDir,
		"cert_dir", certDir,
		"lan_hosts", strings.Join(lanHosts, ","),
		"preferred_host", preferredHost,
	)
	relayKeys := web.NewRelayKeys()
	d := daemon.New(daemon.Options{
		Paths:                 paths,
		DB:                    db,
		Log:                   logger,
		ApprovalTTL:           cfg.Daemon.ApprovalTimeout.Std(),
		ApprovalSweepInterval: cfg.Daemon.ApprovalSweepInterval.Std(),
		IdleThreshold:         cfg.Daemon.TurnIdleThreshold.Std(),
		IdleInterval:          cfg.Daemon.TurnIdleInterval.Std(),
		BufferSize:            cfg.Daemon.PTYBufferBytes,
		TerminalDefault:       cfg.Terminal.Default,
		WebAddr:               cfg.Web.ListenAddr,
		WebCertDir:            certDir,
		RelayKeys:             relayKeys,
		RequireWebE2E:         webtransport.IsRelayMode(cfg.Transport.Mode),
		SkipRestore:           true,
	})
	phase = time.Now()
	session, err := startManagedWebPairShell(ctx, d, cfg, shellCWD, logger)
	if err != nil {
		return err
	}
	sessionID := session.ID
	tmuxTarget := session.TmuxTarget
	defer cleanupManagedWebPairShell(logger, d, sessionID, tmuxTarget)
	logPhase(logger, "shell_ready", phase, "session_id", sessionID)
	visible, _ := cmd.Flags().GetBool("visible")
	if visible {
		if msg, err := d.ShowSession(ctx, sessionID); err != nil {
			logger.Warn("initial Mac handover failed", "session_id", sessionID, "err", err)
		} else {
			logger.Info("initial Mac handover complete", "session_id", sessionID, "message", msg)
		}
	} else if _, err := d.EnsureWebPTYHost(ctx, sessionID); err != nil {
		return err
	}
	errCh := make(chan error, 1)
	phase = time.Now()
	go func() { errCh <- d.Run(ctx) }()
	if err := waitForWebHealth(ctx, port, errCh, logger); err != nil {
		return err
	}
	logPhase(logger, "servers_ready", phase, "socket_path", paths.Socket)

	phase = time.Now()
	pairTransport, err := resolvePairTransport(ctx, cfg.Transport.Mode, port, lanHosts, preferredHost, logger)
	if err != nil {
		return err
	}
	defer cleanupPairTransport(logger, pairTransport)
	logPhase(logger, "pair_transport_ready", phase,
		"transport", pairTransport.Mode,
		"base_url", pairTransport.RedactedBaseURL(),
	)

	phase = time.Now()
	token, err := setup.NewToken(ctx, db)
	if err != nil {
		return err
	}
	urls := pairTransport.URLs(token)
	if webtransport.IsRelayMode(string(pairTransport.Mode)) {
		key, err := envelope.NewKey()
		if err != nil {
			return err
		}
		if err := relayKeys.RegisterPair(ctx, db, token, key, setup.PairTokenTTL); err != nil {
			return err
		}
		fragment := "k=" + envelope.EncodeKey(key)
		for i := range urls {
			urls[i] = appendURLFragment(urls[i], fragment)
		}
		logger.Info("relay e2e key registered", "transport", pairTransport.Mode, "commitment", envelope.Commitment(key))
	}
	url := urls[0]
	logger.Info("web pair token minted",
		"transport", pairTransport.Mode,
		"primary_url", redactPairURL(url),
		"fallback_urls", redactPairURLs(urls[1:]),
		"ttl", setup.PairTokenTTL.String(),
		"expires_at", time.Now().Add(setup.PairTokenTTL).UTC().Format(time.RFC3339Nano),
		"duration_ms", time.Since(phase).Milliseconds(),
	)
	style := styleFor(cmd)
	if quiet(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), url)
	} else {
		if !headerPrinted {
			printCLIHeader(cmd, "Onibi up")
		}
		if debugMode {
			_ = renderTable(cmd.OutOrStdout(), [][]string{
				{"web", style.status("PASS"), cfg.Web.ListenAddr},
				{"transport", style.status("INFO"), string(pairTransport.Mode)},
				{"shell", style.status("PASS"), sessionID},
				{"socket", style.status("PASS"), paths.Socket},
			})
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), style.bold("iPhone HTTPS trust"))
			fmt.Fprintln(cmd.OutOrStdout(), certPaths.MobileConfig)
			fmt.Fprintln(cmd.OutOrStdout(), "Install this profile and enable full trust if Safari warns or pairing returns Forbidden.")
			fmt.Fprintln(cmd.OutOrStdout())
		}
		fmt.Fprintln(cmd.OutOrStdout(), style.bold("Pair from phone"))
		fmt.Fprintln(cmd.OutOrStdout(), url)
		fmt.Fprintln(cmd.OutOrStdout(), "Expires:", setup.PairTokenTTL.String())
		if debugMode {
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
	fmt.Fprintln(cmd.OutOrStdout(), "Waiting for pairing. Press Ctrl-C to stop.")
	logger.Info("onibi up ready", "uptime_ms", time.Since(started).Milliseconds())

	select {
	case <-ctx.Done():
		logger.Info("onibi up stopping", "reason", "context_cancelled", "uptime", time.Since(started).Truncate(time.Millisecond).String())
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
				return err
			}
		case <-time.After(5 * time.Second):
			logger.Warn("onibi up shutdown timed out")
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled) {
			logger.Info("onibi up stopping", "reason", "server_closed", "uptime", time.Since(started).Truncate(time.Millisecond).String())
			return nil
		}
		logger.Error("onibi up server error", "err", err, "uptime", time.Since(started).Truncate(time.Millisecond).String())
		return err
	}
}

func webPairURLs(token string, port int, lanHosts []string, fallback string) []string {
	return webtransport.WebPairURLs(token, port, lanHosts, fallback)
}

func resolvePairTransport(ctx context.Context, mode string, port int, lanHosts []string, fallback string, logger *slog.Logger) (webtransport.Resolved, error) {
	return webtransport.Resolve(ctx, webtransport.ResolverOptions{
		Mode:         mode,
		Port:         port,
		LANHosts:     lanHosts,
		FallbackHost: fallback,
		Logger:       logger,
		Providers:    newTransportProviders(),
	})
}

func cleanupPairTransport(logger *slog.Logger, pt webtransport.Resolved) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pt.Disable(ctx); err != nil {
		logger.Warn("pair transport cleanup failed", "transport", pt.Mode, "err", err)
		return
	}
	switch pt.Mode {
	case webtransport.ModeTailscale:
		logger.Info("pair transport cleanup audit", "transport", pt.Mode, "action", "tailscale funnel --bg off")
	case webtransport.ModeCloudflareQuick, webtransport.ModeCloudflareNamed:
		logger.Info("pair transport cleanup audit", "transport", pt.Mode, "action", "cloudflared process kill")
	case webtransport.ModeNgrok:
		logger.Info("pair transport cleanup audit", "transport", pt.Mode, "action", "ngrok tunnel shutdown")
	}
}

func startManagedWebPairShell(ctx context.Context, d *daemon.Daemon, cfg config.Config, cwd string, logger *slog.Logger) (*daemon.Session, error) {
	launch, err := resolveShellLaunch(cfg.Shell.Default, cfg.Shell.Login, os.Getenv, exec.LookPath)
	if err != nil {
		return nil, err
	}
	if launch.Argv0 != "" {
		logger.Warn("tmux-backed shell ignores argv0 login hint", "shell", launch.Name, "argv0", launch.Argv0)
	}
	session, err := d.StartManagedTmuxSession(ctx, launch.Name, "shell", launch.Command, launch.Args, cwd)
	if err != nil {
		return nil, err
	}
	logger.Info("web pair shell started",
		"session_id", session.ID,
		"tmux_target", session.TmuxTarget,
		"shell", launch.Name,
		"command", launch.Command,
		"args", strings.Join(launch.Args, " "),
		"cwd", cwd,
		"login", cfg.Shell.Login,
	)
	return session, nil
}

func cleanupManagedWebPairShell(logger *slog.Logger, d *daemon.Daemon, sessionID, tmuxTarget string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := d.HideSession(ctx, sessionID, "end"); err != nil {
		logger.Warn("managed shell session cleanup failed", "session_id", sessionID, "tmux_target", tmuxTarget, "err", err)
	}
	cancel()
	if strings.TrimSpace(tmuxTarget) == "" {
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.KillTmuxTarget(ctx, tmuxTarget); err != nil && !tmuxTargetAlreadyGone(err) {
		logger.Warn("managed tmux target cleanup failed", "session_id", sessionID, "tmux_target", tmuxTarget, "err", err)
	}
}

func tmuxTargetAlreadyGone(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "can't find session") || strings.Contains(msg, "no server running")
}

func shellWorkingDir(cmd *cobra.Command) (string, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if strings.TrimSpace(cwd) == "" {
		return os.Getwd()
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--cwd is not a directory: %s", abs)
	}
	return abs, nil
}

func waitForWebHealth(ctx context.Context, port int, errCh <-chan error, logger *slog.Logger) error {
	client := &http.Client{
		Timeout: 250 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	started := time.Now()
	attempts := 0
	healthURL := fmt.Sprintf("https://127.0.0.1:%d/healthz", port)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-deadline.C:
			logger.Warn("web health timeout", "url", healthURL, "attempts", attempts, "duration_ms", time.Since(started).Milliseconds())
			return fmt.Errorf("web server did not become ready")
		case <-tick.C:
			attempts++
			resp, err := client.Get(healthURL)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					logger.Info("web health ready", "url", healthURL, "attempts", attempts, "duration_ms", time.Since(started).Milliseconds())
					return nil
				}
				if attempts == 1 || attempts%10 == 0 {
					logger.Debug("web health probe", "url", healthURL, "attempt", attempts, "status", resp.StatusCode)
				}
			} else if attempts == 1 || attempts%10 == 0 {
				logger.Debug("web health probe", "url", healthURL, "attempt", attempts, "err", err)
			}
		}
	}
}

func logPhase(logger *slog.Logger, phase string, started time.Time, attrs ...any) {
	fields := []any{"phase", phase, "duration_ms", time.Since(started).Milliseconds()}
	fields = append(fields, attrs...)
	logger.Info("up phase complete", fields...)
}

func certLogAttrs(cert tls.Certificate, paths web.CertPaths, certDir string) []any {
	attrs := []any{
		"cert_dir", certDir,
		"ca_cert", paths.CACert,
		"server_cert", paths.ServerCert,
		"mobileconfig", paths.MobileConfig,
	}
	if len(cert.Certificate) == 0 {
		return append(attrs, "cert_loaded", false)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return append(attrs, "cert_loaded", false, "cert_parse_err", err.Error())
	}
	var ips []string
	for _, ip := range leaf.IPAddresses {
		ips = append(ips, ip.String())
	}
	return append(attrs,
		"cert_loaded", true,
		"cert_subject", leaf.Subject.CommonName,
		"cert_not_before", leaf.NotBefore.UTC().Format(time.RFC3339Nano),
		"cert_not_after", leaf.NotAfter.UTC().Format(time.RFC3339Nano),
		"cert_dns_names", strings.Join(leaf.DNSNames, ","),
		"cert_ip_sans", strings.Join(ips, ","),
	)
}

func redactPairURLs(urls []string) []string {
	out := make([]string, len(urls))
	for i, url := range urls {
		out[i] = redactPairURL(url)
	}
	return out
}

func redactPairURL(url string) string {
	i := strings.Index(url, "/pair/")
	if i < 0 {
		return url
	}
	return url[:i] + "/pair/<redacted>"
}

func listenPort(addr string) (int, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return 0, fmt.Errorf("web.listen_addr required")
	}
	i := strings.LastIndex(addr, ":")
	if i < 0 || i == len(addr)-1 {
		return 0, fmt.Errorf("web.listen_addr must include port: %s", addr)
	}
	port, err := strconv.Atoi(addr[i+1:])
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("invalid web.listen_addr port: %s", addr)
	}
	return port, nil
}
