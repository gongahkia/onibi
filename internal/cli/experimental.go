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
		PersistentPreRunE: func(*cobra.Command, []string) error {
			paths, err := config.DefaultPaths()
			if err != nil {
				return err
			}
			cfg, _, err := config.Load(paths)
			if err != nil {
				return err
			}
			if !cfg.Experimental.Providers {
				return errors.New("experimental provider commands are disabled; set experimental.providers=true")
			}
			return nil
		},
	}
	cmd.AddCommand(telegramCmd(), discordCmd())
	return cmd
}
