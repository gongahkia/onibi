package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func webCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "web", Short: "Control existing Ghostty terminals from a private web interface",
		Args: cobra.NoArgs, RunE: runWeb,
	}
	cmd.AddCommand(webDoctorCmd())
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
		return fmt.Errorf("start web listener: %w", err)
	}
	defer listener.Close()
	if bound, ok := listener.Addr().(*net.TCPAddr); !ok || !bound.IP.IsLoopback() {
		return errors.New("web listener must resolve to a loopback address")
	}
	origin, _ := cmd.Flags().GetString("origin")
	if origin == "" {
		origin = "http://" + listener.Addr().String()
	}
	app, err := web.New(web.Options{Backend: ghostty.New(), DB: db, PublicOrigin: origin, ProxyHosts: []string{listener.Addr().String()}, Assets: web.Assets()})
	if err != nil {
		return err
	}
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
	fmt.Fprintf(cmd.OutOrStdout(), "Onibi · Control Ghostty wherever text renders\nListening on %s\nPair this browser (one use, expires in 10 minutes):\n%s\nStop with Ctrl-C. Restarting revokes browser sessions.\n", listener.Addr(), app.PairingURL())
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
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
