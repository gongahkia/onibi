package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/spf13/cobra"
)

func startCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "start", Short: "Run only the sessions daemon (onibi web already includes it)", Args: cobra.NoArgs, RunE: runStart}
	cmd.Flags().String("log-file", "", "write daemon logs to this file")
	return cmd
}
func runStart(cmd *cobra.Command, _ []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	if intake.SocketActive(paths.Socket, 500*time.Millisecond) {
		return errors.New("the sessions daemon is already running (onibi web starts one automatically); see onibi system status")
	}
	level := slog.LevelInfo
	debug, _ := cmd.Root().PersistentFlags().GetBool("debug")
	if debug {
		level = slog.LevelDebug
	}
	logPath, _ := cmd.Flags().GetString("log-file")
	if logPath == "" {
		logPath = daemonLogPath(paths)
	}
	file, err := logging.OpenRotating(logPath, logging.DefaultMaxBytes, logging.DefaultBackups)
	if err != nil {
		return err
	}
	defer file.Close()
	d, err := newDaemon(paths, db, logging.New(io.MultiWriter(cmd.ErrOrStderr(), file), level))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(contextOrBackground(cmd.Context()), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return d.Run(ctx)
}

func daemonLogPath(paths config.Paths) string {
	return filepath.Join(paths.LogDir, "onibi.log")
}

func newDaemon(paths config.Paths, db *store.DB, logger *slog.Logger) (*daemon.Daemon, error) {
	cfg, _, err := config.Load(paths)
	if err != nil {
		return nil, err
	}
	if err := render.ValidateFont(cfg.Screen.Font, cfg.Screen.FontPath); err != nil {
		return nil, fmt.Errorf("screen font: %w", err)
	}
	return daemon.New(daemon.Options{Paths: paths, DB: db, Log: logger, OutputBufferSize: cfg.Daemon.OutputBufferBytes, LivenessInterval: cfg.Daemon.LivenessInterval.Std(), UploadTTL: cfg.Daemon.UploadTTL.Std(), UploadMaxBytes: cfg.Daemon.UploadMaxBytes, ShellDefault: cfg.Shell.Default, ShellLogin: cfg.Shell.Login, ScreenFont: cfg.Screen.Font, ScreenFontPath: cfg.Screen.FontPath}), nil
}
