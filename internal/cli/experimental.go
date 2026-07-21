package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
)

func experimentalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "experimental",
		Short:  "Run explicitly enabled experimental features",
		Hidden: true,
	}
	cmd.AddCommand(telegramCmd())
	cmd.AddCommand(ircCmd())
	cmd.AddCommand(workspaceCmd())
	return cmd
}

func requireExperimental(enabled bool, key string) error {
	if enabled {
		return nil
	}
	return errors.New("experimental command is disabled; set " + key + "=true")
}

func requireExperimentalProviders(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	return requireExperimental(cfg.Experimental.Providers, "experimental.providers")
}

func requireExperimentalWorkspace(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	return requireExperimental(cfg.Experimental.Workspace, "experimental.workspace")
}
