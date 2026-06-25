package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
)

var installServiceRun = runInstallService
var webPairRun = runWebPairUp

const webPairSessionID = "local-shell"

func upCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the local web cockpit and print a pairing QR",
		RunE:  runUp,
	}
	cmd.Flags().String("transport", "", "pairing transport: lan, tailscale, or auto")
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
	logger := logging.New(cmd.ErrOrStderr(), slog.LevelDebug).With("component", "up")
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
	if transport, _ := cmd.Flags().GetString("transport"); strings.TrimSpace(transport) != "" {
		if err := config.Set(&cfg, "transport.mode", transport); err != nil {
			return err
		}
		logger.Info("transport override applied", "transport", cfg.Transport.Mode)
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
	phase = time.Now()
	sessionID, host, err := startWebPairShell(ctx, paths, logger)
	if err != nil {
		return err
	}
	logPhase(logger, "shell_ready", phase, "session_id", sessionID)
	defer func() { _ = host.Close() }()
	hostDone := make(chan error, 1)
	go func() { hostDone <- host.Wait() }()
	queue := approval.New(db, approval.DefaultTTL)
	queue.Log = logger
	sweeper := &approval.Sweeper{Queue: queue, Log: logger, Interval: time.Second}
	intakeServer := intake.New(paths.Socket, func(context.Context, intake.Event) error { return nil }, logger)
	intakeServer.SetApprovalHandler(localWebApprovalHandler(queue, sessionID, logger))
	server := web.New(web.Options{
		TLSCert:       cert,
		DB:            db,
		ApprovalQueue: queue,
		PTYHosts: func() map[string]*pty.Host {
			return map[string]*pty.Host{sessionID: host}
		},
		Log: logger,
	})
	errCh := make(chan error, 1)
	phase = time.Now()
	go func() { errCh <- server.StartContext(ctx, cfg.Web.ListenAddr) }()
	go func() { errCh <- intakeServer.Serve(ctx) }()
	go sweeper.Run(ctx)
	if err := waitForWebHealth(ctx, port, errCh, logger); err != nil {
		return err
	}
	logPhase(logger, "servers_ready", phase, "socket_path", paths.Socket)

	phase = time.Now()
	token, err := setup.NewToken(ctx, db)
	if err != nil {
		return err
	}
	urls := webPairURLs(token, port, lanHosts, preferredHost)
	url := urls[0]
	logger.Info("web pair token minted",
		"primary_url", redactPairURL(url),
		"fallback_urls", redactPairURLs(urls[1:]),
		"ttl", setup.PairTokenTTL.String(),
		"expires_at", time.Now().Add(setup.PairTokenTTL).UTC().Format(time.RFC3339Nano),
		"duration_ms", time.Since(phase).Milliseconds(),
	)
	fmt.Fprintln(cmd.OutOrStdout(), "iPhone HTTPS trust:")
	fmt.Fprintln(cmd.OutOrStdout(), certPaths.MobileConfig)
	fmt.Fprintln(cmd.OutOrStdout(), "Install this profile and enable full trust if Safari warns or pairing returns Forbidden.")
	fmt.Fprintln(cmd.OutOrStdout(), "Pair Onibi from your phone:")
	fmt.Fprintln(cmd.OutOrStdout(), url)
	for _, alt := range urls[1:] {
		fmt.Fprintln(cmd.OutOrStdout(), "Fallback:", alt)
	}
	if err := setup.PrintQR(cmd.OutOrStdout(), url); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Waiting for pairing. Press Ctrl-C to stop.")
	logger.Info("onibi up ready", "uptime_ms", time.Since(started).Milliseconds())

	select {
	case <-ctx.Done():
		logger.Info("onibi up stopping", "reason", "context_cancelled", "uptime", time.Since(started).Truncate(time.Millisecond).String())
		return nil
	case err := <-hostDone:
		if ctx.Err() != nil {
			logger.Info("onibi up stopping", "reason", "context_cancelled", "uptime", time.Since(started).Truncate(time.Millisecond).String())
			return nil
		}
		if err != nil {
			logger.Warn("web pair shell exited", "session_id", sessionID, "err", err, "uptime", time.Since(started).Truncate(time.Millisecond).String())
			return fmt.Errorf("web shell exited: %w", err)
		}
		logger.Info("web pair shell exited", "session_id", sessionID, "uptime", time.Since(started).Truncate(time.Millisecond).String())
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

func localWebApprovalHandler(queue *approval.Queue, fallbackSessionID string, logger *slog.Logger) intake.ApprovalHandler {
	return func(ctx context.Context, ev intake.Event) (intake.Response, error) {
		started := time.Now()
		sessionID := ev.Session
		if sessionID == "" {
			sessionID = fallbackSessionID
		}
		agent := ev.Agent
		if agent == "" {
			agent = "agent"
		}
		tool := ev.Tool
		if tool == "" {
			tool = "tool"
		}
		inputJSON := ev.InputJSON
		if inputJSON == "" {
			inputJSON = "{}"
		}
		approvalID, ch, err := queue.Request(ctx, sessionID, agent, tool, inputJSON)
		if err != nil {
			logger.Warn("web approval request failed", "agent", agent, "tool", tool, "session_id", sessionID, "err", err, "duration_ms", time.Since(started).Milliseconds())
			return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
		}
		logger.Info("web approval requested",
			"approval_id", approvalID,
			"session_id", sessionID,
			"agent", agent,
			"tool", tool,
			"ttl", approval.DefaultTTL.String(),
		)
		select {
		case dec := <-ch:
			resp := intake.Response{
				Decision:  string(dec.Verdict),
				Reason:    dec.Reason,
				DecidedBy: dec.DecidedBy,
			}
			if len(dec.UpdatedInput) > 0 {
				resp.UpdatedInput = string(dec.UpdatedInput)
			}
			logger.Info("web approval response",
				"approval_id", approvalID,
				"session_id", sessionID,
				"agent", agent,
				"tool", tool,
				"decision", resp.Decision,
				"edited", resp.UpdatedInput != "",
				"wait_ms", time.Since(started).Milliseconds(),
			)
			return resp, nil
		case <-ctx.Done():
			_ = queue.Cancel(context.Background(), approvalID, "web pair shutdown")
			logger.Warn("web approval cancelled", "approval_id", approvalID, "session_id", sessionID, "agent", agent, "tool", tool, "reason", "context_done", "wait_ms", time.Since(started).Milliseconds())
			return intake.Response{Decision: "cancelled", Reason: "web pair shutdown"}, nil
		}
	}
}

func webPairURLs(token string, port int, lanHosts []string, fallback string) []string {
	seen := map[string]bool{}
	add := func(host string, urls []string) []string {
		if host == "" || seen[host] {
			return urls
		}
		seen[host] = true
		return append(urls, setup.WebPairURL("https", host, port, token))
	}
	var urls []string
	for _, host := range lanHosts {
		urls = add(host, urls)
	}
	urls = add(fallback, urls)
	if len(urls) == 0 {
		urls = add("localhost", urls)
	}
	return urls
}

func startWebPairShell(ctx context.Context, paths config.Paths, logger *slog.Logger) (string, *pty.Host, error) {
	cfg, _, err := config.Load(paths)
	if err != nil {
		return "", nil, err
	}
	launch, err := resolveShellLaunch(cfg.Shell.Default, cfg.Shell.Login, os.Getenv, exec.LookPath)
	if err != nil {
		return "", nil, err
	}
	cwd, _ := os.Getwd()
	host, err := pty.Spawn(ctx, pty.SpawnOptions{
		Name:  launch.Command,
		Args:  launch.Args,
		Argv0: launch.Argv0,
		Env:   []string{"ONIBI_SESSION_ID=" + webPairSessionID, "ONIBI_SOCK=" + paths.Socket},
		Dir:   cwd,
	})
	if err != nil {
		return "", nil, err
	}
	logger.Info("web pair shell started",
		"session_id", webPairSessionID,
		"shell", launch.Name,
		"command", launch.Command,
		"argv0", launch.Argv0,
		"args", strings.Join(launch.Args, " "),
		"cwd", cwd,
		"login", cfg.Shell.Login,
	)
	return webPairSessionID, host, nil
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
