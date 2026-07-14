//go:build onibi_remote

package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
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
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/fleetnode"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
)

const remoteStoreKeyName = "onibi.store.key.v1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: onibi up [--transport=lan-loopback] [--no-qr] [--shell sh] [--no-login-shell] | onibi fleet")
	}
	switch args[0] {
	case "up", "start":
		return runUp(args[1:])
	case "version":
		fmt.Printf("onibi %s\ncommit %s\ndate %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	case "fleet":
		return runFleet(args[1:])
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
	fleetLink, err := remoteFleetLink(context.Background(), db)
	if err != nil {
		return err
	}
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
		Paths:                  paths,
		DB:                     db,
		Log:                    logger,
		ApprovalTTL:            cfg.Daemon.ApprovalTimeout.Std(),
		ApprovalSweepInterval:  cfg.Daemon.ApprovalSweepInterval.Std(),
		ApprovalMaxSubscribers: cfg.Daemon.MaxSubscribers,
		IdleThreshold:          cfg.Daemon.TurnIdleThreshold.Std(),
		IdleInterval:           cfg.Daemon.TurnIdleInterval.Std(),
		BufferSize:             cfg.Daemon.PTYBufferBytes,
		TerminalDefault:        cfg.Terminal.Default,
		WebAddr:                cfg.Web.ListenAddr,
		WebCertDir:             certDir,
		RelayKeys:              relayKeys,
		RequireWebE2E:          webtransport.IsRelayMode(cfg.Transport.Mode),
		FleetLink:              fleetLink,
		SkipRestore:            true,
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

func runFleet(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: onibi fleet identity|proof|configure")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := openDB(paths)
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "identity":
		fs := flag.NewFlagSet("fleet identity", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		endpoint := fs.String("endpoint", "", "SSH endpoint")
		displayName := fs.String("display-name", "SSH host", "host display name")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("usage: onibi fleet identity --endpoint user@host[:port] [--display-name name]")
		}
		identity, err := fleetnode.LoadOrCreateIdentity(context.Background(), db)
		if err != nil {
			return err
		}
		host, err := identity.Host(*displayName, fleet.Endpoint{Kind: fleet.EndpointSSH, URL: strings.TrimSpace(*endpoint)}, buildinfo.Version, []string{"approval.write", "session.read", "session.write"})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(host)
	case "proof":
		fs := flag.NewFlagSet("fleet proof", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		challengeText := fs.String("challenge", "", "base64 enrollment challenge")
		hostText := fs.String("host", "", "base64 fleet host")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("usage: onibi fleet proof --challenge base64 --host base64")
		}
		var challenge fleet.EnrollmentChallenge
		var host fleet.Host
		if err := decodeRemoteFleetValue(*challengeText, &challenge); err != nil {
			return err
		}
		if err := decodeRemoteFleetValue(*hostText, &host); err != nil {
			return err
		}
		identity, err := fleetnode.LoadOrCreateIdentity(context.Background(), db)
		if err != nil {
			return err
		}
		proof, err := identity.Sign(challenge, host)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(proof)
	case "configure":
		fs := flag.NewFlagSet("fleet configure", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		enrollmentText := fs.String("enrollment", "", "base64 enrollment result")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("usage: onibi fleet configure --enrollment base64")
		}
		var enrollment fleetnode.Enrollment
		if err := decodeRemoteFleetValue(*enrollmentText, &enrollment); err != nil {
			return err
		}
		identity, err := fleetnode.LoadOrCreateIdentity(context.Background(), db)
		if err != nil {
			return err
		}
		_, err = fleetnode.Configure(context.Background(), db, identity, enrollment)
		return err
	default:
		return fmt.Errorf("unsupported remote fleet command %q", args[0])
	}
}

func remoteFleetLink(ctx context.Context, db *store.DB) (*daemon.FleetLink, error) {
	config, found, err := fleetnode.LoadConfig(ctx, db)
	if err != nil || !found {
		return nil, err
	}
	identity, err := fleetnode.LoadOrCreateIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	private, err := identity.PrivateKeyBytes()
	if err != nil {
		return nil, err
	}
	hubPublic, err := base64.RawURLEncoding.DecodeString(config.HubPublic)
	if err != nil {
		return nil, err
	}
	return daemon.NewFleetLink(daemon.FleetLinkOptions{HubURL: config.HubURL, OwnerID: config.OwnerID, HostID: config.HostID, PrivateKey: private, HubPublic: hubPublic, BinaryVersion: config.BinaryVersion, Capabilities: config.Capabilities})
}

func decodeRemoteFleetValue(raw string, dst any) error {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(b) == 0 || len(b) > 64<<10 {
		return errors.New("invalid remote fleet payload")
	}
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errors.New("invalid remote fleet payload")
	}
	return nil
}

func openDB(paths config.Paths) (*store.DB, error) {
	key, err := remoteStoreKey(filepath.Join(paths.StateDir, "store.key"))
	if err != nil {
		return nil, err
	}
	return store.Open(paths.DBFile, store.WithStoreKey(key))
}

func remoteStoreKey(path string) ([]byte, error) {
	key, err := readRemoteStoreKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errRemoteStoreKeyNotFound) {
		return nil, err
	}
	key = make([]byte, envelope.KeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := writeRemoteStoreKey(path, base64.RawURLEncoding.EncodeToString(key)); err != nil {
		return nil, err
	}
	return readRemoteStoreKey(path)
}

var errRemoteStoreKeyNotFound = errors.New("store key not found")

func readRemoteStoreKey(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		return nil, fmt.Errorf("%s has perms %#o (want 0600)", path, got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != remoteStoreKeyName {
			continue
		}
		v = strings.TrimSpace(v)
		if unquoted, err := strconv.Unquote(v); err == nil {
			v = unquoted
		} else {
			v = strings.Trim(v, `"`)
		}
		key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		if len(key) != envelope.KeyBytes {
			return nil, fmt.Errorf("store key must be %d bytes", envelope.KeyBytes)
		}
		return key, nil
	}
	return nil, errRemoteStoreKeyNotFound
}

func writeRemoteStoreKey(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "# onibi secrets; 0600 enforced\n%s=%q\n", remoteStoreKeyName, value); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
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
