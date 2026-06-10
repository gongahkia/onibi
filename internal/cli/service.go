package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/service"
)

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
