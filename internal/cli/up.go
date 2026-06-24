package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
)

var installServiceRun = runInstallService
var webPairRun = runWebPairUp

const webPairSessionID = "local-shell"

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Set up Onibi if needed, otherwise install service and run doctor",
		RunE:  runUp,
	}
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

	ownerSet, err := auth.IsOwnerSet(cmd.Context(), db)
	if err != nil {
		return err
	}
	if !ownerSet {
		return webPairRun(cmd, paths, db)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Already paired - installing service and running doctor.")
	if err := installServiceRun(cmd, nil); err != nil {
		return err
	}
	style := styleFor(cmd)
	report := doctorRun(cmd.Context(), doctor.Options{Paths: paths, Mode: "installed"})
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", style.status(c.Status), c.Name, c.Detail)
	}
	if report.Failed() {
		return fmt.Errorf("doctor failed: see output above")
	}
	return nil
}

func runWebPairUp(cmd *cobra.Command, paths config.Paths, db *store.DB) error {
	const port = 8443
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cert, err := web.GenerateOrLoadCert(filepath.Join(paths.StateDir, "web"))
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Info("web pair server starting", "addr", fmt.Sprintf(":%d", port), "state_dir", paths.StateDir, "cert_dir", filepath.Join(paths.StateDir, "web"))
	sessionID, host, err := startWebPairShell(ctx, paths, logger)
	if err != nil {
		return err
	}
	defer func() { _ = host.Close() }()
	hostDone := make(chan error, 1)
	go func() { hostDone <- host.Wait() }()
	server := web.New(web.Options{
		TLSCert: cert,
		DB:      db,
		PTYHosts: func() map[string]*pty.Host {
			return map[string]*pty.Host{sessionID: host}
		},
		Log: logger,
	})
	errCh := make(chan error, 1)
	go func() { errCh <- server.StartContext(ctx, fmt.Sprintf(":%d", port)) }()
	if err := waitForWebHealth(ctx, port, errCh); err != nil {
		return err
	}

	token, err := setup.NewToken(ctx, db)
	if err != nil {
		return err
	}
	urls := webPairURLs(token, port, web.LANHosts(), web.PreferredHost())
	url := urls[0]
	logger.Info("web pair token minted", "url", url, "ttl", setup.PairTokenTTL.String())
	fmt.Fprintln(cmd.OutOrStdout(), "Pair Onibi from your phone:")
	fmt.Fprintln(cmd.OutOrStdout(), url)
	for _, alt := range urls[1:] {
		fmt.Fprintln(cmd.OutOrStdout(), "Fallback:", alt)
	}
	if err := setup.PrintQR(cmd.OutOrStdout(), url); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Waiting for pairing. Press Ctrl-C to stop.")

	select {
	case <-ctx.Done():
		return nil
	case err := <-hostDone:
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			logger.Warn("web pair shell exited", "session_id", sessionID, "err", err)
			return fmt.Errorf("web shell exited: %w", err)
		}
		logger.Info("web pair shell exited", "session_id", sessionID)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
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
		Env:   []string{"ONIBI_SESSION_ID=" + webPairSessionID},
		Dir:   cwd,
	})
	if err != nil {
		return "", nil, err
	}
	logger.Info("web pair shell started", "session_id", webPairSessionID, "shell", launch.Name, "command", launch.Command)
	return webPairSessionID, host, nil
}

func waitForWebHealth(ctx context.Context, port int, errCh <-chan error) error {
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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-deadline.C:
			return fmt.Errorf("web server did not become ready")
		case <-tick.C:
			resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", port))
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}
