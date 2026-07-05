//go:build onibi_remote

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: onibi up [--transport=lan-loopback] [--no-qr] [--shell sh] [--no-login-shell]")
	}
	switch args[0] {
	case "up", "start":
		return runUp(args[1:])
	case "version":
		fmt.Printf("onibi %s\ncommit %s\ndate %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	default:
		return fmt.Errorf("unsupported remote build command %q", args[0])
	}
}

func runUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	transport := fs.String("transport", "lan-loopback", "pairing transport")
	noQR := fs.Bool("no-qr", false, "print pairing URL without QR")
	shellName := fs.String("shell", "", "shell executable")
	noLoginShell := fs.Bool("no-login-shell", false, "start shell without login argv")
	logFilePath := fs.String("log-file", "", "daemon log file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("remote build supports only onibi up flags")
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
	if strings.TrimSpace(*transport) != "" {
		if err := config.Set(&cfg, "transport.mode", *transport); err != nil {
			return err
		}
	}
	if strings.TrimSpace(*shellName) != "" {
		cfg.Shell.Default = strings.TrimSpace(*shellName)
	}
	if *noLoginShell {
		cfg.Shell.Login = false
	}
	db, err := openDB(paths)
	if err != nil {
		return err
	}
	defer db.Close()
	logger, closeLog, err := newLogger(paths, *logFilePath)
	if err != nil {
		return err
	}
	defer closeLog()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	port, err := listenPort(cfg.Web.ListenAddr)
	if err != nil {
		return err
	}
	certDir := certDir(paths, cfg)
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
	session, err := spawnShell(ctx, d, cfg)
	if err != nil {
		return err
	}
	defer cleanupShell(d, session)
	if _, err := d.EnsureWebPTYHost(ctx, session.ID); err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	if err := waitForHealth(ctx, port, errCh); err != nil {
		return err
	}
	pairTransport, err := webtransport.Resolve(ctx, webtransport.ResolverOptions{
		Mode:         cfg.Transport.Mode,
		Port:         port,
		LANHosts:     web.LANHosts(),
		FallbackHost: web.PreferredHost(),
	})
	if err != nil {
		return err
	}
	token, err := setup.NewToken(ctx, db)
	if err != nil {
		return err
	}
	urls := pairTransport.URLs(token)
	if len(urls) == 0 {
		return errors.New("no pairing URL")
	}
	fmt.Fprintln(os.Stdout, urls[0])
	if !*noQR {
		if err := setup.PrintQR(os.Stdout, urls[0]); err != nil {
			return err
		}
	}
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

func openDB(paths config.Paths) (*store.DB, error) {
	key, err := secrets.GetOrCreateStoreKey(context.Background())
	if err != nil {
		return nil, err
	}
	return store.Open(paths.DBFile, store.WithStoreKey(key))
}

func newLogger(paths config.Paths, path string) (*slog.Logger, func() error, error) {
	if path == "" {
		path = filepath.Join(paths.LogDir, "onibi.log")
	}
	f, err := logging.OpenRotating(path, logging.DefaultMaxBytes, logging.DefaultBackups)
	if err != nil {
		return nil, nil, err
	}
	return logging.New(io.MultiWriter(os.Stderr, f), slog.LevelWarn), f.Close, nil
}

func certDir(paths config.Paths, cfg config.Config) string {
	if cfg.Web.CertDir != "" {
		return cfg.Web.CertDir
	}
	return filepath.Join(paths.StateDir, "web")
}

func spawnShell(ctx context.Context, d *daemon.Daemon, cfg config.Config) (*daemon.Session, error) {
	shellName := strings.TrimSpace(cfg.Shell.Default)
	if shellName == "" {
		shellName = os.Getenv("SHELL")
	}
	if shellName == "" {
		shellName = "sh"
	}
	bin, err := exec.LookPath(shellName)
	if err != nil {
		return nil, fmt.Errorf("shell %q not found in PATH: %w", shellName, err)
	}
	return d.StartManagedTmuxSession(ctx, "shell", "shell", bin, nil, "")
}

func cleanupShell(d *daemon.Daemon, s *daemon.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, _ = d.HideSession(ctx, s.ID, "end")
	cancel()
	if strings.TrimSpace(s.TmuxTarget) == "" {
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.KillTmuxTarget(ctx, s.TmuxTarget); err != nil && !tmuxTargetAlreadyGone(err) {
		fmt.Fprintf(os.Stderr, "managed tmux cleanup failed: %v\n", err)
	}
}

func tmuxTargetAlreadyGone(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "can't find session") || strings.Contains(msg, "no server running")
}

func listenPort(addr string) (int, error) {
	if strings.TrimSpace(addr) == "" {
		return 8443, nil
	}
	_, portText, ok := strings.Cut(strings.TrimSpace(addr), ":")
	if !ok || portText == "" {
		return 0, fmt.Errorf("listen address %q has no port", addr)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid listen port %q", portText)
	}
	return port, nil
}

func waitForHealth(ctx context.Context, port int, errCh <-chan error) error {
	client := &http.Client{
		Timeout: 200 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/healthz", port)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-deadline.C:
			return errors.New("web health check timed out")
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}
