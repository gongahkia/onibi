package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
)

var installServiceRun = runInstallService
var webPairRun = runWebPairUp

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
	server := web.New(web.Options{TLSCert: cert, DB: db, Log: logger})
	errCh := make(chan error, 1)
	go func() { errCh <- server.StartContext(ctx, fmt.Sprintf(":%d", port)) }()
	if err := waitForWebHealth(ctx, port, errCh); err != nil {
		return err
	}

	token, err := setup.NewToken(ctx, db)
	if err != nil {
		return err
	}
	url := setup.WebPairURL("https", web.PreferredHost(), port, token)
	logger.Info("web pair token minted", "url", url, "ttl", setup.PairTokenTTL.String())
	fmt.Fprintln(cmd.OutOrStdout(), "Pair Onibi from your phone:")
	fmt.Fprintln(cmd.OutOrStdout(), url)
	if err := setup.PrintQR(cmd.OutOrStdout(), url); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Waiting for pairing. Press Ctrl-C to stop.")

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
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
