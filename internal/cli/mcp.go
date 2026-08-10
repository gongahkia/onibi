package cli

import (
	"github.com/spf13/cobra"

	"github.com/gongahkia/kaypoh/internal/mcpserver"
)

func newMCPCommand(runtime *runtime, version string) *cobra.Command {
	command := &cobra.Command{Use: "mcp", Short: "Run the kaypoh Model Context Protocol server"}
	command.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Serve MCP over stdio; read-only tools are always available",
		RunE: func(command *cobra.Command, _ []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			server, err := mcpserver.New(service, service.Config(), version)
			if err != nil {
				return err
			}
			return server.RunStdio(command.Context())
		},
	})
	return command
}
