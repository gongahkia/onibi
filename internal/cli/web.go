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
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/termui"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/webadmin"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func webCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "web", Short: "Control persistent sessions and macOS Ghostty panes from a private web interface",
		Args: cobra.NoArgs, RunE: runWeb,
	}
	cmd.AddCommand(webDoctorCmd(), webPairCmd(), webDevicesCmd(), webServiceCmd(), webStatusCmd(), webLogsCmd())
	cmd.Flags().Bool("no-banner", false, "startup banner already printed by the source launcher")
	_ = cmd.Flags().MarkHidden("no-banner")
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
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return errors.New("Onibi supports macOS and Linux, including WSL; native Windows is not supported")
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
	var activity *termui.Live
	if !background {
		presentation := terminalUIFor(cmd, cmd.ErrOrStderr())
		presentation.Interactive = presentation.Interactive && terminalUI(cmd).Interactive
		activity = termui.NewLive(presentation)
		defer activity.Close()
		console = activity
	}
	logger := logging.Web(logFile, console, level)
	var backend web.Backend
	if runtime.GOOS == "darwin" {
		backend = ghostty.New()
	}
	app, err := web.New(web.Options{Backend: backend, SessionsOnly: runtime.GOOS == "linux", Sessions: web.DaemonSessions{Socket: paths.Socket}, Diagnostics: func(ctx context.Context) ghostty.Diagnostics { return inspectWebHost(ctx, paths, runtime.GOOS) }, DB: db, PublicOrigin: origin, ProxyHosts: []string{listener.Addr().String()}, Assets: web.Assets(), Logger: logger})
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
	sessions, stopDaemon := startEmbeddedDaemon(paths, db, level, logger)
	defer stopDaemon()
	go app.RunNotifications(ctx)
	server := &http.Server{
		Handler: app, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	done := make(chan error, 1)
	if !background {
		writeWebWelcome(cmd, app.PairingURL(), listener.Addr().String(), origin, logPath, sessions)
	}
	go func() { done <- server.Serve(listener) }()
	if activity != nil {
		activity.Start(func(elapsed time.Duration) string {
			status := app.RuntimeStatus()
			phase := "HTTP live"
			if ctx.Err() != nil {
				phase = "Draining"
			}
			return activity.StatusLine(phase, elapsed, fmt.Sprintf("%d req / %d active", status.Requests, status.ActiveRequests), "Ctrl-C stops")
		})
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

// startEmbeddedDaemon runs the sessions daemon inside the web server unless
// another daemon already owns the socket. The returned stop must run after
// HTTP handlers drain, because in-flight actions still talk to the daemon.
func startEmbeddedDaemon(paths config.Paths, db *store.DB, level slog.Level, webLog *slog.Logger) (string, func()) {
	if intake.SocketActive(paths.Socket, 500*time.Millisecond) {
		return "Daemon running separately / managed sessions appear in the browser", func() {}
	}
	unavailable := func(err error) (string, func()) {
		webLog.Warn("daemon.unavailable", "err", err)
		return "Unavailable / " + err.Error(), func() {}
	}
	file, err := logging.OpenRotating(daemonLogPath(paths), logging.DefaultMaxBytes, logging.DefaultBackups)
	if err != nil {
		return unavailable(err)
	}
	d, err := newDaemon(paths, db, logging.New(file, level))
	if err != nil {
		_ = file.Close()
		return unavailable(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if intake.SocketActive(paths.Socket, 100*time.Millisecond) {
			break
		}
		select {
		case err := <-done:
			cancel()
			_ = file.Close()
			if err == nil {
				err = errors.New("daemon exited during startup")
			}
			return unavailable(err)
		default:
		}
	}
	webLog.Info("daemon.started", "log", daemonLogPath(paths))
	return "Running inside this server / persistent sessions survive restarts", func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			webLog.Warn("daemon.stop_timeout")
		}
		_ = file.Close()
	}
}

func writeWebWelcome(cmd *cobra.Command, pairingURL, address, origin, logPath, sessions string) {
	ui := terminalUI(cmd)

	bannerShown, _ := cmd.Flags().GetBool("no-banner")
	if !bannerShown {
		ui.Banner("Your terminals, in your pocket")
	}
	ui.Field("Version", termui.Revision(buildinfo.Version, buildinfo.Commit))
	ui.Field("Process", strconv.Itoa(os.Getpid()))
	ui.Field("Listening", address)
	ui.Field("Open", origin)
	if runtime.GOOS == "darwin" {
		ui.Field("Native", "Not checked yet / select a terminal in the browser")
	} else {
		ui.Field("Mode", "Persistent sessions / native Ghostty control is macOS-only")
	}
	ui.Field("Sessions", sessions)
	ui.Field("Logs", termui.ShortPath(logPath))
	ui.Line("")
	ui.Note("PAIR THIS BROWSER / one use / expires in 10 minutes")
	ui.Line("%s", pairingURL)
	ui.Line("")
	hint := commandHint()
	ui.Field("Pair again", hint+" web pair")
	ui.Field("Status", hint+" web status")
	ui.Field("Follow logs", hint+" web logs --follow")
	ui.Note("Keep this terminal open. Use a second terminal for status or logs.")
	ui.Note("Ctrl-C stops Onibi. Temporary access ends on restart.")
	ui.Note("Remembered browsers expire after 30 days or when revoked.")
	ui.Section("ACTIVITY / local server events")
}
