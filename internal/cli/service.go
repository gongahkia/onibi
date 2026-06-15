package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/service"
)

const serviceSocketReadyTimeout = 30 * time.Second

func runInstallService(cmd *cobra.Command, _ []string) error {
	m, err := serviceManager()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	if err := m.Install(ctx); err != nil {
		return err
	}
	path, _ := m.ServicePath()
	if !waitForSocket(ctx, m.Paths.Socket, serviceSocketReadyTimeout) {
		return fmt.Errorf("installed %s, but daemon socket did not become ready within %s; run `onibi doctor --mode installed`", path, serviceSocketReadyTimeout)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Installed and started %s\n", path)
	return nil
}

func runUninstallService(cmd *cobra.Command, _ []string) error {
	m, err := serviceManager()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	path, _ := m.ServicePath()
	if err := m.Uninstall(ctx); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled %s\n", path)
	return nil
}

func serviceManager() (*service.Manager, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}
	return service.NewManager(paths, "")
}

func waitForSocket(ctx context.Context, socket string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if intake.SocketActive(socket, 200*time.Millisecond) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}
