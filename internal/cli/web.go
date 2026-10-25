package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/webadmin"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func webCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "web", Short: "Control existing Ghostty terminals from a private web interface",
		Args: cobra.NoArgs, RunE: runWeb,
	}
	cmd.AddCommand(webDoctorCmd(), webPairCmd(), webDevicesCmd(), webServiceCmd(), webStatusCmd(), webLogsCmd())
	cmd.Flags().String("log-level", "info", "web telemetry verbosity: info or debug (debug includes successful reads)")
	cmd.Flags().Bool("background", false, "suppress pairing secrets in service logs; use onibi web pair locally")
	cmd.Flags().String("listen", "127.0.0.1:8787", "loopback address for the local HTTP server")
	cmd.Flags().String("origin", "", "public HTTPS origin from Tailscale Serve (defaults to local HTTP)")
	return cmd
}

func validateWebListen(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid --listen address: %w", err)
	}
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return errors.New("--listen must use a loopback address; use Tailscale Serve for private HTTPS access")
	}
	return nil
}

func lockWebState(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errors.New("another Onibi web server is using this state directory")
	}
	return f, nil
}

func runWeb(cmd *cobra.Command, _ []string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("Ghostty web control requires macOS and Ghostty 1.3 or later")
	}
	levelName, _ := cmd.Flags().GetString("log-level")
	level := slog.LevelInfo
	switch levelName {
	case "info":
	case "debug":
		level = slog.LevelDebug
	default:
		return errors.New("--log-level must be info or debug")
	}
	address, _ := cmd.Flags().GetString("listen")
	if err := validateWebListen(address); err != nil {
		return err
	}
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	lock, err := lockWebState(filepath.Join(paths.StateDir, "web.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("start web listener: %w; inspect %s web status, or choose another loopback port with --listen", err, commandHint())
	}
	defer listener.Close()
	if bound, ok := listener.Addr().(*net.TCPAddr); !ok || !bound.IP.IsLoopback() {
		return errors.New("web listener must resolve to a loopback address")
	}
	origin, _ := cmd.Flags().GetString("origin")
	if origin == "" {
		origin = "http://" + listener.Addr().String()
	}
	logPath := filepath.Join(paths.LogDir, "web.log")
	logFile, err := logging.NewRotatingWriter(logPath, logging.DefaultMaxBytes, logging.DefaultBackups)
	if err != nil {
		return fmt.Errorf("open private web log: %w", err)
	}
	defer logFile.Close()
	background, _ := cmd.Flags().GetBool("background")
	var console io.Writer
	if !background {
		console = cmd.ErrOrStderr()
	}
	logger := logging.Web(logFile, console, level)
	app, err := web.New(web.Options{Backend: ghostty.New(), Diagnostics: ghostty.Inspect, DB: db, PublicOrigin: origin, ProxyHosts: []string{listener.Addr().String()}, Assets: web.Assets(), Logger: logger})
	if err != nil {
		return err
	}
	admin, err := webadmin.Listen(webadmin.Path(paths.StateDir), app)
	if err != nil {
		return err
	}
	defer admin.Close()
	ctx, stop := signal.NotifyContext(contextOrBackground(cmd.Context()), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Handler: app, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	ui := terminalUI(cmd)
	if !background {
		ui.Banner("Control Ghostty wherever text renders")
		ui.Field("Version", buildinfo.Version+" ("+buildinfo.Commit+")")
		ui.Field("Process", strconv.Itoa(os.Getpid()))
		ui.Field("Listening", listener.Addr().String())
		ui.Field("Open", origin)
		ui.Field("Native", "Not checked yet / select a terminal in the browser")
		ui.Field("Logs", logPath)
		ui.Line("")
		ui.Note("PAIR THIS BROWSER / one use / expires in 10 minutes")
		ui.Line("%s", app.PairingURL())
		ui.Line("")
		hint := commandHint()
		ui.Field("Pair again", hint+" web pair")
		ui.Field("Status", hint+" web status")
		ui.Field("Follow logs", hint+" web logs --follow")
		ui.Note("Ctrl-C stops Onibi. Temporary access ends on restart.")
		ui.Note("Remembered browsers expire after 30 days or when revoked.")
		ui.Line("")
	}
	logger.Info("web.started", "version", buildinfo.Version, "commit", buildinfo.Commit, "pid", os.Getpid(), "log_level", levelName)
	defer logger.Info("web.stopped")
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("web.stopping", "drain_timeout_seconds", 45)
		// Actions survive browser disconnects and have a 30-second execution
		// budget plus 5 seconds to persist their receipt. Keep the database and
		// exclusive state lock alive until those handlers have drained.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
