package cli

import (
	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/mcpserver"
	"github.com/gongahkia/onibi/internal/store"
)

func runMCP(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	return mcpserver.Run(cmd.Context(), mcpserver.Options{
		SocketPath: paths.Socket,
		DB:         db,
	})
}
